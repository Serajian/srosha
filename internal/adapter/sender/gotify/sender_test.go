package gotify_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/gotify"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	token         = "AbCdEf.not-a-real-token"
	applicationID = "42"
)

type request struct {
	method string
	path   string
	query  url.Values
	body   map[string]any
	seen   bool
}

// gotifyServer stands in for one. It is TLS because the sender refuses
// anything else, and that requirement is worth running rather than working
// around: the application token travels in the query string, which is a
// token in the clear over plain http.
func gotifyServer(t *testing.T, status int, answer string) (*gotify.Sender, *request) {
	t.Helper()
	return gotifyServerWith(t, gotify.Config{}, status, answer)
}

// gotifyServerWith is the same, for the tests that care what the credential
// says rather than only what comes back.
func gotifyServerWith(
	t *testing.T, cfg gotify.Config, status int, answer string,
) (*gotify.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.seen = true
		got.method, got.path = r.Method, r.URL.Path
		got.query = r.URL.Query()

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	cfg.ServerURL = server.URL
	s, err := gotify.New(server.Client(), token, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, got
}

func msg(title, body string) shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelGotify, Address: applicationID},
		Title:     title,
		Body:      body,
	}
}

const okAnswer = `{"id":4242}`

func TestASentMessageComesBackWithItsID(t *testing.T) {
	s, got := gotifyServer(t, http.StatusOK, okAnswer)

	id, err := s.Send(context.Background(), msg("Deploy", "it is done"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "4242" {
		t.Errorf("id = %q, want the id gotify gave", id)
	}
	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/message" {
		t.Errorf("path = %q", got.path)
	}
}

// Gotify has its own title field, unlike the bot channels and Matrix, so a
// title and a body never have to be merged into one string.
func TestTitleAndBodyStayInTheirOwnFields(t *testing.T) {
	s, got := gotifyServer(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("Deploy", "it is done")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.body["title"] != "Deploy" {
		t.Errorf("title = %v", got.body["title"])
	}
	if got.body["message"] != "it is done" {
		t.Errorf("message = %v", got.body["message"])
	}
}

// The documented shape: the token decides which application receives the
// message. See (*Sender).endpoint for the assumption this rests on.
func TestTheTokenTravelsAsTheDocumentedQueryParameter(t *testing.T) {
	s, got := gotifyServer(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := got.query.Get("token"); got != token {
		t.Errorf("token = %q, want %q", got, token)
	}
}

// The address is never silently dropped, whatever the token alone would
// imply -- it is sent as an additional parameter. See (*Sender).endpoint.
func TestTheApplicationIDIsNeverDropped(t *testing.T) {
	s, got := gotifyServer(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := got.query.Get("appid"); got != applicationID {
		t.Errorf("appid = %q, want %q", got, applicationID)
	}
}

func TestAMessageWithNoApplicationIDIsRefused(t *testing.T) {
	s, got := gotifyServer(t, http.StatusOK, okAnswer)

	m := msg("", "hello")
	m.Recipient.Address = ""

	if _, err := s.Send(context.Background(), m); err == nil {
		t.Fatal("Send succeeded with no application id")
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
		"token not recognized": {
			http.StatusUnauthorized,
			`{"error":"Unauthorized","errorCode":401,"errorDescription":"invalid token"}`,
			shared.SendPermanent,
		},
		"token forbidden": {
			http.StatusForbidden,
			`{"error":"Forbidden","errorCode":403,"errorDescription":"not allowed"}`,
			shared.SendPermanent,
		},
		"too much, too fast": {
			http.StatusTooManyRequests,
			`{"error":"TooManyRequests","errorCode":429,"errorDescription":"slow down"}`,
			shared.SendTransient,
		},
		"malformed request": {
			http.StatusBadRequest,
			`{"error":"BadRequest","errorCode":400,"errorDescription":"missing message"}`,
			shared.SendPermanent,
		},
		"their fault": {
			http.StatusBadGateway,
			`{"error":"BadGateway","errorCode":502,"errorDescription":"internal error"}`,
			shared.SendTransient,
		},
		"an undocumented body": {
			http.StatusInternalServerError,
			`not even json`,
			shared.SendTransient,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := gotifyServer(t, tt.status, tt.answer)

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

func TestAServerWeCannotReachIsWorthAnotherTry(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, url := server.Client(), server.URL
	server.Close()

	s, err := gotify.New(client, token, gotify.Config{ServerURL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := s.Send(context.Background(), msg("", "hello")); err == nil {
		t.Fatal("Send succeeded against a closed server")
	} else if shared.IsPermanentSend(err) {
		t.Errorf("a dial failure was called final: %v", err)
	}
}

// The server url is the one address in this service a source chooses for this
// channel, so it is the one that has to be checked rather than trusted.
func TestAServerURLThatCannotBeUsed(t *testing.T) {
	cases := map[string]string{
		"empty":            ``,
		"plain http":       `{"server_url":"http://gotify.test"}`,
		"carries a path":   `{"server_url":"https://gotify.test/gotify"}`,
		"carries a user":   `{"server_url":"https://user:pw@gotify.test"}`,
		"carries a query":  `{"server_url":"https://gotify.test?a=1"}`,
		"has no host":      `{"server_url":"https://"}`,
		"is not a url":     `{"server_url":"://"}`,
		"is not even json": `server_url=x`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := gotify.ParseConfig([]byte(raw)); !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("ParseConfig() = %v, want invalid input", err)
			}
		})
	}

	// A trailing slash is a typo, not a mistake worth refusing.
	got, err := gotify.ParseConfig([]byte(`{"server_url":"https://gotify.test/"}`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got.ServerURL != "https://gotify.test" {
		t.Errorf("ServerURL = %q, want the slash gone", got.ServerURL)
	}
}

func TestASenderNeedsATokenAndAClient(t *testing.T) {
	cfg := gotify.Config{ServerURL: "https://gotify.test"}

	if _, err := gotify.New(http.DefaultClient, "", cfg); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New() with no token = %v, want invalid input", err)
	}
	if _, err := gotify.New(nil, token, cfg); err == nil {
		t.Error("New() with no client succeeded")
	}
}

func TestTheChannelIsGotify(t *testing.T) {
	s, _ := gotifyServer(t, http.StatusOK, okAnswer)
	if got := s.Channel(); got != shared.ChannelGotify {
		t.Errorf("Channel() = %q", got)
	}
}

// Nothing about a valid application id is echoed into a message that fails,
// and the token never appears in a returned error either -- both travel in a
// url that unreachable() drops rather than quotes.
func TestNeitherSecretReachesAnError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, url := server.Client(), server.URL
	server.Close()

	s, err := gotify.New(client, token, gotify.Config{ServerURL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Send(context.Background(), msg("", "hello"))
	if err == nil {
		t.Fatal("Send succeeded against a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token is in the error: %v", err)
	}
}

// Plain is Gotify's own default, so saying it out loud would put a key on the
// wire for every message this service has ever sent, to state what was already
// true. Absent is the whole point.
func TestPlainSendsNoExtrasAtAll(t *testing.T) {
	for _, cfg := range []gotify.Config{{}, {ContentType: "text/plain"}} {
		s, got := gotifyServerWith(t, cfg, http.StatusOK, okAnswer)

		if _, err := s.Send(context.Background(), msg("Deploy", "it is done")); err != nil {
			t.Fatalf("Send: %v", err)
		}
		if _, ok := got.body["extras"]; ok {
			t.Errorf("plain put extras on the wire: %v", got.body["extras"])
		}
	}
}

// The shape Gotify documents, exactly: extras, then the client::display key,
// then contentType. A typo in any of the three is a message that renders as
// plain text and says nothing about why.
func TestMarkdownAsksTheClientToRenderIt(t *testing.T) {
	s, got := gotifyServerWith(t,
		gotify.Config{ContentType: "text/markdown"}, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("Deploy", "**it is done**")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	extras, ok := got.body["extras"].(map[string]any)
	if !ok {
		t.Fatalf("no extras on the wire: %v", got.body)
	}
	shown, ok := extras["client::display"].(map[string]any)
	if !ok {
		t.Fatalf("extras has no client::display: %v", extras)
	}
	if shown["contentType"] != "text/markdown" {
		t.Errorf("contentType = %v, want text/markdown", shown["contentType"])
	}

	// And the body is untouched: srosha does not escape, here or anywhere.
	if got.body["message"] != "**it is done**" {
		t.Errorf("the body was rewritten: %v", got.body["message"])
	}
}

// Registration is where a typo should be caught, not the first send.
func TestAnUnknownContentTypeIsRefusedAtRegistration(t *testing.T) {
	_, err := gotify.ParseConfig([]byte(
		`{"server_url":"https://gotify.acme.test","content_type":"text/html"}`))
	if err == nil {
		t.Fatal("a content type Gotify does not render was accepted")
	}
	if !strings.Contains(err.Error(), "content type") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}
