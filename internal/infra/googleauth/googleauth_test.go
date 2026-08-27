package googleauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/googleauth"
)

// serviceAccount writes a key file the way Google does, with a real RSA key:
// the library signs an assertion with it, so a placeholder would not get past
// parsing.
func serviceAccount(t *testing.T, project, email, tokenURL string) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{
		Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key),
	})

	raw, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   project,
		"client_email": email,
		"private_key":  string(pemKey),
		"token_uri":    tokenURL,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	return der
}

// google stands in for the token endpoint, and counts how often it is asked.
func google(t *testing.T, ttl int) (*httptest.Server, *int) {
	t.Helper()

	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"ya29.token-%d","token_type":"Bearer","expires_in":%d}`,
			asked, ttl)
	}))
	t.Cleanup(server.Close)
	return server, &asked
}

func TestATokenIsMintedOnceAndThenReused(t *testing.T) {
	server, asked := google(t, 3600)

	m, err := googleauth.NewMinter(server.Client(), googleauth.ScopeFCM)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	source, err := m.Open(serviceAccount(t, "p", "bot@p.iam.gserviceaccount.com", server.URL))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for i := range 5 {
		token, err := source.Token(context.Background())
		if err != nil {
			t.Fatalf("Token %d: %v", i, err)
		}
		if token != "ya29.token-1" {
			t.Errorf("token %d = %q, want the first one back", i, token)
		}
	}
	if *asked != 1 {
		t.Errorf("asked google %d times, want 1", *asked)
	}
}

// The whole reason this package exists: the dispatcher builds a sender per
// message, so the same account arriving again must not mean a second key and a
// second exchange.
func TestTheSameAccountOpensToTheSameSource(t *testing.T) {
	server, asked := google(t, 3600)

	m, err := googleauth.NewMinter(server.Client(), googleauth.ScopeFCM)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}

	key := serviceAccount(t, "p", "bot@p.iam.gserviceaccount.com", server.URL)
	for i := range 3 {
		source, err := m.Open(key)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		if _, err := source.Token(context.Background()); err != nil {
			t.Fatalf("Token %d: %v", i, err)
		}
	}
	if *asked != 1 {
		t.Errorf("asked google %d times, want 1", *asked)
	}
}

// An expired token is minted again on its own. Google's tokens last an hour and
// nothing tells us when one has gone stale except its expiry.
func TestAnExpiredTokenIsReplaced(t *testing.T) {
	server, asked := google(t, 1)

	m, _ := googleauth.NewMinter(server.Client(), googleauth.ScopeFCM)
	source, err := m.Open(serviceAccount(t, "p", "bot@p.iam.gserviceaccount.com", server.URL))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	first, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	second, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token again: %v", err)
	}
	if first == second {
		t.Error("the expired token was handed back")
	}
	if *asked != 2 {
		t.Errorf("asked google %d times, want 2", *asked)
	}
}

func TestTwoAccountsDoNotShareAToken(t *testing.T) {
	server, asked := google(t, 3600)

	m, _ := googleauth.NewMinter(server.Client(), googleauth.ScopeFCM)

	for _, project := range []string{"one", "two"} {
		source, err := m.Open(serviceAccount(t, project, "bot@"+project+".test", server.URL))
		if err != nil {
			t.Fatalf("Open %s: %v", project, err)
		}
		if got := source.Account().ProjectID; got != project {
			t.Errorf("project = %q, want %q", got, project)
		}
		if _, err := source.Token(context.Background()); err != nil {
			t.Fatalf("Token %s: %v", project, err)
		}
	}
	if *asked != 2 {
		t.Errorf("asked google %d times, want 2", *asked)
	}
}

func TestAccountIsReadWithoutTheKey(t *testing.T) {
	raw := serviceAccount(t, "srosha-1", "pusher@srosha-1.iam.gserviceaccount.com", "https://x")

	account, err := googleauth.ParseAccount(raw)
	if err != nil {
		t.Fatalf("ParseAccount: %v", err)
	}
	if account.ProjectID != "srosha-1" {
		t.Errorf("project = %q, want srosha-1", account.ProjectID)
	}
	if account.Email != "pusher@srosha-1.iam.gserviceaccount.com" {
		t.Errorf("email = %q, want the client_email", account.Email)
	}
}

func TestWhatIsNotAServiceAccount(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"not json at all", "-----BEGIN PRIVATE KEY-----"},
		{"an oauth client instead", `{"type":"authorized_user","project_id":"p"}`},
		{"no project", `{"type":"service_account","client_email":"a@b"}`},
		{"no email", `{"type":"service_account","project_id":"p"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := googleauth.ParseAccount([]byte(c.raw)); err == nil {
				t.Fatal("ParseAccount: want an error")
			}
		})
	}
}

