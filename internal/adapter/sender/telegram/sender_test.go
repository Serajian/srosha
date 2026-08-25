package telegram_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/sender/telegram"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const token = "123456:AAH-not-a-real-token"

type request struct {
	path      string
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// bot stands in for the Bot API: it records what it was asked and answers with
// whatever the test wants.
func bot(t *testing.T, status int, answer string) (*telegram.Sender, *request) {
	t.Helper()
	return botWithConfig(t, status, answer, nil)
}

func botWithConfig(t *testing.T, status int, answer string, config []byte) (*telegram.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, got)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	s, err := telegram.New(server.Client(), token, config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.WithBaseURL(server.URL), got
}

func msg(title, body string) shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelTelegram, Address: "-1001234"},
		Title:     title,
		Body:      body,
	}
}

const okAnswer = `{"ok":true,"result":{"message_id":4242}}`

func TestASentMessageComesBackWithItsID(t *testing.T) {
	s, got := bot(t, http.StatusOK, okAnswer)

	id, err := s.Send(context.Background(), msg("Deploy", "it is done"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "4242" {
		t.Errorf("id = %q, want the message_id telegram gave", id)
	}
	if got.ChatID != "-1001234" {
		t.Errorf("chat_id = %q", got.ChatID)
	}
	if !strings.Contains(got.path, "/bot"+token+"/sendMessage") {
		t.Errorf("path = %q", got.path)
	}
}

// Telegram has one text field, so a title and a body have to become one string.
func TestTheTitleGoesAboveTheBody(t *testing.T) {
	s, got := bot(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("Deploy", "it is done")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.Text != "Deploy\n\nit is done" {
		t.Errorf("text = %q", got.Text)
	}
}

func TestAMessageWithNoTitleIsJustItsBody(t *testing.T) {
	s, got := bot(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("   ", "it is done")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.Text != "it is done" {
		t.Errorf("text = %q, want no leading blank lines", got.Text)
	}
}

// Plain text is the default because it is the only mode where a message cannot
// fail for its own punctuation.
func TestPlainTextIsTheDefault(t *testing.T) {
	s, got := bot(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "1 < 2 & 3 > 2")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.ParseMode != "" {
		t.Errorf("parse_mode = %q, want it absent", got.ParseMode)
	}
	if got.Text != "1 < 2 & 3 > 2" {
		t.Errorf("text = %q, want it untouched", got.Text)
	}
}

func TestASourceCanAskForMarkup(t *testing.T) {
	s, got := botWithConfig(t, http.StatusOK, okAnswer, []byte(`{"parse_mode":"HTML"}`))

	if _, err := s.Send(context.Background(), msg("", "<b>up</b>")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.ParseMode != "HTML" {
		t.Errorf("parse_mode = %q, want HTML", got.ParseMode)
	}
	if got.Text != "<b>up</b>" {
		t.Errorf("text = %q, want it unescaped -- the source owns their markup", got.Text)
	}
}

// The one question the core asks of every failure.
func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		answer    string
		permanent bool
		retry     time.Duration
	}{
		{
			name:      "chat not found",
			status:    http.StatusBadRequest,
			answer:    `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`,
			permanent: true,
		},
		{
			name:      "bot was blocked",
			status:    http.StatusForbidden,
			answer:    `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`,
			permanent: true,
		},
		{
			name:      "bad token",
			status:    http.StatusUnauthorized,
			answer:    `{"ok":false,"error_code":401,"description":"Unauthorized"}`,
			permanent: true,
		},
		{
			name:   "too many requests",
			status: http.StatusTooManyRequests,
			answer: `{"ok":false,"error_code":429,"description":"Too Many Requests",` +
				`"parameters":{"retry_after":17}}`,
			permanent: false,
			retry:     17 * time.Second,
		},
		{
			name:      "too many requests with no hint",
			status:    http.StatusTooManyRequests,
			answer:    `{"ok":false,"error_code":429,"description":"Too Many Requests"}`,
			permanent: false,
			retry:     30 * time.Second,
		},
		{
			name:      "their fault",
			status:    http.StatusBadGateway,
			answer:    `{"ok":false,"error_code":502,"description":"Bad Gateway"}`,
			permanent: false,
		},
		{
			name:      "ok false with a 200",
			status:    http.StatusOK,
			answer:    `{"ok":false,"description":"something undocumented"}`,
			permanent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := bot(t, tt.status, tt.answer)

			_, err := s.Send(context.Background(), msg("", "hello"))
			if err == nil {
				t.Fatal("Send succeeded")
			}
			if got := shared.IsPermanentSend(err); got != tt.permanent {
				t.Errorf("permanent = %v, want %v (%v)", got, tt.permanent, err)
			}
			if got := shared.SendRetryAfter(err); got != tt.retry {
				t.Errorf("retry after = %v, want %v", got, tt.retry)
			}
		})
	}
}

// A message telegram will refuse for its length is refused here, before the
// round trip that was always going to fail.
func TestAMessageTooLongIsNotEvenSent(t *testing.T) {
	s, got := bot(t, http.StatusOK, okAnswer)

	_, err := s.Send(context.Background(), msg("", strings.Repeat("ب", 4097)))
	if err == nil {
		t.Fatal("Send succeeded on a message past the limit")
	}
	if !shared.IsPermanentSend(err) {
		t.Error("a message that is too long will be too long next time as well")
	}
	if got.path != "" {
		t.Error("the request was made anyway")
	}
}

// Nothing about an unreachable provider says anything about the message.
func TestAProviderWeCannotReachIsWorthAnotherTry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	s, err := telegram.New(http.DefaultClient, token, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := s.WithBaseURL(url).Send(context.Background(), msg("", "hello")); err == nil {
		t.Fatal("Send succeeded against a closed server")
	} else if shared.IsPermanentSend(err) {
		t.Errorf("a dial failure was called permanent: %v", err)
	}
}

func TestASenderNeedsATokenAndReadableSettings(t *testing.T) {
	if _, err := telegram.New(http.DefaultClient, "", nil); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New with no token = %v, want invalid input", err)
	}
	if _, err := telegram.New(http.DefaultClient, token, []byte("parse_mode=HTML")); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New with unreadable settings = %v, want invalid input", err)
	}
	if _, err := telegram.New(nil, token, nil); err == nil {
		t.Error("New with no client succeeded")
	}
}

// The token goes in the path, so a token with a slash in it would be choosing
// which endpoint we call.
func TestATokenCannotChooseTheEndpoint(t *testing.T) {
	for _, bad := range []string{"1:a/../../x", "1:a b", "1:a\nHost: evil", "1:a?x=1", "1:a#f"} {
		if _, err := telegram.New(http.DefaultClient, bad, nil); !errs.IsType(err, errs.ErrInvalidInput) {
			t.Errorf("New(%q) = %v, want invalid input", bad, err)
		}
	}
}

// The Bot API puts the credential in the path, so a url from this sender is the
// secret. It must not reach an error a person will read.
func TestTheTokenNeverReachesAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	s, err := telegram.New(http.DefaultClient, token, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.WithBaseURL(url).Send(context.Background(), msg("", "hello"))
	if err == nil {
		t.Fatal("Send succeeded against a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token is in the error: %v", err)
	}
}
