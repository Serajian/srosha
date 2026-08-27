package whatsapp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/whatsapp"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	token       = "EAAG-not-a-real-token"
	phoneNumber = "123456789"
)

type request struct {
	path string
	auth string
	body map[string]any
}

// meta stands in for the Cloud API: it records what it was asked and answers
// with whatever the test wants.
func meta(t *testing.T, status int, answer string) (*whatsapp.Sender, *request) {
	t.Helper()
	return metaWith(t, status, answer, []byte(`{"phone_number_id":"`+phoneNumber+`"}`))
}

func metaWith(t *testing.T, status int, answer string, config []byte) (*whatsapp.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	cfg, err := whatsapp.ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	s, err := whatsapp.New(server.Client(), token, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.WithBaseURL(server.URL), got
}

func msg(title, body string, metadata map[string]string) shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelWhatsApp, Address: "+989121234567"},
		Title:     title,
		Body:      body,
		Metadata:  metadata,
	}
}

const okAnswer = `{"messaging_product":"whatsapp","messages":[{"id":"wamid.HBgLABC"}]}`

func TestASentMessageComesBackWithItsID(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	id, err := s.Send(context.Background(), msg("Deploy", "it is done", nil))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "wamid.HBgLABC" {
		t.Errorf("id = %q, want the id whatsapp gave", id)
	}
	if !strings.Contains(got.path, "/"+phoneNumber+"/messages") {
		t.Errorf("path = %q", got.path)
	}
}

// The token travels in a header, not the path -- which is the one real
// difference from the bot channels, and why a url from here is not a secret.
func TestTheTokenTravelsInAHeader(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello", nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.auth != "Bearer "+token {
		t.Errorf("authorization = %q", got.auth)
	}
	if strings.Contains(got.path, token) {
		t.Errorf("the token is in the path: %q", got.path)
	}
}