// A file is mostly a private key, so nothing from it is quoted back.
func TestTheKeyNeverReachesAnError(t *testing.T) {
	key := serviceAccount(t, "p", "bot@p.test", "https://x")

	var file map[string]string
	if err := json.Unmarshal(key, &file); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	secret := file["private_key"]

	m, _ := googleauth.NewMinter(http.DefaultClient, googleauth.ScopeFCM)

	// A file that parses as json but has no usable key.
	broken := []byte(`{"type":"service_account","project_id":"p","client_email":"bot@p.test",` +
		`"private_key":"` + strings.ReplaceAll(secret, "\n", "")[:40] + `"}`)

	_, err := m.Open(broken)
	if err == nil {
		t.Fatal("Open: want an error")
	}
	if strings.Contains(err.Error(), "PRIVATE KEY") || strings.Contains(err.Error(), "MII") {
		t.Errorf("error = %q, key material leaked into it", err)
	}
}

func TestAMinterNeedsAClientAndAScope(t *testing.T) {
	if _, err := googleauth.NewMinter(nil, googleauth.ScopeFCM); err == nil {
		t.Error("NewMinter with no client: want an error")
	}
	if _, err := googleauth.NewMinter(http.DefaultClient); err == nil {
		t.Error("NewMinter with no scopes: want an error")
	}
}

// The context bounds the wait even though oauth2 cannot be told to stop.
func TestACallerThatGivesUpIsNotLeftWaiting(t *testing.T) {
	// Released at cleanup rather than left hanging: httptest.Server.Close waits
	// for its handlers, and the refresh this blocks has no context to cancel.
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(func() {
		close(release)
		slow.Close()
	})

	m, _ := googleauth.NewMinter(slow.Client(), googleauth.ScopeFCM)
	source, err := m.Open(serviceAccount(t, "p", "bot@p.test", slow.URL))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := source.Token(ctx); err == nil {
		t.Fatal("Token: want an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %v after the context expired", elapsed)
	}
}

// A well-formed key file is not the same as a working one, and only the
// exchange knows the difference. Google says so with an oauth error code, and
// the ones that mean "this account" must not look like "try again later".
func TestAnAccountGoogleRefusesIsFinal(t *testing.T) {
	final := []string{"invalid_grant", "invalid_client", "unauthorized_client", "invalid_scope"}
	for _, code := range final {
		t.Run(code, func(t *testing.T) {
			if !errors.Is(mintAgainst(t, http.StatusBadRequest, code), googleauth.ErrRejected) {
				t.Errorf("%s was not treated as final", code)
			}
		})
	}

	t.Run("a name we do not know", func(t *testing.T) {
		if errors.Is(
			mintAgainst(t, http.StatusBadRequest, "something_new"),
			googleauth.ErrRejected,
		) {
			t.Error("an unrecognized code was treated as final")
		}
	})

	// Whatever Google calls it, a 5xx is Google's and worth asking again.
	t.Run("invalid_grant with a 500", func(t *testing.T) {
		if errors.Is(mintAgainst(t, http.StatusInternalServerError, "invalid_grant"),
			googleauth.ErrRejected) {
			t.Error("a 500 was treated as final")
		}
	})
}

func mintAgainst(t *testing.T, status int, code string) error {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":%q,"error_description":"no"}`, code)
	}))
	t.Cleanup(server.Close)

	m, _ := googleauth.NewMinter(server.Client(), googleauth.ScopeFCM)
	source, err := m.Open(serviceAccount(t, "p", "bot@p.test", server.URL))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := source.Token(context.Background()); err != nil {
		return err
	}
	t.Fatal("Token: want an error")
	return nil
}
