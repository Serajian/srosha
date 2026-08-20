package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

type registerRig struct {
	registrar *usecase.Registrar
	src       *source.Source
}

func newRegisterRig(t *testing.T) *registerRig {
	t.Helper()

	src := acmeSource()
	r := &registerRig{src: src}
	r.registrar = usecase.NewRegistrar(
		source.NewService(
			fakeSources{byID: map[string]*source.Source{"acme": src}},
			fakeLimiter{allow: true},
		),
		webhook.NewService(newFakeWebhooks(), seqIDs(), fixedNow(now), webhook.Strict),
	)
	return r
}

func reg(u string) webhook.Registration {
	return webhook.Registration{CallbackURL: u, BatchInterval: 5 * time.Second, MaxBatchSize: 200}
}

func TestRegisterStoresTheCallback(t *testing.T) {
	r := newRegisterRig(t)

	w, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if w.CallbackURL != "https://acme.com/hooks" || w.SourceID != "acme" {
		t.Errorf("stored %q for %q", w.CallbackURL, w.SourceID)
	}
	if !w.IsActive() {
		t.Error("a new callback should be active")
	}
}

// One source has one callback, so registering again moves the existing one
// rather than adding a second.
func TestRegisterMovesTheExistingCallback(t *testing.T) {
	r := newRegisterRig(t)

	first, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/old"))
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	second, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/new"))
	if err != nil {
		t.Fatalf("second Register() error = %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("id = %q, want the same webhook %q", second.ID, first.ID)
	}
	if second.CallbackURL != "https://acme.com/new" {
		t.Errorf("url = %q, want the new one", second.CallbackURL)
	}
}

// A new address has not failed at anything yet. If we switched the old one off
// because its endpoint was dead, that says nothing about this one.
func TestRegisterGivesANewAddressACleanStart(t *testing.T) {
	r := newRegisterRig(t)

	if _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/old")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.registrar.Deactivate(context.Background(), "acme"); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}

	w, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/new"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if !w.IsActive() {
		t.Error("the new address was left switched off")
	}
	if w.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want the run cleared", w.ConsecutiveFailures())
	}
}

// An address refused at registration must not slip in through an update.
func TestRegisterChecksTheURLOnEveryCall(t *testing.T) {
	r := newRegisterRig(t)

	if _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/ok")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err := r.registrar.Register(context.Background(), "acme", reg("https://nats:8222/jsz"))
	if !errors.Is(err, webhook.ErrPrivateURL) {
		t.Errorf("error = %v, want ErrPrivateURL", err)
	}
}

func TestRegisterRefusesAnInactiveSource(t *testing.T) {
	r := newRegisterRig(t)
	r.src.IsActive = false

	_, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks"))

	if !errors.Is(err, source.ErrSourceInactive) {
		t.Fatalf("error = %v, want ErrSourceInactive", err)
	}
	if !errs.IsType(err, errs.ErrForbidden) {
		t.Errorf("type = %v, want forbidden", errs.TypeOf(err))
	}
}

func TestGetWithoutARegisteredCallback(t *testing.T) {
	r := newRegisterRig(t)

	_, err := r.registrar.Get(context.Background(), "acme")

	if !errors.Is(err, webhook.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// Deactivating keeps the address, so switching back on is not a re-registration.
func TestDeactivateKeepsTheAddress(t *testing.T) {
	r := newRegisterRig(t)

	if _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.registrar.Deactivate(context.Background(), "acme"); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}

	w, err := r.registrar.Get(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if w.IsActive() {
		t.Error("still active")
	}
	if w.CallbackURL != "https://acme.com/hooks" {
		t.Errorf("url = %q, want it kept", w.CallbackURL)
	}
}
