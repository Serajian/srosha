package secret_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/secret"
	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/crypto"
)

// --- a store that remembers what it was told ---------------------------------

type store struct {
	config  []byte
	secret  string
	reseals int
	cleared int
	fail    error
}

func (s *store) Create(
	_ context.Context,
	_ *credential.Credential,
	config []byte,
	secret string,
) error {
	s.config, s.secret = config, secret
	return s.fail
}

func (s *store) ReadMaterial(context.Context, shared.ID) ([]byte, string, error) {
	return s.config, s.secret, nil
}

func (s *store) Reseal(
	_ context.Context,
	_ shared.ID,
	previous, secret string,
	_ time.Time,
) (bool, error) {
	s.reseals++
	if s.fail != nil {
		return false, s.fail
	}
	if s.secret != previous {
		return false, nil // somebody else got there first
	}
	s.secret = secret
	return true, nil
}

func (s *store) Rotate(
	_ context.Context, _ string, _ shared.ID, secret string, _ time.Time,
) error {
	if s.fail != nil {
		return s.fail
	}
	s.secret = secret
	return nil
}

func (s *store) ClearDefault(context.Context, string, shared.Channel, time.Time) error {
	s.cleared++
	return s.fail
}

// --- fixtures ----------------------------------------------------------------

const (
	sourceID = "01J0SOURCE0000000000000000"
	credID   = shared.ID("01J0CRED000000000000000000")
)

func keyring(t *testing.T, active string, ids ...string) *crypto.Keyring {
	t.Helper()

	keys := map[string][]byte{}
	for i, id := range ids {
		keys[id] = bytes.Repeat([]byte{byte('a' + i)}, crypto.KeySize)
	}
	k, err := crypto.NewKeyring(keys, active)
	if err != nil {
		t.Fatalf("NewKeyring() = %v", err)
	}
	return k
}

func newKeeper(t *testing.T, s *store, k *crypto.Keyring) *secret.Keeper {
	t.Helper()

	v, err := secret.New(s, k, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return v
}

func cred(t *testing.T) *credential.Credential {
	t.Helper()

	c, err := credential.New(credID, sourceID, shared.ChannelTelegram, "alerts", true, time.Now())
	if err != nil {
		t.Fatalf("credential.New() = %v", err)
	}
	return c
}

// --- tests -------------------------------------------------------------------

func TestTheSecretIsNeverStoredInTheClear(t *testing.T) {
	s := &store{}
	v := newKeeper(t, s, keyring(t, "1", "1"))

	if err := v.Add(t.Context(), cred(t), []byte(`{"chat":"1"}`), "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	if strings.Contains(s.secret, "bot-token") {
		t.Fatalf("the token was stored as written: %q", s.secret)
	}
	if !strings.HasPrefix(s.secret, "v1.1.") {
		t.Errorf("stored value = %q, want it to name its version and key", s.secret)
	}
}

func TestWhatWasSealedComesBack(t *testing.T) {
	s := &store{}
	v := newKeeper(t, s, keyring(t, "1", "1"))

	if err := v.Add(t.Context(), cred(t), nil, "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	_, secret, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID)
	if err != nil {
		t.Fatalf("Material() = %v", err)
	}
	if secret != "bot-token" {
		t.Errorf("Material() = %q, want %q", secret, "bot-token")
	}
}

// An identity with no secret is a real case, and must not be stored as the
// encryption of nothing.
func TestAnIdentityWithNoSecretStoresNone(t *testing.T) {
	s := &store{}
	v := newKeeper(t, s, keyring(t, "1", "1"))

	if err := v.Add(t.Context(), cred(t), nil, ""); err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if s.secret != "" {
		t.Fatalf("stored %q for an identity with no secret", s.secret)
	}

	_, secret, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID)
	if err != nil || secret != "" {
		t.Errorf("Material() = %q, %v", secret, err)
	}
}

// The attack the binding exists for: a token copied into another source's row.
func TestATokenReadUnderAnotherIdentityDoesNotOpen(t *testing.T) {
	s := &store{}
	v := newKeeper(t, s, keyring(t, "1", "1"))

	if err := v.Add(t.Context(), cred(t), nil, "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	_, _, err := v.Material(
		t.Context(),
		"01J0OTHERSOURCE00000000000",
		shared.ChannelTelegram,
		credID,
	)
	if !errors.Is(err, crypto.ErrCannotOpen) {
		t.Errorf("Material() = %v, want ErrCannotOpen", err)
	}
}

// The guard that keeps rotation free: a value already under the active key is
// read without being written.
func TestReadingUnderTheCurrentKeyWritesNothing(t *testing.T) {
	s := &store{}
	v := newKeeper(t, s, keyring(t, "1", "1"))

	if err := v.Add(t.Context(), cred(t), nil, "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	for range 3 {
		if _, _, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID); err != nil {
			t.Fatalf("Material() = %v", err)
		}
	}
	if s.reseals != 0 {
		t.Errorf("%d writes for reads that needed none", s.reseals)
	}
}

func TestReadingUnderAnOldKeyRewritesTheRow(t *testing.T) {
	s := &store{}

	// Sealed when key 1 was current.
	if err := newKeeper(t, s, keyring(t, "1", "1")).
		Add(t.Context(), cred(t), nil, "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}
	sealed := s.secret

	// The second key is added and made current. Nothing else changes.
	v := newKeeper(t, s, keyring(t, "2", "1", "2"))

	_, secret, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID)
	if err != nil {
		t.Fatalf("Material() = %v", err)
	}
	if secret != "bot-token" {
		t.Errorf("Material() = %q, want the token", secret)
	}
	if s.secret == sealed {
		t.Fatal("the row still holds the value sealed with the old key")
	}
	if !strings.HasPrefix(s.secret, "v1.2.") {
		t.Errorf("resealed value = %q, want it to name the current key", s.secret)
	}

	// And now it is current, so the next read writes nothing.
	before := s.reseals
	if _, _, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID); err != nil {
		t.Fatalf("Material() = %v", err)
	}
	if s.reseals != before {
		t.Error("a value under the current key was written again")
	}
}

// The send is what matters. Tidying up must not be able to stop it.
func TestAFailedResealStillHandsBackTheSecret(t *testing.T) {
	s := &store{}
	if err := newKeeper(t, s, keyring(t, "1", "1")).
		Add(t.Context(), cred(t), nil, "bot-token"); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	s.fail = errors.New("database is having a day")
	v := newKeeper(t, s, keyring(t, "2", "1", "2"))

	_, secret, err := v.Material(t.Context(), sourceID, shared.ChannelTelegram, credID)
	if err != nil {
		t.Fatalf("Material() = %v, want the read to survive a failed reseal", err)
	}
	if secret != "bot-token" {
		t.Errorf("Material() = %q, want the token", secret)
	}
}

func TestAKeeperRefusesToBeBuiltHalfWired(t *testing.T) {
	k := keyring(t, "1", "1")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	cases := map[string]func() (*secret.Keeper, error){
		"no store":   func() (*secret.Keeper, error) { return secret.New(nil, k, time.Now, log) },
		"no keyring": func() (*secret.Keeper, error) { return secret.New(&store{}, nil, time.Now, log) },
		"no clock":   func() (*secret.Keeper, error) { return secret.New(&store{}, k, nil, log) },
		"no logger":  func() (*secret.Keeper, error) { return secret.New(&store{}, k, time.Now, nil) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := build(); err == nil {
				t.Error("New() succeeded")
			}
		})
	}
}
