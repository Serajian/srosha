// Package fcm sends a push notification to one device through Firebase Cloud
// Messaging, and is the only place that knows anything about its HTTP v1 API:
// the url, the json, the error codes and what each of them means for a retry.
//
// It is unlike the channels before it in one way that shows up everywhere. Its
// credential is a service account -- a private key -- and a private key cannot
// be put in a header. It has to be exchanged for a short-lived access token
// first, which is what internal/infra/googleauth does and this package only
// asks for.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/googleauth"
	"github.com/Serajian/srosha/pkg/errs"
)

// Tokens supplies the access token Google actually accepts.
//
// One method, and no mention of the service account it came from: opening that
// is registry's job, and this package is handed the result. Whoever holds the
// key satisfies it; internal/infra/googleauth does.
type Tokens interface {
	Token(ctx context.Context) (string, error)
}

// Sender is one Firebase project.
type Sender struct {
	client *http.Client
	tokens Tokens

	// url is built once. The project is in the path and never changes for a
	// given credential, so there is nothing per-message to put in it.
	url string
}

// New takes the project separately from the token source because they come from
// the same place -- inside the service account file -- and neither is asked of a
// source twice. There is no settings type for this channel for that reason.
func New(client *http.Client, tokens Tokens, projectID string) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("fcm sender has no http client")
	}
	if tokens == nil {
		return nil, errs.InternalErr("fcm sender cannot mint tokens")
	}
	if strings.TrimSpace(projectID) == "" {
		// A configuration answer, not a provider one. The core turns this into
		// NO_SENDER and tells the source rather than calling to be refused.
		return nil, errs.InvalidInputErr("no firebase project for this identity")
	}

	return &Sender{
		client: client,
		tokens: tokens,
		url:    fmt.Sprintf(endpoint, url.PathEscape(projectID)),
	}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelFCM }

// Send delivers one notification to one device and returns FCM's name for it.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	req, err := s.request(ctx, m)
	if err != nil {
		return "", err
	}

	// The host is a constant -- Google's -- and the only part that came from a
	// file is the project, escaped into one path segment by New.
	resp, err := s.client.Do(req) //nolint:gosec // see New
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || answer.Error.Code != 0 {
		return "", classify(resp, answer)
	}
	if answer.Name == "" {
		return "", unreachable("answer", errNoMessageName)
	}
	return answer.Name, nil
}

// request builds the whole call, minting the access token as its last step so
// that nothing is spent on a message that was going to be refused anyway.
func (s *Sender) request(ctx context.Context, m shared.Message) (*http.Request, error) {
	body, err := s.payload(m)
	if err != nil {
		return nil, err
	}

	token, err := s.tokens.Token(ctx)
	if err != nil {
		if errors.Is(err, googleauth.ErrRejected) {
			// Google refused the account, not the message. A well-formed key
			// file is not the same as a working one, and this is the only
			// place that difference shows up.
			return nil, refused(err.Error())
		}
		return nil, unreachable("token", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, unreachable("request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return req, nil
}

func (s *Sender) payload(m shared.Message) ([]byte, error) {
	device := strings.TrimSpace(m.Recipient.Address)
	switch {
	case len(device) < minDeviceTokenLen, len(device) > maxDeviceTokenLen:
		return nil, refused("the message has no usable device token")
	case strings.TrimSpace(m.Body) == "":
		// FCM shows a notification with no body as an empty row. A title alone
		// is a message the recipient cannot read.
		return nil, refused("a push notification needs a body")
	case utf8.RuneCountInString(m.Title) > maxTitleLen:
		return nil, refused(fmt.Sprintf("title is longer than fcm accepts (max %d)", maxTitleLen))
	case utf8.RuneCountInString(m.Body) > maxBodyLen:
		return nil, refused(fmt.Sprintf("body is longer than fcm accepts (max %d)", maxBodyLen))
	}

	data, err := payloadData(m.Metadata)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(sendRequest{Message: message{
		Token:        device,
		Notification: notification{Title: strings.TrimSpace(m.Title), Body: m.Body},
		Data:         data,
	}})
	if err != nil {
		return nil, refused("message could not be encoded for fcm")
	}
	return body, nil
}

// payloadData carries the source's metadata into the push as FCM's data
// payload. The two are the same shape -- string to string -- so nothing is
// interpreted here and nothing is invented.
//
// A reserved key is refused rather than dropped. FCM would refuse the whole
// message for one, and a source whose key silently disappeared would have no
// way to find out.
func payloadData(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	for key := range metadata {
		if reservedDataKey(key) {
			return nil, refused(fmt.Sprintf("metadata key %q is reserved by fcm", key))
		}
	}
	return metadata, nil
}

// reservedDataKey reports the names FCM keeps for itself. Two exact ones and
// two prefixes, matched without case because Google does not say it matters and
// a wrong guess here is a message refused after the round trip.
func reservedDataKey(key string) bool {
	k := strings.ToLower(key)
	switch {
	case k == "from", k == "message_type", k == "notification":
		return true
	case strings.HasPrefix(k, "google"), strings.HasPrefix(k, "gcm"):
		return true
	default:
		return false
	}
}
