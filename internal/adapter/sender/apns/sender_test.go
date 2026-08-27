package apns_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender/apns"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	device     = "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4"
	signed     = "eyJhbGciOiJFUzI1NiJ9.claims.signature"
	deliveryID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8001")
)

func settings() apns.Config {
	return apns.Config{
		KeyID: "ABC1234567", TeamID: "TEAM123456", Topic: "com.acme.app",
	}
}

// tokens stands in for appleauth, and counts how often the held token was
// thrown away.
type tokens struct {
	token   string
	err     error
	asked   int
	expired int
}

func (t *tokens) Token() (string, error) {
	t.asked++
	if t.err != nil {
		return "", t.err
	}
	return t.token, nil
}

func (t *tokens) Expire() { t.expired++ }

type request struct {
	method  string
	path    string
	headers http.Header
	body    map[string]any
	seen    bool
}

func apple(t *testing.T, status int, answer string) (*apns.Sender, *request) {
	t.Helper()
	return appleWith(t, &tokens{token: signed}, settings(), status, answer)
}

func appleWith(
	t *testing.T, mint apns.Tokens, cfg apns.Config, status int, answer string,
) (*apns.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.seen = true
		got.method, got.path, got.headers = r.Method, r.URL.Path, r.Header.Clone()

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)

		// APNs answers with the id in a header both ways, and no success body.
		if id := r.Header.Get("apns-id"); id != "" {
			w.Header().Set("apns-id", id)
		} else {
			w.Header().Set("apns-id", "8B1E7A50-0000-0000-0000-000000000000")
		}
		w.WriteHeader(status)
		if answer != "" {
			_, _ = io.WriteString(w, answer)
		}
	}))
	t.Cleanup(server.Close)

	s, err := apns.New(server.Client(), mint, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.PointAt(server.URL)
	return s, got
}

func msg(title, body string) shared.Message {
	return shared.Message{
		DeliveryID: deliveryID,
		Recipient:  shared.Recipient{Channel: shared.ChannelAPNs, Address: device},
		Title:      title,
		Body:       body,
	}
}

