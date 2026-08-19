package errs

import (
	"errors"
	"fmt"
)

// AppError separates what the caller is allowed to see from what the operator
// needs.
//
//	message -- safe to return to a client; never leaks internals
//	reason  -- the detail, for logs only; may name columns, limits, providers
//
// Keep sentinel errors alongside it rather than instead of it. A fresh
// AppError has no identity, so `errors.Is` cannot tell "invalid target" from
// "empty body" -- both are ErrInvalidInput. Attaching a sentinel with WithErr
// restores that, because Unwrap walks the reason chain:
//
//	return errs.InvalidInputErr("invalid id").
//		WithErr(shared.ErrInvalidID).
//		WithStr(fmt.Sprintf("expected %d chars, got %d", n, len(s)))
type AppError struct {
	typ     Type
	message string
	reason  error
}

func New(t Type, msg string) *AppError {
	return &AppError{typ: t, message: msg}
}

// WithErr attaches a cause. It copies rather than mutating, so a package-level
// AppError used as a template cannot be corrupted by one caller. The new cause
// is ACCUMULATED onto any existing one, so WithErr and WithStr never overwrite
// each other.
func (e *AppError) WithErr(err error) *AppError {
	cp := *e
	cp.reason = chainReason(e.reason, err)
	return &cp
}

// WithStr attaches free-form detail as a cause.
func (e *AppError) WithStr(str string) *AppError {
	cp := *e
	cp.reason = chainReason(e.reason, errors.New(str))
	return &cp
}

// chainReason keeps both causes in the Unwrap chain. The double %w produces an
// error whose Unwrap returns a slice, which errors.Is and errors.As traverse
// in full -- so a sentinel attached first is still findable after more detail
// is added on top.
func chainReason(existing, next error) error {
	switch {
	case existing == nil:
		return next
	case next == nil:
		return existing
	default:
		return fmt.Errorf("%w: %w", existing, next)
	}
}

func (e *AppError) Error() string {
	prefix := e.typ.String() + ": " + e.message
	if e.reason != nil {
		return prefix + " (" + e.reason.Error() + ")"
	}
	return prefix
}

func (e *AppError) Unwrap() error { return e.reason }

func (e *AppError) Type() Type      { return e.typ }
func (e *AppError) Message() string { return e.message }
func (e *AppError) Reason() error   { return e.reason }

// --- constructors ----------------------------------------------------------

func InvalidInputErr(msg string) *AppError { return New(ErrInvalidInput, msg) }
func UnauthorizedErr(msg string) *AppError { return New(ErrUnauthorized, msg) }
func ForbiddenErr(msg string) *AppError    { return New(ErrForbidden, msg) }
func NotFoundErr(msg string) *AppError     { return New(ErrNotFound, msg) }
func DuplicateErr(msg string) *AppError    { return New(ErrDuplicateEntry, msg) }
func TooManyErr(msg string) *AppError      { return New(ErrTooMany, msg) }
func InternalErr(msg string) *AppError     { return New(ErrInternal, msg) }
func UnavailableErr(msg string) *AppError  { return New(ErrUnavailable, msg) }
func TimeoutErr(msg string) *AppError      { return New(ErrTimeout, msg) }

// --- inspection ------------------------------------------------------------

// As extracts the AppError from anywhere in the chain.
func As(err error) (*AppError, bool) {
	var ae *AppError
	ok := errors.As(err, &ae)
	return ae, ok
}

// IsType reports whether err is an AppError of the given type. Adapters use it
// to decide a response without unpacking the error themselves.
func IsType(err error, t Type) bool {
	ae, ok := As(err)
	return ok && ae.typ == t
}

// TypeOf returns the classification of err, or ErrInternal for anything that
// is not an AppError. An unclassified error reaching a boundary is a bug or an
// infrastructure failure, and both are safest reported as internal rather than
// as something the client could act on.
func TypeOf(err error) Type {
	if ae, ok := As(err); ok {
		return ae.typ
	}
	return ErrInternal
}
