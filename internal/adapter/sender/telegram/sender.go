// Package telegram sends through the Bot API, and is the only place that knows
// anything about it: its address, its json, its status codes and what each of
// them means for a retry.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Config is what a source may set about its bot beyond the token. It arrives as
// the raw json stored with the credential and is parsed here, because what a
// provider needs is the provider's own business.
type Config struct {
	// ParseMode is empty by default, which means plain text -- and plain text
	// is the only mode where a message cannot fail for its own punctuation. Set
	// to HTML or MarkdownV2 and the body travels as written: a source that asks
	// for markup owns escaping it, because escaping on their behalf would break
	// the markup they meant.
	ParseMode string `json:"parse_mode"`
}

// Sender is one bot. It is built per send and holds no connection of its own --
// the http client underneath is shared and pools its sockets.
type Sender struct {
	client  *http.Client
	token   string
	config  Config
	baseURL string
}

func New(client *http.Client, token string, rawConfig []byte) (*Sender, error) {
	if client == nil {
		return nil, errs.InternalErr("telegram sender has no http client")
	}
	if token == "" {
		// A configuration answer, not a provider one. The core turns this into
		// NO_SENDER and tells the source, rather than retrying a bot that
		// cannot exist.
		return nil, errs.InvalidInputErr("no telegram token for this identity")
	}
	if !usableToken(token) {
		// Never echoed, whatever it turned out to be.
		return nil, errs.InvalidInputErr("telegram token has the wrong format")
	}

	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.InvalidInputErr("telegram settings are not valid json").
				WithErr(err)
		}
	}

	return &Sender{client: client, token: token, config: config, baseURL: apiBase}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelTelegram }

// Send posts one message and returns the id the Bot API gave it.
//
// That id is the handle a source needs to find the message on their own side.
// We do not track delivery ourselves, so it is the only thing we can hand back.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	text := compose(m.Title, m.Body)
	if utf8.RuneCountInString(text) > maxTextLen {
		return "", refused(
			fmt.Sprintf("message is longer than telegram accepts (max %d)", maxTextLen),
		)
	}

	payload, err := json.Marshal(sendMessageRequest{
		ChatID:    m.Recipient.Address,
		Text:      text,
		ParseMode: s.config.ParseMode,
	})
	if err != nil {
		return "", refused("message could not be encoded for telegram")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		s.endpoint(),
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", unreachable("request", s.token, err)
	}
	req.Header.Set("Content-Type", "application/json")

	// The host is a constant and the token is checked against an alphabet that
	// cannot leave the path, so the only part of this url anybody else chooses
	// cannot choose where it goes.
	resp, err := s.client.Do(req) //nolint:gosec // see usableToken and apiBase
	if err != nil {
		return "", unreachable("call", s.token, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var answer apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer); err != nil {
		// A body we cannot read is not a message we can conclude anything
		// about: it may well have been sent.
		return "", unreachable("answer", s.token, err)
	}

	if !answer.OK || resp.StatusCode != http.StatusOK {
		return "", classify(resp.StatusCode, answer)
	}
	if answer.Result == nil {
		return "", unreachable("answer", s.token, errNoResult)
	}
	return strconv.FormatInt(answer.Result.MessageID, 10), nil
}

// usableToken keeps the token to characters that cannot change which endpoint
// is called. It is the same reason the api base is a constant: everything that
// decides where a secret is sent has to be ours.
func usableToken(token string) bool {
	return strings.IndexFunc(token, func(r rune) bool {
		return !strings.ContainsRune(tokenAlphabet, r)
	}) < 0
}

// endpoint is where the token lives, which is why it is built here and never
// logged: the Bot API puts the credential in the path rather than in a header,
// so a url from this sender IS the secret.
func (s *Sender) endpoint() string {
	return s.baseURL + "/bot" + s.token + "/" + sendMethod
}

// compose puts a title and a body into the one text field telegram has.
//
// No formatting is added around the title. In plain text there is none to add,
// and in a markup mode anything added here would have to be escaped against a
// body we have deliberately not touched.
func compose(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return body
	}
	return title + titleGap + body
}
