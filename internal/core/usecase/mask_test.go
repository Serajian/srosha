// This test is in the package rather than beside it because the case that
// matters most -- the empty string -- cannot be produced through the public
// surface: every real Recipient's address is validated before a Delivery
// exists at all, and validation refuses an empty one. mask has to be reached
// directly to cover that, and the short and boundary cases are tested here
// beside it rather than split across two styles of test for the same
// function.
package usecase

import "testing"

func TestMaskHidesEverythingBelowTheThreshold(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty string", "", "…"},
		{"a short numeric chat id", "654321", "…"},
		{"one below the threshold", "1234567", "…"},
		{"at the threshold", "12345678", "12…78"},
		{"one above the threshold", "123456789", "12…89"},
		{"a long email", "billing@acme.test", "bi…st"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mask(tt.in); got != tt.want {
				t.Errorf("mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The property mask exists for, stated directly rather than through one more
// example: below the threshold the original length is not just concealed in
// content but the output carries no trace of it at all -- every short input
// collapses to the same single-character answer.
func TestMaskBelowTheThresholdRevealsNoLength(t *testing.T) {
	for _, in := range []string{"", "1", "12", "654321", "1234567"} {
		if got := mask(in); got != "…" {
			t.Errorf("mask(%q) = %q, want the opaque placeholder", in, got)
		}
	}
}
