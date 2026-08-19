package errs

import (
	"fmt"
	"testing"
)

// A freshly built error has code 0 — the "no specific meaning" default.
func TestCodeDefaultsToZero(t *testing.T) {
	t.Parallel()

	if got := BadRequestErr("ctx").Code(); got != 0 {
		t.Fatalf("Code() = %d, want 0", got)
	}
}

// WithCode attaches the code without disturbing the rest of the error.
func TestWithCodeAttaches(t *testing.T) {
	t.Parallel()

	err := BadRequestErr("game is closed").WithCode(2002)

	if got := err.Code(); got != 2002 {
		t.Fatalf("Code() = %d, want 2002", got)
	}
	if got := err.Type(); got != ErrInvalidInput {
		t.Fatalf("Type() = %v, want ErrInvalidInput", got)
	}
	if got := err.Message(); got != "game is closed" {
		t.Fatalf("Message() = %q, want %q", got, "game is closed")
	}
}

// WithCode is immutable-style: it must not mutate the receiver.
func TestWithCodeDoesNotMutateReceiver(t *testing.T) {
	t.Parallel()

	base := BadRequestErr("ctx")
	_ = base.WithCode(2002)

	if got := base.Code(); got != 0 {
		t.Fatalf("receiver Code() = %d after WithCode, want 0 (unchanged)", got)
	}
}

// The code must survive wrapping and be recoverable through As.
func TestCodeSurvivesWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("outer: %w", BadRequestErr("game is closed").WithCode(2002))

	ae, ok := As(wrapped)
	if !ok {
		t.Fatal("As() failed to recover the AppError from a wrapped error")
	}
	if got := ae.Code(); got != 2002 {
		t.Fatalf("recovered Code() = %d, want 2002", got)
	}
}
