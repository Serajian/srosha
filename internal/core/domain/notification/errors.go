package notification

import "errors"

// Sentinel errors of the notification aggregate. Ours are internal, the
// caller's are invalid input; the split is what keeps our bugs from reading
// like something they could fix.
var (
	ErrMissingSource    = errors.New("source is required")
	ErrMissingTimestamp = errors.New("creation timestamp is required")

	ErrNotFound = errors.New("notification not found")

	ErrEmptyBody      = errors.New("body is required")
	ErrAlreadyExpired = errors.New("expiry is not in the future")
)
