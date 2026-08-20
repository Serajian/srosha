package source_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func newSource() *source.Source {
	return &source.Source{
		ID:                 "billing-service",
		Name:               "Billing",
		MaxPriority:        shared.PriorityCritical,
		IsActive:           true,
		AllowCustomAddress: false,
		DefaultAddresses: map[shared.Channel]string{
			shared.ChannelEmail:    "ops@example.com",
			shared.ChannelTelegram: "-1001234567890",
		},
	}
}

// resolveOne asserts Resolve succeeded with exactly one recipient, and returns it.
func resolveOne(t *testing.T, s *source.Source, c shared.Channel, addr string) shared.Recipient {
	t.Helper()
	got, err := s.Resolve(c, addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	return got[0]
}

func TestEnsureActive(t *testing.T) {
	s := newSource()
	if err := s.EnsureActive(); err != nil {
		t.Errorf("an active source was refused: %v", err)
	}

	s.IsActive = false
	err := s.EnsureActive()
	if !errors.Is(err, source.ErrSourceInactive) {
		t.Fatalf("error = %v, want ErrSourceInactive", err)
	}
	if !errs.IsType(err, errs.ErrForbidden) {
		t.Errorf("type = %v, want ErrForbidden", errs.TypeOf(err))
	}
}

// Switching a source off must take effect on the next request, not only on the
// paths someone remembered to guard.
func TestEnsureActiveMessageHidesTheSourceID(t *testing.T) {
	s := newSource()
	s.IsActive = false

	ae, ok := errs.As(s.EnsureActive())
	if !ok {
		t.Fatal("not an AppError")
	}
	if strings.Contains(ae.Message(), "billing-service") {
		t.Errorf("message leaks the source id: %q", ae.Message())
	}
}

func TestResolveFallsBackToTheDefault(t *testing.T) {
	s := newSource()

	got := resolveOne(t, s, shared.ChannelEmail, "")

	if got.Channel != shared.ChannelEmail {
		t.Errorf("channel = %q, want email", got.Channel)
	}
	if got.Address != "ops@example.com" {
		t.Errorf("address = %q, want the configured default", got.Address)
	}
}

func TestResolveUsesTheExplicitAddressWhenAllowed(t *testing.T) {
	s := newSource()
	s.AllowCustomAddress = true

	got := resolveOne(t, s, shared.ChannelEmail, "user@example.com")

	if got.Address != "user@example.com" {
		t.Errorf("address = %q, want the explicit one", got.Address)
	}
}

// The whole point of the flag: a leaked key for a system source must not let
// anyone message strangers.
func TestResolveRefusesCustomAddressWhenNotAllowed(t *testing.T) {
	s := newSource()

	_, err := s.Resolve(shared.ChannelEmail, "attacker@example.com")

	if !errors.Is(err, source.ErrCustomAddressNotAllowed) {
		t.Fatalf("error = %v, want ErrCustomAddressNotAllowed", err)
	}
	if !errs.IsType(err, errs.ErrForbidden) {
		t.Errorf("type = %v, want ErrForbidden", errs.TypeOf(err))
	}
}

// A permitted source still cannot send garbage: the shape is checked.
func TestResolveValidatesTheExplicitAddress(t *testing.T) {
	s := newSource()
	s.AllowCustomAddress = true

	_, err := s.Resolve(shared.ChannelTelegram, "user@example.com")

	if !errors.Is(err, shared.ErrInvalidAddress) {
		t.Errorf("error = %v, want ErrInvalidAddress", err)
	}
}

func TestResolveRefusesUnknownChannel(t *testing.T) {
	s := newSource()
	s.AllowCustomAddress = true

	_, err := s.Resolve(shared.Channel("carrier-pigeon"), "someone")

	if !errors.Is(err, shared.ErrUnknownChannel) {
		t.Errorf("error = %v, want ErrUnknownChannel", err)
	}
}

func TestResolveRefusesUnconfiguredChannel(t *testing.T) {
	s := newSource() // has no whatsapp default

	_, err := s.Resolve(shared.ChannelWhatsApp, "")

	if !errors.Is(err, source.ErrNoAddressForChannel) {
		t.Fatalf("error = %v, want ErrNoAddressForChannel", err)
	}
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("type = %v, want ErrInvalidInput", errs.TypeOf(err))
	}
}

// An empty string in the map is the same as no entry at all -- otherwise a
// half-filled config row would produce a delivery aimed at nowhere.
func TestResolveTreatsEmptyDefaultAsMissing(t *testing.T) {
	s := newSource()
	s.DefaultAddresses[shared.ChannelEmail] = ""

	_, err := s.Resolve(shared.ChannelEmail, "")

	if !errors.Is(err, source.ErrNoAddressForChannel) {
		t.Errorf("error = %v, want ErrNoAddressForChannel", err)
	}
}

// A default written before a validation rule tightened must not silently ship
// a bad send.
func TestResolveRevalidatesTheStoredDefault(t *testing.T) {
	s := newSource()
	s.DefaultAddresses[shared.ChannelTelegram] = "ops@example.com"

	_, err := s.Resolve(shared.ChannelTelegram, "")

	if !errors.Is(err, shared.ErrInvalidAddress) {
		t.Errorf("error = %v, want ErrInvalidAddress", err)
	}
}

// The refusal must not echo back who the source is allowed to reach.
func TestResolveMessageHidesConfiguration(t *testing.T) {
	s := newSource()

	_, err := s.Resolve(shared.ChannelEmail, "attacker@example.com")

	ae, ok := errs.As(err)
	if !ok {
		t.Fatal("not an AppError")
	}
	if strings.Contains(ae.Message(), "ops@example.com") {
		t.Errorf("message leaks the configured address: %q", ae.Message())
	}
	if strings.Contains(ae.Message(), "billing-service") {
		t.Errorf("message leaks the source id: %q", ae.Message())
	}
}
