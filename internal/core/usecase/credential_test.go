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
	err     error
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
	return nil
}

// ClearDefault is a different port on purpose: it seals nothing. Both are on
// this fake because the test wants to see both halves of one transaction.
func (v *fakeSecrets) ClearDefault(
	_ context.Context, _ string, c shared.Channel, _ time.Time,
) error {
	if v.err != nil {
		return v.err
	}
	v.cleared = append(v.cleared, c)
	return nil
}

type credentialRig struct {
	creds *usecase.Credentials
	vault *fakeSecrets
}

func newCredentialRig(t *testing.T, uow usecase.UnitOfWork) *credentialRig {
	t.Helper()

	r := &credentialRig{vault: &fakeSecrets{}}
	r.creds = usecase.NewCredentials(
		source.NewService(
			fakeSources{byID: map[string]*source.Source{"acme": acmeSource()}},
			fakeLimiter{allow: true},
		),
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
