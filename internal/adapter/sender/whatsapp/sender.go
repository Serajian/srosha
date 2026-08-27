// Package whatsapp sends through Meta's Cloud API, and is the only place that
// knows anything about it: its address, its json, its error codes and what each
// of them means for a retry.
//
// It is also the first channel that cannot always say what it likes. Outside the
// window a recipient opened, WhatsApp accepts only a template they approved in
// advance -- so the source says which one through the message's metadata, and a
// message carrying none is sent as text.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Sender is one WhatsApp business number.
type Sender struct {
	client *http.Client
	token  string
	cfg    Config

	baseURL string
}

// New takes a parsed config rather than raw json, because these settings are
// required and this is the only channel besides mail where they are. A source's
// stored json goes through ParseConfig first; the service's own identity comes
// from settings and never was json.
func New(client *http.Client, token string, cfg Config) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("whatsapp sender has no http client")
	}
	if token == "" {
		// A configuration answer, not a provider one. The core turns this into
		// NO_SENDER and tells the source rather than calling to be refused.
		return nil, errs.InvalidInputErr("no whatsapp token for this identity")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Sender{client: client, token: token, cfg: cfg, baseURL: apiBase}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelWhatsApp }

// Send hands one message over and returns the id Meta gave it.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	body, err := s.compose(m)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", refused("message could not be encoded for whatsapp")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return "", unreachable("request", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// A header, not the path -- which is the one real difference from the bot
	// channels, and the reason a url from this sender is not itself a secret.
	req.Header.Set("Authorization", "Bearer "+s.token)

	// The host is a constant and the only part of this url anybody else chooses
	// is the phone number id, checked against an alphabet that cannot leave the
	// path.
	resp, err := s.client.Do(req) //nolint:gosec // see usableID and apiBase
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || answer.Error != nil {
		return "", classify(resp.StatusCode, answer, s.token)
	}
	if len(answer.Messages) == 0 || answer.Messages[0].ID == "" {
		return "", unreachable("answer", errNoMessageID)
	}
	return answer.Messages[0].ID, nil
}

// compose decides between a template and text, which is the source's decision
// and not ours: they know whether the recipient wrote to them recently and we do
// not. A message naming a template is sent as one; anything else is text.
func (s *Sender) compose(m shared.Message) (sendRequest, error) {
	to, err := recipient(m.Recipient.Address)
	if err != nil {
		return sendRequest{}, err
	}

	base := sendRequest{MessagingProduct: "whatsapp", To: to}

	t, wanted, err := templateFrom(m.Metadata)
	if err != nil {
		return sendRequest{}, err
	}
	if wanted {
		base.Type = "template"
		base.Template = &templateBody{
			Name:       t.name,
			Language:   templateLanguage{Code: t.language},
			Components: components(t.params),
		}
		return base, nil
	}

	text := compose(m.Title, m.Body)
	if utf8.RuneCountInString(text) > maxTextLen {
		return sendRequest{}, refused(
			fmt.Sprintf("message is longer than whatsapp accepts (max %d)", maxTextLen))
	}

	base.Type = "text"
	base.Text = &textBody{Body: text}
	return base, nil
}

// components carries the positional parameters into the one shape Meta accepts.
// No parameters means no components at all, not an empty one: a template with
// none is refused if it is sent an empty list.
func components(params []string) []component {
	if len(params) == 0 {
		return nil
	}

	out := make([]parameter, 0, len(params))
	for _, p := range params {
		out = append(out, parameter{Type: "text", Text: p})
	}
	return []component{{Type: "body", Parameters: out}}
}

// recipient is the address as Meta wants it: digits, no plus. Everywhere else in
// this service a phone number is E.164 and carries one.
func recipient(address string) (string, error) {
	to, ok := strings.CutPrefix(strings.TrimSpace(address), "+")
	if !ok || to == "" {
		return "", refused("the recipient address is not a phone number")
	}
	return to, nil
}

// endpoint is where the message goes. The phone number id is the sending
// number's, not the recipient's.
func (s *Sender) endpoint() string {
	return s.baseURL + "/" + apiVersion + "/" + s.cfg.PhoneNumberID + "/" + sendPath
}

// compose puts a title and a body into the one text field there is.
func compose(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return body
	}
	return title + titleGap + body
}
