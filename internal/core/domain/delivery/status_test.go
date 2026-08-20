package delivery_test

import (
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/delivery"
)

func TestStatusValid(t *testing.T) {
	tests := []struct {
		name  string
		s     delivery.Status
		valid bool
	}{
		{"pending", delivery.StatusPending, true},
		{"sent", delivery.StatusSent, true},
		{"failed", delivery.StatusFailed, true},
		{"empty", delivery.Status(""), false},
		{"unknown", delivery.Status("DELIVERED"), false},
		{"wrong case", delivery.Status("pending"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// TestTransitions walks the whole matrix rather than the legal moves only. The
// refusals are the point: they stop a redelivered message from sending twice.
func TestTransitions(t *testing.T) {
	all := []delivery.Status{delivery.StatusPending, delivery.StatusSent, delivery.StatusFailed}

	allowed := map[delivery.Status]map[delivery.Status]bool{
		delivery.StatusPending: {delivery.StatusSent: true, delivery.StatusFailed: true},
		delivery.StatusSent:    {},
		delivery.StatusFailed:  {},
	}

	for _, from := range all {
		for _, to := range all {
			want := allowed[from][to]
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				if got := from.CanTransitionTo(to); got != want {
					t.Errorf("%s -> %s = %v, want %v", from, to, got, want)
				}
			})
		}
	}
}

// A settled delivery is final. Retries happen while it is still PENDING -- a
// transient failure writes nothing at all -- so nothing may leave SENT or
// FAILED. If this test is ever "fixed" by widening the table, redelivery can
// resend a message the recipient already has.
func TestSettledStatusesAreFinal(t *testing.T) {
	for _, s := range []delivery.Status{delivery.StatusSent, delivery.StatusFailed} {
		if !s.IsSettled() {
			t.Errorf("%s should be settled", s)
		}
		for _, to := range []delivery.Status{
			delivery.StatusPending, delivery.StatusSent, delivery.StatusFailed,
		} {
			if s.CanTransitionTo(to) {
				t.Errorf("%s must not transition to %s", s, to)
			}
		}
	}

	if delivery.StatusPending.IsSettled() {
		t.Error("pending must not be settled")
	}
}

func TestStatusString(t *testing.T) {
	if got := delivery.StatusSent.String(); got != "SENT" {
		t.Errorf("String() = %q, want %q", got, "SENT")
	}
}

func TestFailureReasonValid(t *testing.T) {
	tests := []struct {
		name  string
		r     delivery.FailureReason
		valid bool
	}{
		{"expired", delivery.FailureExpired, true},
		{"max attempts", delivery.FailureMaxAttempts, true},
		{"permanent", delivery.FailurePermanent, true},
		{"no sender", delivery.FailureNoSender, true},
		{"empty", delivery.FailureReason(""), false},
		{"unknown", delivery.FailureReason("BECAUSE"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// An empty reason must stay invalid: MarkFailed relies on it to refuse a FAILED
// delivery that says nothing about why.
func TestEmptyFailureReasonIsRejected(t *testing.T) {
	var zero delivery.FailureReason
	if zero.Valid() {
		t.Error("the zero FailureReason must not be valid")
	}
}