func TestASentMessageComesBackWithItsID(t *testing.T) {
	s, got := apple(t, http.StatusOK, "")

	id, err := s.Send(context.Background(), msg("Hello", "world"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	want, _ := apns.NotificationID(deliveryID)
	if id != want {
		t.Errorf("provider id = %q, want the delivery id as a uuid (%q)", id, want)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/3/device/"+device {
		t.Errorf("path = %q, want the device token in it", got.path)
	}
	if got.headers.Get("Authorization") != "bearer "+signed {
		t.Errorf("authorization = %q, want the signed token", got.headers.Get("Authorization"))
	}
	if got.headers.Get("apns-topic") != "com.acme.app" {
		t.Errorf("apns-topic = %q, want the bundle id", got.headers.Get("apns-topic"))
	}
	if got.headers.Get("apns-push-type") != "alert" {
		t.Errorf("apns-push-type = %q, want alert", got.headers.Get("apns-push-type"))
	}

	aps, _ := got.body["aps"].(map[string]any)
	alert, _ := aps["alert"].(map[string]any)
	if alert["title"] != "Hello" || alert["body"] != "world" {
		t.Errorf("alert = %v, want the title and body separately", alert)
	}
}

// A delivery id and a UUID are both 128 bits, so this is a rewriting rather
// than a mapping -- the same value in the shape Apple asks for.
func TestTheDeliveryIDBecomesTheAPNsID(t *testing.T) {
	s, got := apple(t, http.StatusOK, "")

	if _, err := s.Send(context.Background(), msg("Hello", "world")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	id := got.headers.Get("apns-id")
	if len(id) != 36 {
		t.Fatalf("apns-id = %q, want a canonical uuid", id)
	}
	for _, at := range []int{8, 13, 18, 23} {
		if id[at] != '-' {
			t.Fatalf("apns-id = %q, want dashes at 8-4-4-4-12", id)
		}
	}
	if strings.ContainsAny(id, "GHIJKLMNOPQRSTUVWXYZ") {
		t.Errorf("apns-id = %q, want hexadecimal", id)
	}
}

// Nothing is lost by sending no header: Apple invents an id. So a delivery id
// that is somehow not a ULID costs correlation, not a message.
func TestAMessageWithNoUsableIDStillGoesOut(t *testing.T) {
	s, got := apple(t, http.StatusOK, "")

	m := msg("Hello", "world")
	m.DeliveryID = ""

	if _, err := s.Send(context.Background(), m); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.headers.Get("apns-id") != "" {
		t.Error("an apns-id was sent for a delivery that has no id")
	}
}

func TestMetadataSitsBesideAPSNotInsideIt(t *testing.T) {
	s, got := apple(t, http.StatusOK, "")

	m := msg("Hello", "world")
	m.Metadata = map[string]string{"order_id": "42", "deep_link": "app://orders/42"}

	if _, err := s.Send(context.Background(), m); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got.body["order_id"] != "42" || got.body["deep_link"] != "app://orders/42" {
		t.Errorf("payload = %v, want the metadata at the top level", got.body)
	}
	if _, ok := got.body["aps"]; !ok {
		t.Error("aps went missing")
	}
}

func TestTheAPSKeyIsRefusedBeforeTheCall(t *testing.T) {
	for _, key := range []string{"aps", "APS"} {
		t.Run(key, func(t *testing.T) {
			s, got := apple(t, http.StatusOK, "")

			m := msg("Hello", "world")
			m.Metadata = map[string]string{key: "x"}

			if _, err := s.Send(context.Background(), m); err == nil {
				t.Fatal("Send: want a refusal")
			}
			if got.seen {
				t.Error("the message was sent anyway")
			}
		})
	}
}

func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reason string
		want   shared.SendKind
	}{
		{"a token for another environment", 400, "BadDeviceToken", shared.SendUnreachable},
		{"the app was removed", 410, "Unregistered", shared.SendUnreachable},
		{"a token for another app", 400, "DeviceTokenNotForTopic", shared.SendUnreachable},
		{"our provider token aged out", 403, "ExpiredProviderToken", shared.SendTransient},
		{"we signed too often", 429, "TooManyProviderTokenUpdates", shared.SendTransient},
		{"too much for one device", 429, "TooManyRequests", shared.SendTransient},
		{"apple is down", 503, "ServiceUnavailable", shared.SendTransient},
		{"a key apple does not know", 403, "InvalidProviderToken", shared.SendPermanent},
		{"a topic that is not ours", 400, "TopicDisallowed", shared.SendPermanent},
		{"more than apns carries", 413, "PayloadTooLarge", shared.SendPermanent},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := apple(t, c.status, `{"reason":"`+c.reason+`"}`)

			_, err := s.Send(context.Background(), msg("Hello", "world"))
			if err == nil {
				t.Fatal("Send: want a refusal")
			}
			if got := shared.SendKindOf(err); got != c.want {
				t.Errorf("kind = %v, want %v", got, c.want)
			}
			if !strings.Contains(err.Error(), c.reason) {
				t.Errorf("detail = %q, want the reason in it", err)
			}
		})
	}
}

// ExpiredProviderToken is only worth another attempt with a different token.
// Without this the cache would present the same stale one until the attempts
// ran out.
func TestAnExpiredProviderTokenIsThrownAway(t *testing.T) {
	mint := &tokens{token: signed}
	s, _ := appleWith(t, mint, settings(), http.StatusForbidden,
		`{"reason":"ExpiredProviderToken"}`)

	if _, err := s.Send(context.Background(), msg("Hello", "world")); err == nil {
		t.Fatal("Send: want a refusal")
	}
	if mint.expired != 1 {
		t.Errorf("the held token was discarded %d times, want 1", mint.expired)
	}
}

// The opposite treatment, and it matters: signing another is what caused this.
func TestSigningTooOftenDoesNotDiscardTheToken(t *testing.T) {
	mint := &tokens{token: signed}
	s, _ := appleWith(t, mint, settings(), http.StatusTooManyRequests,
		`{"reason":"TooManyProviderTokenUpdates"}`)

	if _, err := s.Send(context.Background(), msg("Hello", "world")); err == nil {
		t.Fatal("Send: want a refusal")
	}
	if mint.expired != 0 {
		t.Errorf("the held token was discarded %d times, want none", mint.expired)
	}
}

