package usecase_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
	"github.com/Serajian/srosha/pkg/errs"
)

type fakeSecrets struct {
	added   []*credential.Credential
	secrets []string
	configs [][]byte
	cleared []shared.Channel
	rotated []shared.ID
	err     error

	// rows is where Add writes through, so the rig can read back what it
	// registered. The real keeper writes the row as it seals.
	rows *fakeCredentials
}

func (v *fakeSecrets) Add(
	_ context.Context, c *credential.Credential, config []byte, secret string,
) error {
	if v.err != nil {
		return v.err
	}
	v.added = append(v.added, c)
	v.configs = append(v.configs, config)
	v.secrets = append(v.secrets, secret)

	// The real one writes the row as it seals. Without this the rig could
	// register an identity and then not find it.
	if v.rows != nil {
		return v.rows.save(c)
	}
	return nil
}

// Replace is the rotation: a new secret over the old one, same identity.
func (v *fakeSecrets) Replace(
	_ context.Context, c *credential.Credential, secret string,
) error {
	if v.err != nil {
		return v.err
	}
	v.rotated = append(v.rotated, c.ID)
	v.secrets = append(v.secrets, secret)
	return nil
}

// ClearDefault is a different port on purpose: it seals nothing. Both are on
// this fake because the test wants to see both halves of one transaction.
func (v *fakeSecrets) ClearDefault(
	ctx context.Context, sourceID string, c shared.Channel, now time.Time,
) error {
	if v.err != nil {
		return v.err
	}
	v.cleared = append(v.cleared, c)

	// It writes through for the same reason Add does: in production this port is
	// the repository itself, so a fake that only counted the call would leave
	// the old default in place and let a broken SetDefault pass.
	if v.rows != nil {
		return v.rows.ClearDefault(ctx, sourceID, c, now)
	}
	return nil
}

type credentialRig struct {
	creds *usecase.Credentials
	vault *fakeSecrets
	rows  *fakeCredentials
}

func newCredentialRig(t *testing.T, uow usecase.UnitOfWork) *credentialRig {
	t.Helper()

	rows := newFakeCredentials(nil)
	r := &credentialRig{vault: &fakeSecrets{rows: rows}, rows: rows}
	r.creds = usecase.NewCredentials(
		source.NewService(
			fakeSources{byID: map[string]*source.Source{"acme": acmeSource()}},
			fakeLimiter{allow: true},
		),
		credential.NewService(r.rows, fixedNow(now)),
		r.vault, r.vault, uow, seqIDs(), fixedNow(now),
	)
	return r
}

func registration(name string, isDefault bool) usecase.CredentialRegistration {
	return usecase.CredentialRegistration{
		Channel:   shared.ChannelTelegram,
		Name:      name,
		Config:    []byte(`{"chat_id":"42"}`),
		Secret:    "bot-token",
		IsDefault: isDefault,
	}
}

func TestRegisterOpensAnIdentity(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	c, err := r.creds.Register(context.Background(), "acme", registration("alerts", false))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if c.SourceID != "acme" || c.Name != "alerts" || c.Channel != shared.ChannelTelegram {
		t.Errorf("registered %+v", c)
	}
	if !c.IsActive() {
		t.Error("a new identity should be active")
	}

	if len(r.vault.secrets) != 1 || r.vault.secrets[0] != "bot-token" {
		t.Errorf("the vault was given %v", r.vault.secrets)
	}
	if len(r.vault.cleared) != 0 {
		t.Error("the default was moved for an identity that did not ask to be it")
	}
}

// Taking the default over is two writes, and the second is worthless without
// the first: the index refuses two defaults on one channel.
func TestRegisteringADefaultTakesItOver(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	if _, err := r.creds.Register(context.Background(), "acme", registration("alerts", true)); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(r.vault.cleared) != 1 || r.vault.cleared[0] != shared.ChannelTelegram {
		t.Errorf("cleared %v, want the telegram default", r.vault.cleared)
	}
	if len(r.vault.added) != 1 {
		t.Errorf("stored %d identities, want 1", len(r.vault.added))
	}
}

// And both halves are one write. A transaction that cannot start must leave the
// channel with the default it already had.
func TestNothingIsStoredWhenTheTransactionFails(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{err: errs.UnavailableErr("no transaction")})

	if _, err := r.creds.Register(context.Background(), "acme", registration("alerts", true)); err == nil {
		t.Fatal("Register() succeeded without a transaction")
	}
	if len(r.vault.added) != 0 || len(r.vault.cleared) != 0 {
		t.Error("something was written outside the transaction")
	}
}

func TestAnUnknownSourceRegistersNothing(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	_, err := r.creds.Register(context.Background(), "nobody", registration("alerts", false))
	if !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("Register() = %v, want a not-found", err)
	}
	if len(r.vault.added) != 0 {
		t.Error("an identity was stored for a source that does not exist")
	}
}

func TestTheNameHasToBeUsable(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	for _, name := range []string{"", "Alerts", "with space", "with_underscore"} {
		t.Run(name, func(t *testing.T) {
			_, err := r.creds.Register(context.Background(), "acme", registration(name, false))
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("Register(%q) = %v, want invalid input", name, err)
			}
		})
	}
}

func TestSettingsThatAreNotJSONAreRefused(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	reg := registration("alerts", false)
	reg.Config = []byte("chat_id=42")

	_, err := r.creds.Register(context.Background(), "acme", reg)
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("Register() = %v, want invalid input", err)
	}
	if len(r.vault.added) != 0 {
		t.Error("a row the database would refuse was written anyway")
	}
}

