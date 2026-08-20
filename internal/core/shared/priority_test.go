package shared_test

import (
	"encoding/json"
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
		{
			"far above ceiling",
			shared.PriorityCritical,
			shared.PriorityNormal,
			shared.PriorityNormal,
		},
		{
			"ceiling is the maximum",
			shared.PriorityCritical,
			shared.PriorityCritical,
			shared.PriorityCritical,
		},
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
				t.Errorf(
					"Clamp(%v, %v) = %v: a downgrade must never raise priority",
					requested,
					ceiling,
					got,
				)
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

// On a wire the name is what a human reading a stuck message needs; the number
// says nothing without the source in front of you.
func TestPriorityMarshalsAsItsName(t *testing.T) {
	for _, tt := range []struct {
		p    shared.Priority
		want string
	}{
		{shared.PriorityNormal, `"NORMAL"`},
		{shared.PriorityHigh, `"HIGH"`},
		{shared.PriorityCritical, `"CRITICAL"`},
	} {
		got, err := json.Marshal(tt.p)
		if err != nil {
			t.Fatalf("Marshal(%v) error = %v", tt.p, err)
		}
		if string(got) != tt.want {
			t.Errorf("Marshal(%v) = %s, want %s", tt.p, got, tt.want)
		}
	}
}

// Written and read through the same map, so a value always survives the trip.
func TestPriorityRoundTrips(t *testing.T) {
	for _, want := range []shared.Priority{
		shared.PriorityNormal, shared.PriorityHigh, shared.PriorityCritical,
	} {
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}

		var got shared.Priority
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", b, err)
		}
		if got != want {
			t.Errorf("round trip gave %v, want %v", got, want)
		}
	}
}

func TestPriorityUnmarshalRejects(t *testing.T) {
	for _, in := range []string{`"URGENT"`, `""`, `"high"`, `1`, `null`} {
		var p shared.Priority
		if err := json.Unmarshal([]byte(in), &p); err == nil {
			t.Errorf("Unmarshal(%s) was accepted as %v", in, p)
		}
	}
}

// A value outside the three constants must not be written at all: it would
// arrive as something no reader can parse back.
func TestPriorityMarshalRejectsAnUnknownValue(t *testing.T) {
	if _, err := json.Marshal(shared.Priority(42)); err == nil {
		t.Error("Marshal accepted a priority that is not one of the three")
	}
}
