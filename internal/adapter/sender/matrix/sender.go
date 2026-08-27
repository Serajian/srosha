// Package matrix sends into a room on a homeserver, and is the only place that
// knows anything about the Client-Server API: its paths, its json, its error
// codes and what each of them means for a retry.
//
// Two things make it unlike the channels before it. The address is a ROOM and
// never a person -- Matrix has no way to message somebody, only to write where
// they are listening. And the homeserver comes from the source rather than a
// constant, because the protocol is federated and there is no address that is
// right for everybody.
package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Sender is one Matrix account on one homeserver.
type Sender struct {
	client *http.Client
	token  string
	cfg    Config
}

// New takes a parsed config rather than raw json, as mail and whatsapp do: the
// homeserver is required, and it is an address somebody else chose, so it is
// checked once at registration instead of on every message.
func New(client *http.Client, token string, cfg Config) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("matrix sender has no http client")
	}
	if token == "" {
		// A configuration answer, not a provider one. The core turns this into
		// NO_SENDER and tells the source rather than calling to be refused.
		return nil, errs.InvalidInputErr("no matrix access token for this identity")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Sender{client: client, token: token, cfg: cfg}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelMatrix }

// Send writes one message into a room and returns the event id it became.
//
// The delivery id is the transaction id, and that is the point of it being on
// the message at all: a homeserver will not create a second event for a
// transaction it has already seen, so a redelivered event that reaches here
// again does not put the message in the room twice.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	if m.DeliveryID.IsZero() {
		// Without it every attempt would be a new transaction, and a retry would
		// write the message into the room again.
		return "", refused("the message has no id to send it under")
	}

	text := compose(m.Title, m.Body)
	if utf8.RuneCountInString(text) > maxTextLen {
		return "", refused(
			fmt.Sprintf("message is longer than matrix accepts (max %d)", maxTextLen),
		)
	}

	payload, err := json.Marshal(sendRequest{MsgType: messageType, Body: text})
	if err != nil {
		return "", refused("message could not be encoded for matrix")
	}

	endpoint, err := s.endpoint(m.Recipient.Address, m.DeliveryID.String())
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", unreachable("request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.token)

	// The host is the source's, which is the one thing this channel cannot make
	// a constant -- so it is checked at registration instead: https, no
	// credentials, no path. See Config.validate.
	resp, err := s.client.Do(req) //nolint:gosec // see Config.validate
	if err != nil {
		return "", unreachable("call", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var answer apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer); err != nil {
		// A body we cannot read is not a message we can conclude anything
		// about: it may well have been sent.
		return "", unreachable("answer", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || answer.ErrCode != "" {
		return "", classify(resp.StatusCode, answer)
	}
	if answer.EventID == "" {
		return "", unreachable("answer", errNoEventID)
	}
	return answer.EventID, nil
}

// endpoint puts the room and the transaction into the path.
//
// Both are escaped, and a room id needs it: it begins with "!" and carries a
// ":" before the server name, neither of which survives a path unencoded.
func (s *Sender) endpoint(room, txn string) (string, error) {
	if strings.TrimSpace(room) == "" {
		return "", refused("the message has no room to go to")
	}
	return s.cfg.Homeserver + fmt.Sprintf(sendPath, url.PathEscape(room), url.PathEscape(txn)), nil
}

// compose puts a title and a body into the one text field there is.
func compose(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return body
	}
	return title + titleGap + body
}
