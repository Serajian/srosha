package source_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func later(d time.Duration) *time.Time {
	t := authNow.Add(d)
	return &t
}

func TestNewKeyDescribesWhatWeKeep(t *testing.T) {
	k, err := source.NewKey(keyID, "acme", "production", later(24*time.Hour), authNow)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	if k.ID != keyID || k.SourceID != "acme" || k.Label != "production" {
		t.Errorf("fields not copied: %+v", k)
	}
	if !k.CreatedAt.Equal(authNow) {
		t.Errorf("CreatedAt = %v, want %v", k.CreatedAt, authNow)
	}
	if k.LastUsedAt != nil || k.RevokedAt != nil {
		t.Error("a key that has never been used or revoked must say so with nil")
	}
}

func TestNewKeyRejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		id       shared.ID
		sourceID string
		label    string
		expires  *time.Time
		sentinel error
		typ      errs.Type
	}{
		{
			name: "missing id", id: "", sourceID: "acme", label: "production",
			sentinel: shared.ErrInvalidID, typ: errs.ErrInternal,
		},
		{
			name: "missing source", id: keyID, sourceID: "", label: "production",
			sentinel: source.ErrMissingSource, typ: errs.ErrInternal,
		},
		{
			name: "no label", id: keyID, sourceID: "acme", label: "",
			sentinel: source.ErrKeyLabelRequired, typ: errs.ErrInvalidInput,
		},
		{
			name: "label too long", id: keyID, sourceID: "acme",
			label:    strings.Repeat("a", 65),
			sentinel: source.ErrKeyLabelTooLong, typ: errs.ErrInvalidInput,
		},
		{
			name: "expires in the past", id: keyID, sourceID: "acme", label: "production",
			expires:  later(-time.Hour),
			sentinel: source.ErrKeyAlreadyExpired, typ: errs.ErrInvalidInput,
		},
		{
			name: "expires exactly now", id: keyID, sourceID: "acme", label: "production",
			expires:  later(0),
			sentinel: source.ErrKeyAlreadyExpired, typ: errs.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := source.NewKey(tt.id, tt.sourceID, tt.label, tt.expires, authNow)
			if err == nil {
				t.Fatalf("NewKey() = %+v, want an error", got)
			}
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("errors.Is(%v) = false, got %v", tt.sentinel, err)
			}
			if !errs.IsType(err, tt.typ) {
				t.Errorf("type = %v, want %v", errs.TypeOf(err), tt.typ)
			}
		})
	}
}

// A key with no expiry is a customer's deliberate choice, not an oversight.
func TestAKeyWithNoExpiryIsAccepted(t *testing.T) {
	k, err := source.NewKey(keyID, "acme", "ci", nil, authNow)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	if k.ExpiresAt != nil {
		t.Error("ExpiresAt should stay nil")
	}
	if !k.IsLive(authNow.Add(100 * 365 * 24 * time.Hour)) {
		t.Error("a key with no expiry must never expire")
	}
}

func TestIsLive(t *testing.T) {
	revoked := authNow.Add(-time.Minute)

	tests := []struct {
		name string
		key  source.Key
		want bool
	}{
		{"fresh", source.Key{ExpiresAt: later(time.Hour)}, true},
		{"expired", source.Key{ExpiresAt: later(-time.Hour)}, false},
		{"expires exactly now", source.Key{ExpiresAt: later(0)}, false},
		{"revoked", source.Key{RevokedAt: &revoked, ExpiresAt: later(time.Hour)}, false},
		{
			"revoked and expired",
			source.Key{RevokedAt: &revoked, ExpiresAt: later(-time.Hour)},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.IsLive(authNow); got != tt.want {
				t.Errorf("IsLive() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The type has no field for the key or its hash, so nothing can put one there
// -- not a mapper, not a SELECT *.
func TestKeyCarriesNeitherTheKeyNorItsHash(t *testing.T) {
	k, err := source.NewKey(keyID, "acme", "production", nil, authNow)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	if got := strings.ToLower(fmt.Sprintf("%+v", *k)); strings.Contains(got, "hash") ||
		strings.Contains(got, "secret") || strings.Contains(got, "token") {
		t.Errorf("the entity gained a secret-shaped field: %s", got)
	}
}
