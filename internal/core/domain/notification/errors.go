package notification

import "errors"

// Sentinel errors of the notification aggregate. They carry identity: the
// errs.AppError says how to answer the caller, errors.Is says what happened.
// Grows one entry at a time, as the code that returns it is written.
var (
	// Caused by us -- a bug on our side, never the caller's to fix.
	ErrMissingSource    = errors.New("source is required")
	ErrMissingTimestamp = errors.New("creation timestamp is required")

	// Caused by the caller.
	ErrEmptyBody      = errors.New("body is required")
	ErrAlreadyExpired = errors.New("expiry is not in the future")
)
