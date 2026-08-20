package shared_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func TestParseID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want shared.ID
		ok   bool
	}{
		{"valid", "01HQ8XJ9Z4K7M2N5P6R8S9T0VW", "01HQ8XJ9Z4K7M2N5P6R8S9T0VW", true},
		{
			"lowercase is normalised",
			"01hq8xj9z4k7m2n5p6r8s9t0vw",
			"01HQ8XJ9Z4K7M2N5P6R8S9T0VW",
			true,
		},
		{"too short", "01HQ8XJ9Z4", "", false},
		{"too long", "01HQ8XJ9Z4K7M2N5P6R8S9T0VWXY", "", false},
		{"empty", "", "", false},
		{"excluded letter I", "01HQ8XJ9Z4K7M2N5P6R8S9T0VI", "", false},
		{"excluded letter U", "01HQ8XJ9Z4K7M2N5P6R8S9T0VU", "", false},
		{"punctuation", "01HQ8XJ9Z4K7M2N5P6R8S9T0V!", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shared.ParseID(tc.in)

			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != tc.want {
					t.Errorf("id = %q, want %q", got, tc.want)
				}
				return
			}

			if !errors.Is(err, shared.ErrInvalidID) {
				t.Errorf("error = %v, want it to wrap ErrInvalidID", err)
			}
			if !errs.IsType(err, errs.ErrInvalidInput) {
				t.Errorf("type = %v, want ErrInvalidInput", errs.TypeOf(err))
			}
			if got != "" {
				t.Errorf("id = %q, want empty on failure", got)
			}
		})
	}
}

// The message is what a client sees, the reason is what the operator sees.
// They must not be the same string, or the split buys nothing.
func TestParseIDKeepsDetailOutOfTheClientMessage(t *testing.T) {
	_, err := shared.ParseID("01HQ8XJ9Z4")

	ae, ok := errs.As(err)
	if !ok {
		t.Fatal("not an AppError")
	}
	if strings.Contains(ae.Message(), "26") {
		t.Errorf("message leaks the internal format: %q", ae.Message())
	}
	if ae.Reason() == nil || !strings.Contains(ae.Reason().Error(), "got 10") {
		t.Errorf("reason lost the detail: %v", ae.Reason())
	}
}

func TestIDZeroValue(t *testing.T) {
	var id shared.ID
	if !id.IsZero() {
		t.Error("the zero ID should report IsZero")
	}
	if id.String() != "" {
		t.Errorf("String() = %q, want empty", id.String())
	}
}
