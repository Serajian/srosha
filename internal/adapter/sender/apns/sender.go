// Package apns sends a push notification to one Apple device, and is the only
// place that knows anything about the APNs provider API: its headers, its json,
// its one-word refusals and what each of them means for a retry.
//
// It is the second channel whose credential is not what gets sent, and it goes
// further than the first. Google exchanges a service account for a token; Apple
// has no endpoint to ask at all -- the token is a JWT this service signs
// itself, which is internal/infra/appleauth's job and never this package's.
package apns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Tokens supplies the provider token APNs authenticates with.
//
// No context, unlike fcm's: signing a token reaches nobody and takes
// microseconds, so there is nothing to cancel. Expire is here because one of
// Apple's refusals is about the token itself, and the only useful response to
// it is a different one.
type Tokens interface {
	Token() (string, error)
	Expire()
}

// Sender is one app on one APNs environment.
type Sender struct {
	client *http.Client
	tokens Tokens
	cfg    Config

	// host is production or sandbox, decided once.
	host string
}

// New takes the parsed config rather than raw json, as mail, whatsapp and
// matrix do: every field is required, and the environment is the kind of
// mistake that is much cheaper to catch here than as BadDeviceToken later.
func New(client *http.Client, tokens Tokens, cfg Config) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("apns sender has no http client")
	}
	if tokens == nil {
		return nil, errs.InternalErr("apns sender cannot sign provider tokens")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Sender{client: client, tokens: tokens, cfg: cfg, host: cfg.host()}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelAPNs }

// Send delivers one notification to one device and returns the id APNs knows it
// by.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	req, err := s.request(ctx, m)
	if err != nil {
		return "", err
	}

	// The host is one of two constants and the only part of the path that came
	// from outside is the device token, checked to be hexadecimal above.
	resp, err := s.client.Do(req) //nolint:gosec // see payload and Config.host
	if err != nil {
		return "", unreachable("call", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The id is in a header both ways: APNs echoes ours, or makes one up when
	// we send none. There is no success body to read it out of.
	id := resp.Header.Get(headerID)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var answer apiResponse
		// A refusal always has a body; a truncated one still leaves the status.
		_ = json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer)

		if answer.Reason == reasonExpiredToken {
			// Throw the held token away, so the next attempt signs a new one
			// rather than presenting the same expired one until the attempts
			// run out.
			s.tokens.Expire()
		}
		return "", classify(resp.StatusCode, answer)
	}

	if id == "" {
		return "", unreachable("answer", errNoNotificationID)
	}
	return id, nil
}

func (s *Sender) request(ctx context.Context, m shared.Message) (*http.Request, error) {
	device, err := deviceToken(m.Recipient.Address)
	if err != nil {
		return nil, err
	}

	body, err := payload(m)
	if err != nil {
		return nil, err
	}

	token, err := s.tokens.Token()
	if err != nil {
		// The key was checked when the credential was opened, so a failure here
		// is not the key being wrong. Final anyway: signing is local, and a
		// second attempt does the same arithmetic.
		return nil, refused(err.Error())
	}

	endpoint := s.host + fmt.Sprintf(sendPath, url.PathEscape(device))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, unreachable("request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set(headerTopic, s.cfg.Topic)
	req.Header.Set(headerPushType, pushTypeAlert)
	req.Header.Set(headerPriority, priorityImmediate)

	// Our own id, so the row in deliveries and the notification Apple reports
	// on are the same thing. APNs makes one up otherwise, and then the two
	// cannot be lined up at all.
	if id, ok := notificationID(m.DeliveryID); ok {
		req.Header.Set(headerID, id)
	}
	return req, nil
}

// payload builds the json. The message goes under "aps", which is Apple's, and
// the source's metadata goes beside it at the top level, which is theirs.
func payload(m shared.Message) ([]byte, error) {
	body := strings.TrimSpace(m.Body)
	if body == "" {
		// A notification with no body is a blank row on the lock screen.
		return nil, refused("a push notification needs a body")
	}

	alert := map[string]string{"body": body}
	if title := strings.TrimSpace(m.Title); title != "" {
		alert["title"] = title
	}

	out := map[string]any{"aps": map[string]any{"alert": alert}}
	for key, value := range m.Metadata {
		if strings.EqualFold(key, "aps") {
			// Refused rather than dropped, as fcm does: overwriting Apple's own
			// key would change the message into something else entirely, and
			// dropping it silently would leave the source none the wiser.
			return nil, refused(`metadata key "aps" is reserved by apns`)
		}
		out[key] = value
	}

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, refused("message could not be encoded for apns")
	}
	if len(raw) > maxPayloadBytes {
		return nil, refused(fmt.Sprintf(
			"payload is %d bytes, more than apns accepts (max %d)", len(raw), maxPayloadBytes))
	}
	return raw, nil
}

// deviceToken checks the address before it becomes part of a url.
func deviceToken(address string) (string, error) {
	t := strings.TrimSpace(address)
	if len(t) < minDeviceTokenLen || len(t) > maxDeviceTokenLen || !isHex(t) {
		return "", refused("the message has no usable device token")
	}
	return t, nil
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
