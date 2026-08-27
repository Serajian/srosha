package matrix_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/matrix"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	token      = "syt-not-a-real-token"
	room       = "!abcdef:matrix.test"
	deliveryID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8001")
)

type request struct {
	method string
	path   string
	auth   string
	body   map[string]any
	seen   bool
}

// homeserver stands in for one. It is TLS because the sender refuses anything
// else, and that requirement is worth running rather than working around: an
// access token over plain http is a token in the clear.
func homeserver(t *testing.T, status int, answer string) (*matrix.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.seen = true
		got.method, got.path = r.Method, r.URL.Path
		got.auth = r.Header.Get("Authorization")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	s, err := matrix.New(server.Client(), token, matrix.Config{Homeserver: server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, got
}

func msg(title, body string) shared.Message {
	return shared.Message{
		DeliveryID: deliveryID,
		Recipient:  shared.Recipient{Channel: shared.ChannelMatrix, Address: room},
		Title:      title,
		Body:       body,
	}
}

const okAnswer = `{"event_id":"$abc123:matrix.test"}`

func TestASentMessageComesBackWithItsEventID(t *testing.T) {
	s, got := homeserver(t, http.StatusOK, okAnswer)

	id, err := s.Send(context.Background(), msg("Deploy", "it is done"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "$abc123:matrix.test" {
		t.Errorf("id = %q, want the event id", id)
	}
	if got.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", got.method)
	}
	if got.auth != "Bearer "+token {
		t.Errorf("authorization = %q", got.auth)
	}
	if got.body["msgtype"] != "m.text" {
		t.Errorf("msgtype = %v", got.body["msgtype"])
	}
	if got.body["body"] != "Deploy\n\nit is done" {
		t.Errorf("body = %v", got.body["body"])
	}
}

// The delivery id is the transaction id, and that is why it is on the message:
// a homeserver will not make a second event for a transaction it has seen, so a
// redelivered event does not write the message into the room twice.
func TestTheDeliveryIDIsTheTransaction(t *testing.T) {
	s, got := homeserver(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.HasSuffix(got.path, "/"+deliveryID.String()) {
		t.Errorf("path = %q, want it to end in the delivery id", got.path)
	}
}

// A room id begins with "!" and carries a ":", neither of which survives a path
// unencoded.
func TestTheRoomIsEscapedIntoThePath(t *testing.T) {
	s, got := homeserver(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// net/http decodes the path before a handler sees it, so what arrives is the
	// room again -- which is the proof that it survived the trip intact.
	if !strings.Contains(got.path, room) {
		t.Errorf("path = %q, want the room to arrive whole", got.path)
	}
}

// Without an id every attempt would be a new transaction, and a retry would put
// the message in the room again.
func TestAMessageWithNoIDIsRefused(t *testing.T) {
	s, got := homeserver(t, http.StatusOK, okAnswer)

	m := msg("", "hello")
	m.DeliveryID = ""

	if _, err := s.Send(context.Background(), m); err == nil {
		t.Fatal("Send succeeded with nothing to deduplicate on")
	}
	if got.seen {
		t.Error("the request was made anyway")
	}
}

// The one question the core asks of every failure.
func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	tests := map[string]struct {
		status int
		answer string
		kind   shared.SendKind
	}{
		"not in the room": {
			http.StatusForbidden,
			`{"errcode":"M_FORBIDDEN","error":"You are not invited to this room"}`,
			shared.SendUnreachable,
		},
		"no such room": {
			http.StatusNotFound,
			`{"errcode":"M_NOT_FOUND","error":"Room not found"}`,
			shared.SendUnreachable,
		},
		"token refused": {
			http.StatusUnauthorized,
			`{"errcode":"M_UNKNOWN_TOKEN","error":"Invalid access token"}`,
			shared.SendPermanent,
		},
		"no token at all": {
			http.StatusUnauthorized,
			`{"errcode":"M_MISSING_TOKEN","error":"Missing access token"}`,
			shared.SendPermanent,
		},
		"too much, too fast": {
			http.StatusTooManyRequests,
			`{"errcode":"M_LIMIT_EXCEEDED","error":"Too many requests","retry_after_ms":2000}`,
			shared.SendTransient,
		},
		"too big": {
			http.StatusBadRequest,
			`{"errcode":"M_TOO_LARGE","error":"Event too large"}`,
			shared.SendPermanent,
		},
		"their fault": {
			http.StatusBadGateway,
			`{"errcode":"M_UNKNOWN","error":"Internal server error"}`,
			shared.SendTransient,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := homeserver(t, tt.status, tt.answer)

			_, err := s.Send(context.Background(), msg("", "hello"))
			if err == nil {
				t.Fatal("Send succeeded")
			}
			if got := shared.SendKindOf(err); got != tt.kind {
				t.Errorf("kind = %v, want %v (%v)", got, tt.kind, err)
			}
		})
	}
}

// Matrix states its wait in milliseconds, so it is used rather than a guess.
func TestTheStatedWaitIsUsed(t *testing.T) {
	s, _ := homeserver(t, http.StatusTooManyRequests,
		`{"errcode":"M_LIMIT_EXCEEDED","error":"slow down","retry_after_ms":2500}`)

	_, err := s.Send(context.Background(), msg("", "hello"))
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if got := shared.SendRetryAfter(err); got.Milliseconds() != 2500 {
		t.Errorf("retry after = %v, want the stated 2.5s", got)
	}
}

func TestAHomeserverWeCannotReachIsWorthAnotherTry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, url := server.Client(), server.URL
	server.Close()

	s, err := matrix.New(client, token, matrix.Config{Homeserver: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := s.Send(context.Background(), msg("", "hello")); err == nil {
		t.Fatal("Send succeeded against a closed server")
	} else if shared.IsPermanentSend(err) {
		t.Errorf("a dial failure was called final: %v", err)
	}
}

// The homeserver is the one address in this service that a source chooses, so it
// is the one that has to be checked rather than trusted.
func TestAHomeserverThatCannotBeUsed(t *testing.T) {
	cases := map[string]string{
		"empty":            ``,
		"plain http":       `{"homeserver":"http://matrix.test"}`,
		"carries a path":   `{"homeserver":"https://matrix.test/_matrix"}`,
		"carries a user":   `{"homeserver":"https://user:pw@matrix.test"}`,
		"carries a query":  `{"homeserver":"https://matrix.test?a=1"}`,
		"has no host":      `{"homeserver":"https://"}`,
		"is not a url":     `{"homeserver":"://"}`,
		"is not even json": `homeserver=x`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := matrix.ParseConfig([]byte(raw)); !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("ParseConfig() = %v, want invalid input", err)
			}
		})
	}

	// A trailing slash is a typo, not a mistake worth refusing.
	got, err := matrix.ParseConfig([]byte(`{"homeserver":"https://matrix.test/"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got.Homeserver != "https://matrix.test" {
		t.Errorf("Homeserver = %q, want the slash gone", got.Homeserver)
	}
}

func TestASenderNeedsATokenAndAClient(t *testing.T) {
	cfg := matrix.Config{Homeserver: "https://matrix.test"}

	if _, err := matrix.New(http.DefaultClient, "", cfg); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New() with no token = %v, want invalid input", err)
	}
	if _, err := matrix.New(nil, token, cfg); err == nil {
		t.Error("New() with no client succeeded")
	}
}

func TestTheChannelIsMatrix(t *testing.T) {
	s, _ := homeserver(t, http.StatusOK, okAnswer)
	if got := s.Channel(); got != shared.ChannelMatrix {
		t.Errorf("Channel() = %q", got)
	}
}
