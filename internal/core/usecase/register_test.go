package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type registerRig struct {
	registrar *usecase.Registrar
	src       *source.Source

	secrets *issuer
}

func newRegisterRig(t *testing.T) *registerRig {
	t.Helper()

	src := acmeSource()
	r := &registerRig{src: src, secrets: &issuer{}}
	r.registrar = usecase.NewRegistrar(
		source.NewService(
			fakeSources{byID: map[string]*source.Source{"acme": src}},
			fakeLimiter{allow: true},
		),
		webhook.NewService(newFakeWebhooks(), seqIDs(), fixedNow(now), webhook.Strict),
		r.secrets,
	)
	return r
}

// issuer stands in for whoever holds the encryption keys. It counts, because
// the rule worth checking is that a secret is issued once and not on every
// registration.
type issuer struct {
	issued int
	err    error
}

func (i *issuer) Issue(_ context.Context, _ string, _ shared.ID) (string, error) {
	i.issued++
	if i.err != nil {
		return "", i.err
	}
	return fmt.Sprintf("whsec_%d", i.issued), nil
}

func reg(u string) webhook.Registration {
	return webhook.Registration{CallbackURL: u}
}

func TestRegisterStoresTheCallback(t *testing.T) {
	r := newRegisterRig(t)

	w, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks"))
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

	first, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/old"))
	if err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	second, _, err := r.registrar.Register(
		context.Background(),
		"acme",
		reg("https://acme.com/new"),
	)
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

	if _, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/old")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := r.registrar.Deactivate(context.Background(), "acme"); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}

	w, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/new"))
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

	if _, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/ok")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, _, err := r.registrar.Register(context.Background(), "acme", reg("https://nats:8222/jsz"))
	if !errors.Is(err, webhook.ErrPrivateURL) {
		t.Errorf("error = %v, want ErrPrivateURL", err)
	}
}

// A source that cannot send can still be configured, and this is the case that
// matters: a source is created waiting for approval, so if registering a
// callback needed it to be active, a customer could never set one up before an
// operator had already approved an empty source.
//
// This test used to assert the opposite. The behavior changed with approval,
// and the reason is worth more than the assertion was.
func TestACallbackCanBeSetOnASourceThatCannotSendYet(t *testing.T) {
	r := newRegisterRig(t)
	r.src.IsActive = false

	_, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks"))
	if err != nil {
		t.Fatalf("Register on a source waiting for approval: %v", err)
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

	if _, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/hooks")); err != nil {
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

// The secret is handed over exactly once, on the call that creates the
// callback. It crosses the wire there and nowhere else.
func TestTheSigningSecretIsIssuedOnce(t *testing.T) {
	r := newRegisterRig(t)

	_, secret, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/a"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if secret == "" {
		t.Fatal("the first registration returned no secret")
	}
	if r.secrets.issued != 1 {
		t.Errorf("issued %d secrets, want 1", r.secrets.issued)
	}
}

// Registering again moves the address. Rotating the secret at the same time
// would break every receiver that was already verifying, and none of them asked
// for that.
func TestChangingTheAddressKeepsTheSecret(t *testing.T) {
	r := newRegisterRig(t)

	if _, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/a")); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	w, secret, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/b"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if w.CallbackURL != "https://acme.com/b" {
		t.Errorf("callback url = %q, want it moved", w.CallbackURL)
	}
	if secret != "" {
		t.Errorf("a second registration returned a secret (%q)", secret)
	}
	if r.secrets.issued != 1 {
		t.Errorf("issued %d secrets, want the first one to still stand", r.secrets.issued)
	}
}

// Without this, a source that lost its secret could never verify another
// callback: what is stored is sealed and nothing reads it back.
func TestRotateIssuesANewSecret(t *testing.T) {
	r := newRegisterRig(t)

	_, first, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/a"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	second, err := r.registrar.RotateSecret(context.Background(), "acme")
	if err != nil {
		t.Fatalf("RotateSecret() error = %v", err)
	}
	if second == "" || second == first {
		t.Errorf("rotate gave %q, want a different secret", second)
	}
	if r.secrets.issued != 2 {
		t.Errorf("issued %d secrets, want 2", r.secrets.issued)
	}
}

// Rotating for a source with no callback is a not-found, not a secret issued
// against nothing.
func TestRotatingWithNoCallback(t *testing.T) {
	r := newRegisterRig(t)

	if _, err := r.registrar.RotateSecret(context.Background(), "acme"); err == nil {
		t.Fatal("RotateSecret() with no callback succeeded")
	}
	if r.secrets.issued != 0 {
		t.Errorf("issued %d secrets for a callback that does not exist", r.secrets.issued)
	}
}

// A callback with no secret cannot be signed, and the notifier refuses to send
// one unsigned -- so a webhook that was created but never got one is a row that
// would never fire. The registration fails instead.
func TestARegistrationThatCannotBeGivenASecret(t *testing.T) {
	r := newRegisterRig(t)
	r.secrets.err = errors.New("the keyring is unreachable")

	if _, _, err := r.registrar.Register(context.Background(), "acme", reg("https://acme.com/a")); err == nil {
		t.Fatal("Register() succeeded with no secret issued")
	}
}
