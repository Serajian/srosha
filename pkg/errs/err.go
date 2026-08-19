package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a public, permanent business error code that clients branch on. Its
// zero value means "no specific meaning — fall back to the HTTP status". The
// numbers themselves live in internal/core/shared/errcode, not here, so this
// package stays domain-neutral.
type Code int

type AppError struct {
	typ        Type
	message    string
	statusCode int
	reason     error
	code       Code
}

func New(t Type, msg string, code int) *AppError {
	return &AppError{typ: t, message: msg, statusCode: code}
}

// WithErr immutable-style. Accumulates onto any existing reason instead of
// replacing it, so WithErr and WithStr no longer overwrite each other.
func (e *AppError) WithErr(err error) *AppError {
	cp := *e
	cp.reason = chainReason(e.reason, err)
	return &cp
}

func (e *AppError) WithStr(str string) *AppError {
	cp := *e
	cp.reason = chainReason(e.reason, errors.New(str))
	return &cp
}

// chainReason combines an existing reason with a new one rather than dropping the
// old one. Both stay in the Unwrap chain, so errors.Is/As keep traversing them.
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

// WithCode attaches a business error code, immutable-style like WithErr.
func (e *AppError) WithCode(c Code) *AppError {
	cp := *e
	cp.code = c
	return &cp
}

func (e *AppError) Unwrap() error { return e.reason }

func (e *AppError) Type() Type      { return e.typ }
func (e *AppError) Message() string { return e.message }
func (e *AppError) StatusCode() int { return e.statusCode }
func (e *AppError) Reason() error   { return e.reason }
func (e *AppError) Code() Code      { return e.code }

// helpers

func InternalErr(
	msg string,
) *AppError {
	return New(ErrInternal, msg, http.StatusInternalServerError)
}

func UnauthorizedErr(
	msg string,
) *AppError {
	return New(ErrUnauthorized, msg, http.StatusUnauthorized)
}

func BadRequestErr(
	msg string,
) *AppError {
	return New(ErrInvalidInput, msg, http.StatusBadRequest)
}
func NotFoundErr(msg string) *AppError  { return New(ErrNotFound, msg, http.StatusNotFound) }
func ForbiddenErr(msg string) *AppError { return New(ErrForbidden, msg, http.StatusForbidden) }

func TooManyErr(
	msg string,
) *AppError {
	return New(ErrTooMany, msg, http.StatusTooManyRequests)
}

func UnavailableErr(msg string) *AppError {
	return New(ErrUnavailable, msg, http.StatusServiceUnavailable)
}
func TimeoutErr(msg string) *AppError   { return New(ErrTimeout, msg, http.StatusGatewayTimeout) }
func DuplicateErr(msg string) *AppError { return New(ErrDuplicateEntry, msg, http.StatusConflict) }

// As type guard
func As(err error) (*AppError, bool) {
	var ae *AppError
	ok := errors.As(err, &ae)
	return ae, ok
}
