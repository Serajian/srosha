package notification

import "errors"

// Sentinel errors of the notification aggregate. Ours are internal, the
// caller's are invalid input; the split is what keeps our bugs from reading
// like something they could fix.
var (
	ErrMissingSource    = errors.New("source is required")
	ErrMissingTimestamp = errors.New("creation timestamp is required")

	ErrNotFound = errors.New("notification not found")

	// ErrDuplicateKey says this idempotency key was stored by somebody else
	// between our check and our write. It is not a failure: the caller reads
	// the original back and answers as it would have if the check had caught it.
	ErrDuplicateKey = errors.New("idempotency key already used")

	ErrEmptyBody      = errors.New("body is required")
	ErrAlreadyExpired = errors.New("expiry is not in the future")
	ErrEmptyWindow    = errors.New("the time window ends before it starts")
)
