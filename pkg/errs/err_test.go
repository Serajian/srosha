package errs

import (
	"errors"
	"testing"
)

// WithErr and WithStr must ACCUMULATE onto an existing reason, not overwrite it.
func TestWithErrAndWithStrAccumulate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "WithErr then WithStr",
			got: BadRequestErr(
				"ctx",
			).WithErr(errors.New("invalid syntax")).
				WithStr("player_game_id").
				Error(),
			want: "INVALID_INPUT: ctx (invalid syntax: player_game_id)",
		},
		{
			name: "WithStr then WithErr",
			got: BadRequestErr(
				"ctx",
			).WithStr("player_game_id").
				WithErr(errors.New("invalid syntax")).
				Error(),
			want: "INVALID_INPUT: ctx (player_game_id: invalid syntax)",
		},
		{
			name: "single WithStr unchanged",
			got:  InternalErr("ctx").WithStr("boom").Error(),
			want: "INTERNAL_ERROR: ctx (boom)",
		},
		{
			name: "single WithErr unchanged",
			got:  InternalErr("ctx").WithErr(errors.New("boom")).Error(),
			want: "INTERNAL_ERROR: ctx (boom)",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.want {
				t.Fatalf("Error() = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// Several WithStr in a row must ALL be kept, joined in order — not just the last.
func TestRepeatedWithStrKeepsAll(t *testing.T) {
	t.Parallel()

	got := BadRequestErr("ctx").WithStr("a").WithStr("b").WithStr("c").Error()
	want := "INVALID_INPUT: ctx (a: b: c)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// The originally wrapped error must stay discoverable after a later WithStr — the
// accumulation must not break errors.Is/As traversal.
func TestChainedReasonStaysUnwrappable(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sentinel")
	err := BadRequestErr("ctx").WithErr(sentinel).WithStr("label")

	if !errors.Is(err, sentinel) {
		t.Fatal("errors.Is lost the wrapped error after a later WithStr")
	}

	// the top-level AppError type must still be recoverable
	if ae, ok := As(err); !ok || ae.Type() != ErrInvalidInput {
		t.Fatalf("As() = (%v, %v), want bad-request AppError", ae, ok)
	}
}