// A command struct reaches a log line eventually.
func TestARegistrationDoesNotPrintItsSecret(t *testing.T) {
	printed := registration("alerts", false).String()
	if strings.Contains(printed, "bot-token") {
		t.Errorf("the registration printed its secret: %s", printed)
	}
	if !strings.Contains(printed, "alerts") {
		t.Errorf("the registration printed nothing useful: %s", printed)
	}
}

// --- the rest of the identity's life -----------------------------------------

func (r *credentialRig) register(t *testing.T, name string, isDefault bool) *credential.Credential {
	t.Helper()

	c, err := r.creds.Register(context.Background(), "acme", registration(name, isDefault))
	if err != nil {
		t.Fatalf("Register(%q) error = %v", name, err)
	}
	return c
}

func TestListShowsWhatWasRegistered(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	r.register(t, "alerts", true)
	r.register(t, "marketing", false)

	got, err := r.creds.List(context.Background(), "acme")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %d identities, want 2", len(got))
	}
	if got[0].Name != "alerts" || got[1].Name != "marketing" {
		t.Errorf("listed %q and %q", got[0].Name, got[1].Name)
	}
}

// The answer to "what do I have" must include the one somebody disabled, or
// nobody can turn it back on.
func TestListKeepsTheOnesThatWereSwitchedOff(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", false)

	if _, err := r.creds.Deactivate(context.Background(), "acme", c.ID); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}

	got, err := r.creds.List(context.Background(), "acme")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].IsActive() {
		t.Errorf("listed %+v, want the switched-off one", got)
	}
}

// Switching off the default leaves the channel with none. Guessing which should
// take over would move it silently.
func TestDeactivatingTheDefaultLeavesNone(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", true)

	got, err := r.creds.Deactivate(context.Background(), "acme", c.ID)
	if err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}
	if got.IsActive() {
		t.Error("still active")
	}
	if got.IsDefault() {
		t.Error("a switched-off identity is still the default")
	}
}

// And switching it back on does not hand the default back.
func TestActivatingDoesNotRestoreTheDefault(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", true)

	if _, err := r.creds.Deactivate(context.Background(), "acme", c.ID); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}
	got, err := r.creds.Activate(context.Background(), "acme", c.ID)
	if err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	if !got.IsActive() {
		t.Error("not active again")
	}
	if got.IsDefault() {
		t.Error("activating handed the default back")
	}
}

func TestSetDefaultMovesIt(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	first := r.register(t, "alerts", true)
	second := r.register(t, "marketing", false)

	if _, err := r.creds.SetDefault(context.Background(), "acme", second.ID); err != nil {
		t.Fatalf("SetDefault() error = %v", err)
	}

	got, err := r.creds.List(context.Background(), "acme")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, c := range got {
		want := c.ID == second.ID
		if c.IsDefault() != want {
			t.Errorf("%q default = %v, want %v", c.Name, c.IsDefault(), want)
		}
	}
	_ = first
}

// A default that cannot be used leaves every message naming no identity failing
// with nothing to fix.
func TestASwitchedOffIdentityCannotBecomeTheDefault(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", false)

	if _, err := r.creds.Deactivate(context.Background(), "acme", c.ID); err != nil {
		t.Fatalf("Deactivate() error = %v", err)
	}
	if _, err := r.creds.SetDefault(context.Background(), "acme", c.ID); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("SetDefault() = %v, want invalid input", err)
	}
}

// A leaked token must not cost the source a code change. Rotating keeps the
// name, so every message still naming it keeps working.
func TestRotateKeepsTheName(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", true)

	got, err := r.creds.Rotate(context.Background(), "acme", c.ID, "a-new-token")
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if got.Name != "alerts" || got.ID != c.ID {
		t.Errorf("rotate changed the identity: %+v", got)
	}
	if len(r.vault.rotated) != 1 || r.vault.rotated[0] != c.ID {
		t.Errorf("the vault was asked to rotate %v", r.vault.rotated)
	}
	if last := r.vault.secrets[len(r.vault.secrets)-1]; last != "a-new-token" {
		t.Errorf("sealed %q, want the new token", last)
	}
}

// The id arrives in a request body. A source naming somebody else's identity
// must find nothing, not find it.
func TestOneSourceCannotTouchAnothersIdentity(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})
	c := r.register(t, "alerts", true)

	// Same id, a source that does not own it -- and does not exist here either,
	// which is the first gate.
	for name, call := range map[string]func() error{
		"deactivate": func() error {
			_, err := r.creds.Deactivate(context.Background(), "somebody-else", c.ID)
			return err
		},
		"set default": func() error {
			_, err := r.creds.SetDefault(context.Background(), "somebody-else", c.ID)
			return err
		},
		"rotate": func() error {
			_, err := r.creds.Rotate(context.Background(), "somebody-else", c.ID, "x")
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s succeeded for a source that does not own it", name)
		}
	}
}

func TestAnIdentityThatIsNotThere(t *testing.T) {
	r := newCredentialRig(t, fakeUOW{})

	if _, err := r.creds.Deactivate(context.Background(), "acme", shared.ID("01J8XQ2M4E7N9V3B5C6D7F8999")); !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("Deactivate() = %v, want not found", err)
	}
	if _, err := r.creds.Rotate(context.Background(), "acme", "", "x"); !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("Rotate() with no id = %v, want invalid input", err)
	}
}
