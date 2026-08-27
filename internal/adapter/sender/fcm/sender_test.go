package fcm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/sender/fcm"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/googleauth"
	"github.com/Serajian/srosha/pkg/errs"
)

const (
	projectID = "srosha-test"
	device    = "fCm-DeViCe-ToKeN-0123456789abcdef0123456789abcdef"
	minted    = "ya29.not-a-real-access-token"
)

// tokens stands in for googleauth. The real one signs an assertion and asks
// Google; this one only has to answer.
type tokens struct {
	token string
	err   error
	asked int
}

func (t *tokens) Token(context.Context) (string, error) {
	t.asked++
	if t.err != nil {
		return "", t.err
	}
	return t.token, nil
}

type request struct {
	method string
	path   string
	auth   string
	body   map[string]any
	seen   bool
}

func firebase(t *testing.T, status int, answer string, header http.Header) (*fcm.Sender, *request) {
	t.Helper()
	return firebaseWith(t, &tokens{token: minted}, status, answer, header)
}

func firebaseWith(
	t *testing.T, mint fcm.Tokens, status int, answer string, header http.Header,
) (*fcm.Sender, *request) {
	t.Helper()

	got := &request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.seen = true
		got.method, got.path = r.Method, r.URL.Path
		got.auth = r.Header.Get("Authorization")

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)

		for k, v := range header {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(server.Close)

	s, err := fcm.New(server.Client(), mint, projectID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.PointAt(server.URL)
	return s, got
}

func msg(title, body string) shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelFCM, Address: device},
		Title:     title,
		Body:      body,
	}
}

const accepted = `{"name":"projects/srosha-test/messages/0:1500415314455276%31bd1c96"}`