// Unregistered is the one refusal that says anything beyond "stop sending here".
func TestWhenTheAppWasRemovedIsKept(t *testing.T) {
	s, _ := apple(t, http.StatusGone,
		`{"reason":"Unregistered","timestamp":1700000000000}`)

	_, err := s.Send(context.Background(), msg("Hello", "world"))
	if !strings.Contains(err.Error(), "1700000000000") {
		t.Errorf("detail = %q, want the timestamp in it", err)
	}
}

func TestASenderPicksTheEnvironment(t *testing.T) {
	cfg := settings()
	cfg.Environment = "sandbox"

	s, err := apns.New(http.DefaultClient, &tokens{token: signed}, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !strings.Contains(s.Host(), "sandbox") {
		t.Errorf("host = %q, want the sandbox service", s.Host())
	}

	// The default, because it is the one a running app uses.
	plain, err := apns.New(http.DefaultClient, &tokens{token: signed}, settings())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if strings.Contains(plain.Host(), "sandbox") {
		t.Errorf("host = %q, want production by default", plain.Host())
	}
}

func TestWhatThisSenderRefusesOnItsOwn(t *testing.T) {
	cases := []struct {
		name string
		msg  shared.Message
	}{
		{"no device token", msg("Hello", "world")},
		{"a device token that is not hex", msg("Hello", "world")},
		{"a device token pasted short", msg("Hello", "world")},
		{"nothing to read", msg("Hello", "   ")},
		{"more than apns carries", msg("Hello", strings.Repeat("a", 5000))},
	}
	cases[0].msg.Recipient.Address = ""
	cases[1].msg.Recipient.Address = strings.Repeat("z", 64)
	cases[2].msg.Recipient.Address = "a1b2"

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, got := apple(t, http.StatusOK, "")

			_, err := s.Send(context.Background(), c.msg)
			if err == nil {
				t.Fatal("Send: want a refusal")
			}
			if shared.SendKindOf(err) != shared.SendPermanent {
				t.Errorf("kind = %v, want permanent", shared.SendKindOf(err))
			}
			if got.seen {
				t.Error("the message was sent anyway")
			}
		})
	}
}

// Nothing is spent on a message that was going to be refused anyway.
func TestNoTokenIsSignedForAMessageWeRefuse(t *testing.T) {
	mint := &tokens{token: signed}
	s, _ := appleWith(t, mint, settings(), http.StatusOK, "")

	if _, err := s.Send(context.Background(), msg("Hello", "")); err == nil {
		t.Fatal("Send: want a refusal")
	}
	if mint.asked != 0 {
		t.Errorf("signed %d tokens, want none", mint.asked)
	}
}

// Signing is local arithmetic, so a second attempt does the same thing.
func TestATokenWeCouldNotSignIsFinal(t *testing.T) {
	mint := &tokens{err: errors.New("appleauth: could not sign a provider token")}
	s, got := appleWith(t, mint, settings(), http.StatusOK, "")

	_, err := s.Send(context.Background(), msg("Hello", "world"))
	if shared.SendKindOf(err) != shared.SendPermanent {
		t.Errorf("kind = %v, want permanent", shared.SendKindOf(err))
	}
	if got.seen {
		t.Error("a message went out without a token")
	}
}

func TestSettingsThatCannotBeUsed(t *testing.T) {
	cases := map[string]apns.Config{
		"no key id":    {TeamID: "TEAM123456", Topic: "com.acme.app"},
		"no team id":   {KeyID: "ABC1234567", Topic: "com.acme.app"},
		"no topic":     {KeyID: "ABC1234567", TeamID: "TEAM123456"},
		"a third apns": {KeyID: "A", TeamID: "T", Topic: "c", Environment: "staging"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := apns.New(http.DefaultClient, &tokens{token: signed}, cfg)
			if err == nil {
				t.Fatal("New: want an error")
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("error = %v, want invalid input", err)
			}
		})
	}
}

func TestASenderNeedsAClientAndTokens(t *testing.T) {
	if _, err := apns.New(nil, &tokens{}, settings()); err == nil {
		t.Error("New with no client: want an error")
	}
	if _, err := apns.New(http.DefaultClient, nil, settings()); err == nil {
		t.Error("New with no tokens: want an error")
	}
}

func TestTheChannelIsAPNs(t *testing.T) {
	s, _ := apple(t, http.StatusOK, "")
	if s.Channel() != shared.ChannelAPNs {
		t.Errorf("channel = %q, want apns", s.Channel())
	}
}
