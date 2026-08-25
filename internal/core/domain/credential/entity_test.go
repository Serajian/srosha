package credential_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

var (
	credID = shared.ID("01J8XQ2M4E7N9V3B5C6D7F8C01")
	now    = time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)
	later  = now.Add(time.Hour)
)

func newCred(t *testing.T, name string, isDefault bool) *credential.Credential {
	t.Helper()
	c, err := credential.New(credID, "acme", shared.ChannelEmail, name, isDefault, now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func snap(name string, isDefault, isActive bool) credential.Snapshot {
	return credential.Snapshot{
		ID: credID, SourceID: "acme", Channel: shared.ChannelEmail,
		Name: name, IsDefault: isDefault, IsActive: isActive,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestNewOpensAnActiveCredential(t *testing.T) {
	c := newCred(t, "transactional", true)

	if c.Name != "transactional" || c.SourceID != "acme" {
		t.Errorf("fields not copied: %q / %q", c.Name, c.SourceID)
	}
	if !c.IsActive() {
		t.Error("a new credential should be active")
	}
	if !c.IsDefault() {
		t.Error("IsDefault() = false, want true")
	}
	if !c.CreatedAt.Equal(now) || !c.UpdatedAt.Equal(now) {
		t.Error("timestamps not set")
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		id       shared.ID
		sourceID string
		channel  shared.Channel
		credName string
		now      time.Time
		sentinel error
		typ      errs.Type
	}{
		{
			name: "missing id", id: "", sourceID: "acme", channel: shared.ChannelEmail,
			credName: "default", now: now,
			sentinel: shared.ErrInvalidID, typ: errs.ErrInternal,
		},
		{
			name: "missing source", id: credID, sourceID: "", channel: shared.ChannelEmail,
			credName: "default", now: now,
			sentinel: credential.ErrMissingSource, typ: errs.ErrInternal,
		},
		{
			name: "unknown channel", id: credID, sourceID: "acme", channel: "carrier-pigeon",
			credName: "default", now: now,
			sentinel: shared.ErrUnknownChannel, typ: errs.ErrInvalidInput,
		},
		{
			name: "empty name", id: credID, sourceID: "acme", channel: shared.ChannelEmail,
			credName: "", now: now,
			sentinel: credential.ErrEmptyName, typ: errs.ErrInvalidInput,
		},
		{
			name: "name with spaces", id: credID, sourceID: "acme", channel: shared.ChannelEmail,
			credName: "my sender", now: now,
			sentinel: credential.ErrInvalidName, typ: errs.ErrInvalidInput,
		},
		{
			name: "name in capitals", id: credID, sourceID: "acme", channel: shared.ChannelEmail,
			credName: "Marketing", now: now,
			sentinel: credential.ErrInvalidName, typ: errs.ErrInvalidInput,
		},
		{
			name: "name too long", id: credID, sourceID: "acme", channel: shared.ChannelEmail,
			credName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now: now,
			sentinel: credential.ErrInvalidName, typ: errs.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := credential.New(tt.id, tt.sourceID, tt.channel, tt.credName, false, tt.now)
			if err == nil {
				t.Fatalf("New() = %+v, want an error", got)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false", tt.sentinel)
			}
			if !errs.IsType(err, tt.typ) {
				t.Errorf("type = %v, want %v", errs.TypeOf(err), tt.typ)
			}
		})
	}
}

// A default that cannot be used leaves every send on that channel failing with
// nothing obvious to fix, so the two flags may never contradict.
func TestDefaultAndActiveCannotContradict(t *testing.T) {
	t.Run("deactivating clears the default", func(t *testing.T) {
		c := newCred(t, "transactional", true)

		c.Deactivate(later)

		if c.IsActive() {
			t.Error("still active")
		}
		if c.IsDefault() {
			t.Error("an inactive credential must not stay the default")
		}
		if !c.UpdatedAt.Equal(later) {
			t.Error("UpdatedAt not moved")
		}
	})

	t.Run("an inactive credential refuses to become the default", func(t *testing.T) {
		c := credential.Restore(snap("marketing", false, false))

		err := c.MakeDefault(later)

		if !errors.Is(err, credential.ErrDefaultUnusable) {
			t.Fatalf("error = %v, want ErrDefaultUnusable", err)
		}
		if c.IsDefault() {
			t.Error("the flag was set anyway")
		}
	})

	t.Run("an active one accepts it", func(t *testing.T) {
		c := newCred(t, "marketing", false)

		if err := c.MakeDefault(later); err != nil {
			t.Fatalf("MakeDefault() error = %v", err)
		}
		if !c.IsDefault() {
			t.Error("IsDefault() = false")
		}
	})
}

func TestActivate(t *testing.T) {
	c := credential.Restore(snap("marketing", false, false))

	c.Activate(later)

	if !c.IsActive() {
		t.Error("IsActive() = false")
	}
	if c.IsDefault() {
		t.Error("activating must not make it the default on its own")
	}
}

func TestPickByName(t *testing.T) {
	creds := []credential.Credential{
		*credential.Restore(snap("transactional", true, true)),
		*credential.Restore(snap("marketing", false, true)),
	}

	got, err := credential.Pick(creds, "marketing")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if got.Name != "marketing" {
		t.Errorf("Name = %q, want marketing", got.Name)
	}
}

func TestPickFallsBackToTheDefault(t *testing.T) {
	creds := []credential.Credential{
		*credential.Restore(snap("marketing", false, true)),
		*credential.Restore(snap("transactional", true, true)),
	}

	got, err := credential.Pick(creds, "")
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if got.Name != "transactional" {
		t.Errorf("Name = %q, want the default", got.Name)
	}
}

func TestPickRefuses(t *testing.T) {
	tests := []struct {
		name     string
		creds    []credential.Credential
		want     string
		sentinel error
	}{
		{
			// Not ErrNoDefault: nothing was registered, so nothing was chosen.
			// A service-wide identity may stand in for this and not for the
			// case below, where the source registered one and switched it off.
			name:     "no credential at all",
			creds:    nil,
			want:     "",
			sentinel: credential.ErrNoCredentials,
		},
		{
			name:     "none marked default",
			creds:    []credential.Credential{*credential.Restore(snap("marketing", false, true))},
			want:     "",
			sentinel: credential.ErrNoDefault,
		},
		{
			name: "the default was deactivated",
			creds: []credential.Credential{
				*credential.Restore(snap("transactional", true, false)),
			},
			want:     "",
			sentinel: credential.ErrNoDefault,
		},
		{
			name: "unknown name",
			creds: []credential.Credential{
				*credential.Restore(snap("transactional", true, true)),
			},
			want:     "marketing",
			sentinel: credential.ErrNotFound,
		},
		{
			name:     "named but inactive",
			creds:    []credential.Credential{*credential.Restore(snap("marketing", false, false))},
			want:     "marketing",
			sentinel: credential.ErrInactive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := credential.Pick(tt.creds, tt.want)
			if err == nil {
				t.Fatalf("Pick() = %+v, want an error", got)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, got %v", tt.sentinel, err)
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("type = %v, want invalid input", errs.TypeOf(err))
			}
		})
	}
}

// The type has no field for a token or a provider setting, so nothing can put
// one there -- not a mapper, not a SELECT *.
func TestCredentialCarriesNoSecret(t *testing.T) {
	c := newCred(t, "transactional", true)

	// If this ever needs updating, ask why the domain now knows what SMTP is.
	if got := fmt.Sprintf("%+v", *c); strings.Contains(got, "ecret") ||
		strings.Contains(got, "onfig") || strings.Contains(got, "oken") {
		t.Errorf("the entity gained a secret-shaped field: %s", got)
	}
}
