package webhook_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

var (
	hookID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8W01")
	now    = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	later  = now.Add(time.Hour)
)

func reg(u string) webhook.Registration {
	return webhook.Registration{CallbackURL: u}
}

func newHook(t *testing.T) *webhook.Webhook {
	t.Helper()
	w, err := webhook.New(hookID, "acme", reg("https://acme.com/hooks/srosha"), webhook.Strict, now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return w
}

func TestNewRegistersAnActiveWebhook(t *testing.T) {
	w := newHook(t)

	if w.SourceID != "acme" || w.CallbackURL != "https://acme.com/hooks/srosha" {
		t.Errorf("fields not copied: %q / %q", w.SourceID, w.CallbackURL)
	}
	if !w.IsActive() {
		t.Error("a new webhook should be active")
	}
	if w.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want 0", w.ConsecutiveFailures())
	}
}

// A callback makes US call an address THEY chose. Without this check a source
// reaches anything on our private network -- the unauthenticated NATS
// monitoring port, the database, a cloud metadata endpoint -- and authentication
// does not help, because registering a webhook is something they are allowed to
// do. The destination is the attack, not the access.
func TestNewRefusesURLsPointingInsideOurNetwork(t *testing.T) {
	hosts := []string{
		"https://localhost/hook",
		"https://localhost.:8080/hook",
		"https://api.localhost/hook",
		"https://printer.local/hook",
		"https://127.0.0.1/hook",
		"https://127.0.0.53:8222/hook",
		"https://[::1]/hook",
		"https://10.0.0.5/hook",
		"https://172.16.3.4/hook",
		"https://192.168.1.1/hook",
		"https://169.254.169.254/latest/meta-data/",
		"https://0.0.0.0/hook",
		"https://nats:8222/jsz",
		"https://postgres:5432/",
	}

	for _, u := range hosts {
		t.Run(u, func(t *testing.T) {
			_, err := webhook.New(hookID, "acme", reg(u), webhook.Strict, now)
			if err == nil {
				t.Fatal("accepted a private destination")
			}
			if !errors.Is(err, webhook.ErrPrivateURL) {
				t.Errorf("errors.Is(ErrPrivateURL) = false, got %v", err)
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("type = %v, want invalid input", errs.TypeOf(err))
			}
		})
	}
}

// Public addresses that merely look private must still pass.
func TestNewAcceptsPublicURLs(t *testing.T) {
	urls := []string{
		"https://acme.com/hook",
		"https://hooks.acme.co.uk/srosha?x=1",
		"https://8.8.8.8/hook",
		"https://172.32.0.1/hook", // just outside the private range
		"https://localhost.acme.com/hook",
	}

	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			if _, err := webhook.New(hookID, "acme", reg(u), webhook.Strict, now); err != nil {
				t.Errorf("rejected a public url: %v", err)
			}
		})
	}
}

func TestNewRefusesBadURLs(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		sentinel error
	}{
		{"empty", "", webhook.ErrEmptyURL},
		{"blank", "   ", webhook.ErrEmptyURL},
		{"no host", "https:///hook", webhook.ErrMalformedURL},
		{"not a url", "://nope", webhook.ErrMalformedURL},
		{"plain http", "http://acme.com/hook", webhook.ErrInsecureURL},
		{"another scheme", "ftp://acme.com/hook", webhook.ErrInsecureURL},
		{"credentials in url", "https://user:pass@acme.com/hook", webhook.ErrCredentialsInURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := webhook.New(hookID, "acme", reg(tt.url), webhook.Strict, now)
			if err == nil {
				t.Fatal("accepted a bad url")
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, got %v", tt.sentinel, err)
			}
		})
	}
}

// The rule stays here; how strict it is comes from config, so a developer can
// test against localhost without the production check being weakened.
func TestPolicyRelaxesTheRuleForDevelopment(t *testing.T) {
	dev := webhook.URLPolicy{AllowInsecure: true, AllowPrivate: true}

	if _, err := webhook.New(hookID, "acme", reg("http://localhost:9000/hook"), dev, now); err != nil {
		t.Errorf("the dev policy still rejected localhost: %v", err)
	}
	if _, err := webhook.New(hookID, "acme", reg("http://localhost:9000/hook"), webhook.Strict, now); err == nil {
		t.Error("the strict policy accepted localhost")
	}
}

// A dead endpoint must stop being called; an endpoint that fails now and then
// must not be switched off, which is why success clears the run.
func TestFailureRunSwitchesTheWebhookOff(t *testing.T) {
	w := newHook(t)

	for count := 1; count <= 4; count++ {
		w.RecordFailure(count, 5, later)
	}
	if !w.IsActive() {
		t.Fatal("switched off too early")
	}

	w.RecordSuccess(later)
	if w.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want the run cleared", w.ConsecutiveFailures())
	}

	for count := 1; count <= 5; count++ {
		w.RecordFailure(count, 5, later)
	}
	if w.IsActive() {
		t.Error("still active after the limit was reached")
	}
}

func TestActivateClearsTheFailureRun(t *testing.T) {
	w := webhook.Restore(webhook.Snapshot{
		ID: hookID, SourceID: "acme", CallbackURL: "https://acme.com/hook",
		IsActive: false, ConsecutiveFailures: 9,
	})

	w.Activate(later)

	if !w.IsActive() {
		t.Error("IsActive() = false")
	}
	if w.ConsecutiveFailures() != 0 {
		t.Error("switching it back on must clear the run, or one hiccup kills it again")
	}
}

func TestDeactivate(t *testing.T) {
	w := newHook(t)

	w.Deactivate(later)

	if w.IsActive() {
		t.Error("IsActive() = true")
	}
	if !w.UpdatedAt.Equal(later) {
		t.Error("UpdatedAt not moved")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	s := webhook.Snapshot{
		ID: hookID, SourceID: "acme",
		CallbackURL: "https://acme.com/hook",
		IsActive:    false, ConsecutiveFailures: 3,
		CreatedAt: now, UpdatedAt: later,
	}

	w := webhook.Restore(s)

	if w.ID != s.ID || w.SourceID != s.SourceID || w.CallbackURL != s.CallbackURL {
		t.Error("identity not carried through")
	}
	if w.IsActive() || w.ConsecutiveFailures() != 3 {
		t.Error("state not carried through")
	}
}

// Restore must load a row the current rules would reject.
func TestRestoreLoadsARowNewWouldReject(t *testing.T) {
	w := webhook.Restore(webhook.Snapshot{
		ID: hookID, SourceID: "acme", CallbackURL: "http://localhost/hook", IsActive: true,
	})

	if w.CallbackURL != "http://localhost/hook" {
		t.Errorf("CallbackURL = %q, want it loaded unchanged", w.CallbackURL)
	}
}
