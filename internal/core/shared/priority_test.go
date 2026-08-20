package shared_test

import (
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// The whole silent-downgrade rule rests on this ordering. If someone reorders
// the constants, every ceiling check inverts silently -- nothing else would
// fail to compile.
func TestPriorityOrdering(t *testing.T) {
	if shared.PriorityNormal >= shared.PriorityHigh {
		t.Error("NORMAL must sort below HIGH")
	}
	if shared.PriorityHigh >= shared.PriorityCritical {
		t.Error("HIGH must sort below CRITICAL")
	}
}

func TestClamp(t *testing.T) {
	cases := []struct {
		name      string
		requested shared.Priority
		ceiling   shared.Priority
		want      shared.Priority
	}{
		{"below ceiling", shared.PriorityNormal, shared.PriorityHigh, shared.PriorityNormal},
		{"at ceiling", shared.PriorityHigh, shared.PriorityHigh, shared.PriorityHigh},
		{"one above ceiling", shared.PriorityCritical, shared.PriorityHigh, shared.PriorityHigh},
		{"far above ceiling", shared.PriorityCritical, shared.PriorityNormal, shared.PriorityNormal},
		{"ceiling is the maximum", shared.PriorityCritical, shared.PriorityCritical, shared.PriorityCritical},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.requested.Clamp(tc.ceiling); got != tc.want {
				t.Errorf("Clamp() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClampNeverRaises(t *testing.T) {
	all := []shared.Priority{shared.PriorityNormal, shared.PriorityHigh, shared.PriorityCritical}

	for _, requested := range all {
		for _, ceiling := range all {
			got := requested.Clamp(ceiling)
			if got > requested {
				t.Errorf("Clamp(%v, %v) = %v: a downgrade must never raise priority", requested, ceiling, got)
			}
			if got > ceiling {
				t.Errorf("Clamp(%v, %v) = %v: result exceeds the ceiling", requested, ceiling, got)
			}
		}
	}
}

func TestParsePriorityRoundTrip(t *testing.T) {
	for _, p := range []shared.Priority{shared.PriorityNormal, shared.PriorityHigh, shared.PriorityCritical} {
		got, err := shared.ParsePriority(p.String())
		if err != nil {
			t.Fatalf("ParsePriority(%q): %v", p.String(), err)
		}
		if got != p {
			t.Errorf("round trip turned %v into %v", p, got)
		}
	}
}

func TestParsePriorityRejectsUnknown(t *testing.T) {
	for _, bad := range []string{"URGENT", "normal", "", "1"} {
		t.Run("rejects "+bad, func(t *testing.T) {
			_, err := shared.ParsePriority(bad)
			if !errors.Is(err, shared.ErrUnknownPriority) {
				t.Errorf("error = %v, want it to wrap ErrUnknownPriority", err)
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("type = %v, want ErrInvalidInput", errs.TypeOf(err))
			}
		})
	}
}

func TestValidAndStringForOutOfRange(t *testing.T) {
	rogue := shared.Priority(7)

	if rogue.Valid() {
		t.Error("Priority(7) should not be valid")
	}
	if got := rogue.String(); got != "Priority(7)" {
		t.Errorf("String() = %q, want a debuggable form", got)
	}
}
