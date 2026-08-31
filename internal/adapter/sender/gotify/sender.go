// Package gotify posts to a self-hosted Gotify server, and is the only place
// that knows anything about its Client-Server API: its address, its json and
// what a refusal from it means for a retry.
//
// Two things make it unlike the channels before it. The server is
// source-chosen, as Matrix's homeserver was -- Gotify is self-hosted, so
// there is no address that is right for everybody. And its credential's
// secret, the application token, is also what a stock server uses to decide
// which application receives a message -- so this channel's address (the
// application id, per the owner's spec) sits in tension with the token in a
// way no other channel's does. See (*Sender).endpoint, which is the one
// place that tension is resolved and the one place to revisit if the
// resolution turns out wrong.
package gotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Sender is one application on one Gotify server.
type Sender struct {
	client *http.Client
	token  string
	cfg    Config
}

// New takes a parsed config rather than raw json, as mail, whatsapp and
// matrix did: the server url is required and it is an address somebody else
// chose, so it is checked once at registration instead of on every message.
func New(client *http.Client, token string, cfg Config) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("gotify sender has no http client")
	}
	if token == "" {
		// A configuration answer, not a provider one. The core turns this into
		// NO_SENDER and tells the source rather than calling to be refused.
		return nil, errs.InvalidInputErr("no gotify application token for this identity")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Sender{client: client, token: token, cfg: cfg}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelGotify }

// Send posts one message and returns the id Gotify gave it.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	if utf8.RuneCountInString(m.Body) > maxTextLen {
		return "", refused(
			fmt.Sprintf("message is longer than gotify accepts (max %d)", maxTextLen),
		)
	}

	endpoint, err := s.endpoint(m.Recipient.Address)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(sendRequest{Title: m.Title, Message: m.Body})
	if err != nil {
		return "", refused("message could not be encoded for gotify")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", unreachable("request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// The host is the source's, which is the one thing this channel cannot
	// make a constant -- so it is checked at registration instead: https, no
	// credentials, no path. See Config.validate.
	resp, err := s.client.Do(req) //nolint:gosec // see Config.validate
	if err != nil {
		return "", unreachable("call", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var answer apiError
		// Best effort: if the body is not the shape apiError assumes, the
		// fields decode as zero values and classify falls back to the status
		// alone, which is still enough to decide on a retry.
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer)
		return "", classify(resp.StatusCode, answer)
	}

	var answer apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer); err != nil {
		// A body we cannot read is not a message we can conclude anything
		// about: it may well have been sent.
		return "", unreachable("answer", err)
	}
	if answer.ID == 0 {
		return "", unreachable("answer", errNoMessageID)
	}
	return strconv.FormatInt(answer.ID, 10), nil
}

// endpoint is the one place a credential and a recipient become a call to
// Gotify. Read this before changing how either is used.
//
// ASSUMPTION, unverified -- this service has no network access to check it
// against a real server. Gotify's documented Client-Server API posts a
// message with
//
//	POST {server}/message?token={applicationToken}
//
// and the application token alone decides which application's subscribers
// see it: a token is minted per application, so the documented request has
// no field for an application id at all. Under that reading, the application
// id the owner specified as this credential's address is redundant with the
// token -- the token already says which application.
//
// The address is not dropped for that. It travels as well, in a second query
// parameter this service adds: `appid`. A stock Gotify server ignores query
// parameters it does not define, so this is harmless against the documented
// API, and it is exactly what would matter if the owner's server is not the
// stock one -- see below.
//
// What would change if the owner's server uses a CLIENT token instead of an
// application token: Gotify's client tokens are per-user, sent as the header
// `X-Gotify-Key` rather than in the query string, and the documented API
// accepts them only on the stream and management endpoints -- never on
// POST /message, which refuses anything but an application token. A server
// that accepts a client token here is already not the documented one, and it
// is exactly the shape where `appid` would stop being a harmless extra and
// start being how it picks the application to post into. This function would
// then need the token moved into a header instead of the `token` query
// parameter, and `appid` would become load-bearing rather than redundant.
// That is the one thing to confirm with the owner before trusting this
// further.
func (s *Sender) endpoint(applicationID string) (string, error) {
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return "", refused("the message has no application id to send to")
	}

	u, err := url.Parse(s.cfg.ServerURL + sendPath)
	if err != nil {
		return "", refused("gotify server url could not be used")
	}
	q := u.Query()
	q.Set(tokenParam, s.token)
	q.Set(appIDParam, applicationID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