func TestASentMessageComesBackWithItsName(t *testing.T) {
	s, got := firebase(t, http.StatusOK, accepted, nil)

	id, err := s.Send(context.Background(), msg("Hello", "world"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if want := "projects/srosha-test/messages/0:1500415314455276%31bd1c96"; id != want {
		t.Errorf("provider id = %q, want %q", id, want)
	}

	if got.method != http.MethodPost {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.auth != "Bearer "+minted {
		t.Errorf("authorization = %q, want the minted token", got.auth)
	}

	message, _ := got.body["message"].(map[string]any)
	if message["token"] != device {
		t.Errorf("token = %v, want the device token", message["token"])
	}

	note, _ := message["notification"].(map[string]any)
	if note["title"] != "Hello" || note["body"] != "world" {
		t.Errorf("notification = %v, want the title and body separately", note)
	}
}

// The title and body stay apart, unlike every channel before this one: a push
// notification has a field for each and the platform renders them differently.
func TestTitleAndBodyAreNotJoined(t *testing.T) {
	s, got := firebase(t, http.StatusOK, accepted, nil)

	if _, err := s.Send(context.Background(), msg("Subject", "Sentence")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	message, _ := got.body["message"].(map[string]any)
	note, _ := message["notification"].(map[string]any)
	if body, _ := note["body"].(string); strings.Contains(body, "Subject") {
		t.Errorf("body = %q, the title leaked into it", body)
	}
}

// The project id goes in the path, and it comes out of a file somebody else
// wrote, so it is escaped rather than trusted.
func TestTheProjectIsEscapedIntoThePath(t *testing.T) {
	s, err := fcm.New(http.DefaultClient, &tokens{token: minted}, "sneaky/../../v1/projects")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if strings.Contains(s.URL(), "/../") {
		t.Errorf("url = %q, the project walked out of its segment", s.URL())
	}
	if want := "sneaky%2F..%2F..%2Fv1%2Fprojects"; !strings.Contains(s.URL(), want) {
		t.Errorf("url = %q, want the project escaped into one segment", s.URL())
	}
}

func TestMetadataBecomesTheDataPayload(t *testing.T) {
	s, got := firebase(t, http.StatusOK, accepted, nil)

	m := msg("Hello", "world")
	m.Metadata = map[string]string{"order_id": "42", "deep_link": "app://orders/42"}

	if _, err := s.Send(context.Background(), m); err != nil {
		t.Fatalf("Send: %v", err)
	}

	message, _ := got.body["message"].(map[string]any)
	data, _ := message["data"].(map[string]any)
	if data["order_id"] != "42" || data["deep_link"] != "app://orders/42" {
		t.Errorf("data = %v, want the metadata carried through", data)
	}
}

// Refused rather than dropped: FCM would refuse the whole message for one of
// these, and a key that silently disappeared would be found out by nobody.
func TestAReservedDataKeyIsRefusedBeforeTheCall(t *testing.T) {
	for _, key := range []string{"from", "message_type", "notification", "google_x", "GCM_y"} {
		t.Run(key, func(t *testing.T) {
			s, got := firebase(t, http.StatusOK, accepted, nil)

			m := msg("Hello", "world")
			m.Metadata = map[string]string{key: "x"}

			_, err := s.Send(context.Background(), m)
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

func TestWhatIsWorthAnotherAttempt(t *testing.T) {
	fcmError := func(status int, code, name string) (int, string) {
		return status, `{"error":{"code":` + itoa(status) + `,"message":"nope","status":"` + name +
			`","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError",` +
			`"errorCode":"` + code + `"}]}}`
	}

	cases := []struct {
		name   string
		status int
		body   string
		want   shared.SendKind
	}{
		{"a token nobody is behind any more", 0, "", shared.SendUnreachable},
		{"a token from another project", 0, "", shared.SendUnreachable},
		{"over quota", 0, "", shared.SendTransient},
		{"firebase is down", 0, "", shared.SendTransient},
		{"an apns certificate we got wrong", 0, "", shared.SendPermanent},
		{"a message it will never accept", 0, "", shared.SendPermanent},
	}
	cases[0].status, cases[0].body = fcmError(404, "UNREGISTERED", "NOT_FOUND")
	cases[1].status, cases[1].body = fcmError(403, "SENDER_ID_MISMATCH", "PERMISSION_DENIED")
	cases[2].status, cases[2].body = fcmError(429, "QUOTA_EXCEEDED", "RESOURCE_EXHAUSTED")
	cases[3].status, cases[3].body = fcmError(503, "UNAVAILABLE", "UNAVAILABLE")
	cases[4].status, cases[4].body = fcmError(401, "THIRD_PARTY_AUTH_ERROR", "UNAUTHENTICATED")
	cases[5].status, cases[5].body = fcmError(400, "INVALID_ARGUMENT", "INVALID_ARGUMENT")

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := firebase(t, c.status, c.body, nil)

			_, err := s.Send(context.Background(), msg("Hello", "world"))
			if err == nil {
				t.Fatal("Send: want a refusal")
			}
			if got := shared.SendKindOf(err); got != c.want {
				t.Errorf("kind = %v, want %v", got, c.want)
			}
		})
	}
}

// A credential problem, not a device one: our own token was refused, so no
// retry helps and no source can act on it either.
func TestAnUnauthenticatedCallIsFinal(t *testing.T) {
	s, _ := firebase(t, http.StatusUnauthorized,
		`{"error":{"code":401,"message":"Request had invalid authentication credentials.",`+
			`"status":"UNAUTHENTICATED"}}`, nil)

	_, err := s.Send(context.Background(), msg("Hello", "world"))
	if shared.SendKindOf(err) != shared.SendPermanent {
		t.Errorf("kind = %v, want permanent", shared.SendKindOf(err))
	}
	if !strings.Contains(err.Error(), "UNAUTHENTICATED") {
		t.Errorf("detail = %q, want the status in it", err)
	}
}

func TestTheStatedWaitIsUsed(t *testing.T) {
	header := http.Header{"Retry-After": []string{"120"}}
	s, _ := firebase(t, http.StatusTooManyRequests,
		`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, header)

	_, err := s.Send(context.Background(), msg("Hello", "world"))

	var se *shared.SendError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want a SendError", err)
	}
	if se.RetryAfter != 2*time.Minute {
		t.Errorf("retry after = %v, want 2m", se.RetryAfter)
	}
}

// Transient on purpose. A key that is wrong fails when the credential is
// opened; a refusal here is Google being unreachable, and worth trying again.
func TestATokenWeCouldNotMintIsWorthAnotherTry(t *testing.T) {
	mint := &tokens{err: errors.New("dial tcp: connection refused")}
	s, got := firebaseWith(t, mint, http.StatusOK, accepted, nil)

	_, err := s.Send(context.Background(), msg("Hello", "world"))
	if shared.SendKindOf(err) != shared.SendTransient {
		t.Errorf("kind = %v, want transient", shared.SendKindOf(err))
	}
	if got.seen {
		t.Error("a message went out without a token")
	}
}

// Google refusing the account is not Google being unreachable. A key file that
// parses is not the same as an account that exists, and only the exchange knows
// the difference -- so this one must not be retried.
func TestAnAccountGoogleRefusedIsFinal(t *testing.T) {
	mint := &tokens{err: fmt.Errorf("googleauth: %w for bot@p.test: 400",
		googleauth.ErrRejected)}
	s, got := firebaseWith(t, mint, http.StatusOK, accepted, nil)

	_, err := s.Send(context.Background(), msg("Hello", "world"))
	if shared.SendKindOf(err) != shared.SendPermanent {
		t.Errorf("kind = %v, want permanent", shared.SendKindOf(err))
	}
	if got.seen {
		t.Error("a message went out without a token")
	}
}

// Nothing is spent on a message that was going to be refused anyway.
func TestNoTokenIsMintedForAMessageWeRefuse(t *testing.T) {
	mint := &tokens{token: minted}
	s, _ := firebaseWith(t, mint, http.StatusOK, accepted, nil)

	if _, err := s.Send(context.Background(), msg("Hello", "")); err == nil {
		t.Fatal("Send: want a refusal")
	}
	if mint.asked != 0 {
		t.Errorf("minted %d tokens, want none", mint.asked)
	}
}

func TestWhatThisSenderRefusesOnItsOwn(t *testing.T) {
	cases := []struct {
		name string
		msg  shared.Message
	}{
		{"no device token", msg("Hello", "world")},
		{"a device token pasted wrong", msg("Hello", "world")},
		{"nothing to read", msg("Hello", "   ")},
		{"a title longer than fcm takes", msg(strings.Repeat("a", 2000), "world")},
		{"a body longer than fcm takes", msg("Hello", strings.Repeat("a", 5000))},
	}
	cases[0].msg.Recipient.Address = ""
	cases[1].msg.Recipient.Address = "short"

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, got := firebase(t, http.StatusOK, accepted, nil)

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

func TestASenderNeedsAProjectAClientAndTokens(t *testing.T) {
	if _, err := fcm.New(nil, &tokens{}, projectID); err == nil {
		t.Error("New with no client: want an error")
	}
	if _, err := fcm.New(http.DefaultClient, nil, projectID); err == nil {
		t.Error("New with no tokens: want an error")
	}
	_, err := fcm.New(http.DefaultClient, &tokens{}, "  ")
	if err == nil {
		t.Fatal("New with no project: want an error")
	}
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("error = %v, want invalid input", err)
	}
}

func TestTheChannelIsFCM(t *testing.T) {
	s, _ := firebase(t, http.StatusOK, accepted, nil)
	if s.Channel() != shared.ChannelFCM {
		t.Errorf("channel = %q, want fcm", s.Channel())
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
