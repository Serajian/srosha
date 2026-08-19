package errs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/pkg/errs"
)

var (
	errSentinelA = errors.New("sentinel a")
	errSentinelB = errors.New("sentinel b")
)

func TestSentinelStaysFindableAfterMoreDetail(t *testing.T) {
	err := errs.InvalidInputErr("invalid id").
		WithErr(errSentinelA).
		WithStr("expected 26 chars, got 10")

	if !errors.Is(err, errSentinelA) {
		t.Error("sentinel was lost once detail was appended")
	}
	if !strings.Contains(err.Error(), "expected 26 chars") {
		t.Errorf("detail missing from message: %q", err.Error())
	}
}

func TestBothCausesSurvive(t *testing.T) {
	err := errs.InternalErr("write failed").
		WithErr(errSentinelA).
		WithErr(errSentinelB)

	if !errors.Is(err, errSentinelA) {
		t.Error("first cause was dropped")
	}
	if !errors.Is(err, errSentinelB) {
		t.Error("second cause was dropped")
	}
}

func TestWithErrDoesNotMutateTheOriginal(t *testing.T) {
	base := errs.NotFoundErr("notification not found")
	derived := base.WithErr(errSentinelA)

	if base.Reason() != nil {
		t.Error("the template was mutated; a shared package-level error would be corrupted")
	}
	if !errors.Is(derived, errSentinelA) {
		t.Error("the copy lost its cause")
	}
}

func TestAsFindsAppErrorThroughWrapping(t *testing.T) {
	wrapped := errs.DuplicateErr("idempotency key already used").WithErr(errSentinelA)

	ae, ok := errs.As(wrapped)
	if !ok {
		t.Fatal("As did not find the AppError")
	}
	if ae.Type() != errs.ErrDuplicateEntry {
		t.Errorf("type = %v, want ErrDuplicateEntry", ae.Type())
	}
	if ae.Message() != "idempotency key already used" {
		t.Errorf("message = %q", ae.Message())
	}
}

func TestTypeOfTreatsUnclassifiedAsInternal(t *testing.T) {
	if got := errs.TypeOf(errors.New("some driver blew up")); got != errs.ErrInternal {
		t.Errorf("type = %v, want ErrInternal", got)
	}
	if got := errs.TypeOf(nil); got != errs.ErrInternal {
		t.Errorf("type = %v, want ErrInternal", got)
	}
}

func TestIsType(t *testing.T) {
	err := errs.TooManyErr("rate limit exceeded")

	if !errs.IsType(err, errs.ErrTooMany) {
		t.Error("IsType missed the matching type")
	}
	if errs.IsType(err, errs.ErrNotFound) {
		t.Error("IsType matched the wrong type")
	}
}
