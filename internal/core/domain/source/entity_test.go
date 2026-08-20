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
		ID:                "billing-service",
		Name:              "Billing",
		MaxPriority:       shared.PriorityCritical,
		IsActive:          true,
		AllowCustomTarget: false,
		DefaultTargets: map[shared.Channel]string{
			shared.ChannelEmail:    "ops@example.com",
			shared.ChannelTelegram: "-1001234567890",
		},
	}
}

func TestResolveTargetFallsBackToTheDefault(t *testing.T) {
	s := newSource()

	got, err := s.ResolveTarget(shared.ChannelEmail, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ops@example.com" {
		t.Errorf("target = %q, want the configured default", got)
	}
}

func TestResolveTargetUsesTheExplicitOneWhenAllowed(t *testing.T) {
	s := newSource()
	s.AllowCustomTarget = true

	got, err := s.ResolveTarget(shared.ChannelEmail, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "user@example.com" {
		t.Errorf("target = %q, want the explicit one", got)
	}
}

// The whole point of the flag: a leaked key for a system source must not let
// anyone message strangers.
func TestResolveTargetRefusesCustomTargetWhenNotAllowed(t *testing.T) {
	s := newSource()

	_, err := s.ResolveTarget(shared.ChannelEmail, "attacker@example.com")

	if !errors.Is(err, source.ErrCustomTargetNotAllowed) {
		t.Fatalf("error = %v, want ErrCustomTargetNotAllowed", err)
	}
	if !errs.IsType(err, errs.ErrForbidden) {
		t.Errorf("type = %v, want ErrForbidden", errs.TypeOf(err))
	}
}

// A permitted source still cannot send garbage: the shape is checked.
func TestResolveTargetValidatesTheExplicitTarget(t *testing.T) {
	s := newSource()
	s.AllowCustomTarget = true

	_, err := s.ResolveTarget(shared.ChannelTelegram, "user@example.com")

	if !errors.Is(err, shared.ErrInvalidTarget) {
		t.Errorf("error = %v, want ErrInvalidTarget", err)
	}
}

func TestResolveTargetRefusesUnconfiguredChannel(t *testing.T) {
	s := newSource() // has no whatsapp default

	_, err := s.ResolveTarget(shared.ChannelWhatsApp, "")

	if !errors.Is(err, source.ErrNoTargetForChannel) {
		t.Fatalf("error = %v, want ErrNoTargetForChannel", err)
	}
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Errorf("type = %v, want ErrInvalidInput", errs.TypeOf(err))
	}
}

// An empty string in the map is the same as no entry at all -- otherwise a
// half-filled config row would produce a delivery aimed at nowhere.
func TestResolveTargetTreatsEmptyDefaultAsMissing(t *testing.T) {
	s := newSource()
	s.DefaultTargets[shared.ChannelEmail] = ""

	_, err := s.ResolveTarget(shared.ChannelEmail, "")

	if !errors.Is(err, source.ErrNoTargetForChannel) {
		t.Errorf("error = %v, want ErrNoTargetForChannel", err)
	}
}

// A default written before a validation rule tightened must not silently ship
// a bad send.
func TestResolveTargetRevalidatesTheStoredDefault(t *testing.T) {
	s := newSource()
	s.DefaultTargets[shared.ChannelTelegram] = "ops@example.com"

	_, err := s.ResolveTarget(shared.ChannelTelegram, "")

	if !errors.Is(err, shared.ErrInvalidTarget) {
		t.Errorf("error = %v, want ErrInvalidTarget", err)
	}
}

// The refusal must not echo back who the source is allowed to reach.
func TestResolveTargetMessageHidesConfiguration(t *testing.T) {
	s := newSource()

	_, err := s.ResolveTarget(shared.ChannelEmail, "attacker@example.com")

	ae, ok := errs.As(err)
	if !ok {
		t.Fatal("not an AppError")
	}
	if strings.Contains(ae.Message(), "ops@example.com") {
		t.Errorf("message leaks the configured target: %q", ae.Message())
	}
	if strings.Contains(ae.Message(), "billing-service") {
		t.Errorf("message leaks the source id: %q", ae.Message())
	}
}