// Everywhere else in this service a phone number is E.164 and carries a plus.
// Meta wants digits.
func TestTheRecipientLosesItsPlus(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("", "hello", nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.body["to"] != "989121234567" {
		t.Errorf("to = %v, want the digits alone", got.body["to"])
	}
}

// A message carrying no template is text, which is what a source inside the
// window can send.
func TestAMessageWithNoTemplateIsText(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	if _, err := s.Send(context.Background(), msg("Deploy", "it is done", nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.body["type"] != "text" {
		t.Fatalf("type = %v, want text", got.body["type"])
	}

	text, _ := got.body["text"].(map[string]any)
	if text["body"] != "Deploy\n\nit is done" {
		t.Errorf("body = %v", text["body"])
	}
	if text["preview_url"] != false {
		t.Error("a body containing a link should not become a card nobody asked for")
	}
}

// And the source says which template through metadata, because it knows whether
// the recipient wrote to them recently and we do not.
func TestATemplateComesFromMetadata(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	_, err := s.Send(context.Background(), msg("", "ignored", map[string]string{
		"template":          "order_shipped",
		"template_language": "fa",
		"template_params":   `["سفارش ۴۲","فردا"]`,
	}))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.body["type"] != "template" {
		t.Fatalf("type = %v, want template", got.body["type"])
	}

	tpl, _ := got.body["template"].(map[string]any)
	if tpl["name"] != "order_shipped" {
		t.Errorf("name = %v", tpl["name"])
	}
	if lang, _ := tpl["language"].(map[string]any); lang["code"] != "fa" {
		t.Errorf("language = %v", tpl["language"])
	}

	comps, _ := tpl["components"].([]any)
	if len(comps) != 1 {
		t.Fatalf("components = %v, want one body component", comps)
	}
	first, _ := comps[0].(map[string]any)
	params, _ := first["parameters"].([]any)
	if len(params) != 2 {
		t.Fatalf("parameters = %v, want two", params)
	}
	if p, _ := params[0].(map[string]any); p["text"] != "سفارش ۴۲" {
		t.Errorf("first parameter = %v", params[0])
	}
}

func TestATemplateWithoutParametersSendsNoComponents(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	_, err := s.Send(context.Background(), msg("", "x", map[string]string{"template": "hello_world"}))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	tpl, _ := got.body["template"].(map[string]any)
	if _, has := tpl["components"]; has {
		t.Errorf("components = %v, want them absent", tpl["components"])
	}
	if lang, _ := tpl["language"].(map[string]any); lang["code"] != "en_US" {
		t.Errorf("language = %v, want the default", tpl["language"])
	}
}

// Parameters have an order, so they travel as json rather than in a format that
// cannot hold one. A value that is not json is the source's mistake and reads
// the same on every retry.
func TestParametersThatAreNotJSON(t *testing.T) {
	s, got := meta(t, http.StatusOK, okAnswer)

	_, err := s.Send(context.Background(), msg("", "x", map[string]string{
		"template":        "order_shipped",
		"template_params": "a,b,c",
	}))
	if err == nil {
		t.Fatal("Send succeeded with parameters that are not json")
	}
	if !shared.IsPermanentSend(err) {
		t.Error("it will read the same next time")
	}
	if got.path != "" {
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
		"outside the window": {
			http.StatusBadRequest,
			`{"error":{"message":"Re-engagement message","code":131047}}`,
			shared.SendUnreachable,
		},
		"not on whatsapp": {
			http.StatusBadRequest,
			`{"error":{"message":"Message undeliverable","code":131026}}`,
			shared.SendUnreachable,
		},
		"template not approved": {
			http.StatusBadRequest,
			`{"error":{"message":"Template name does not exist","code":132001}}`,
			shared.SendPermanent,
		},
		"token refused": {
			http.StatusUnauthorized,
			`{"error":{"message":"Invalid OAuth access token","code":190}}`,
			shared.SendPermanent,
		},
		"too many": {
			http.StatusTooManyRequests,
			`{"error":{"message":"Rate limit hit","code":130429}}`,
			shared.SendTransient,
		},
		"their fault": {
			http.StatusBadGateway,
			`{"error":{"message":"Bad Gateway","code":1}}`,
			shared.SendTransient,
		},
		"an error with a 200": {
			http.StatusOK,
			`{"error":{"message":"something undocumented","code":9999}}`,
			shared.SendTransient,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			s, _ := meta(t, tt.status, tt.answer)

			_, err := s.Send(context.Background(), msg("", "hello", nil))
			if err == nil {
				t.Fatal("Send succeeded")
			}
			if got := shared.SendKindOf(err); got != tt.kind {
				t.Errorf("kind = %v, want %v (%v)", got, tt.kind, err)
			}
		})
	}
}

// A recipient outside the window is not the same answer as a template that does
// not exist: the source can act on one and cannot act on the other.
func TestOutsideTheWindowStopsTheRetriesToo(t *testing.T) {
	s, _ := meta(t, http.StatusBadRequest,
		`{"error":{"message":"Re-engagement message","code":131047}}`)

	_, err := s.Send(context.Background(), msg("", "hello", nil))
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if !shared.IsPermanentSend(err) {
		t.Error("it should stop the retries like any other final answer")
	}
}

func TestAProviderWeCannotReachIsWorthAnotherTry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	s, err := whatsapp.New(http.DefaultClient, token, whatsapp.Config{PhoneNumberID: phoneNumber})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := s.WithBaseURL(url).Send(context.Background(), msg("", "hello", nil)); err == nil {
		t.Fatal("Send succeeded against a closed server")
	} else if shared.IsPermanentSend(err) {
		t.Errorf("a dial failure was called final: %v", err)
	}
}

func TestSettingsThatCannotBeUsed(t *testing.T) {
	cases := map[string][]byte{
		"no id":            []byte(`{}`),
		"nothing at all":   nil,
		"id with a slash":  []byte(`{"phone_number_id":"123/../evil"}`),
		"id with a letter": []byte(`{"phone_number_id":"12a"}`),
		"not json":         []byte("phone_number_id=1"),
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := whatsapp.ParseConfig(config); !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("ParseConfig() = %v, want invalid input", err)
			}
		})
	}

	good := whatsapp.Config{PhoneNumberID: phoneNumber}
	if _, err := whatsapp.New(http.DefaultClient, "", good); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("New() with no token = %v, want invalid input", err)
	}
	if _, err := whatsapp.New(nil, token, good); err == nil {
		t.Error("New() with no client succeeded")
	}
}

// An account id is somebody's, so it does not travel into an error a person
// reads.
func TestTheAccountIDIsNotEchoed(t *testing.T) {
	_, err := whatsapp.ParseConfig([]byte(`{"phone_number_id":"999secret999"}`))
	if err == nil {
		t.Fatal("ParseConfig() accepted an id with letters in it")
	}
	if strings.Contains(err.Error(), "999secret999") {
		t.Errorf("the id is in the error: %v", err)
	}
}

// Meta quotes the credential back in its own error message. That message is
// written to the delivery row, so the answer leaks the token if nothing strips
// it -- the opposite direction from the bot channels, where the request did.
func TestTheTokenIsStrippedFromWhatMetaSaysBack(t *testing.T) {
	s, _ := meta(t, http.StatusUnauthorized,
		`{"error":{"message":"Malformed access token `+token+`","code":190}}`)

	_, err := s.Send(context.Background(), msg("", "hello", nil))
	if err == nil {
		t.Fatal("Send succeeded")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token came back in the error: %v", err)
	}
}

func TestTheChannelIsWhatsApp(t *testing.T) {
	s, _ := meta(t, http.StatusOK, okAnswer)
	if got := s.Channel(); got != shared.ChannelWhatsApp {
		t.Errorf("Channel() = %q", got)
	}
}
