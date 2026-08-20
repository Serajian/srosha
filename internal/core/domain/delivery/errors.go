package delivery

import "errors"

// Sentinel errors of the delivery aggregate. They carry identity: the
// errs.AppError says how to answer the caller, errors.Is says what happened.
// Grows one entry at a time, as the code that returns it is written.
var (
	// Caused by the caller.
	ErrNoRecipients       = errors.New("at least one recipient is required")
	ErrDuplicateRecipient = errors.New("recipient listed more than once")

	// Caused by us.
	ErrMissingNotification  = errors.New("notification id is required")
	ErrMissingIDFunc        = errors.New("delivery id generator is required")
	ErrMissingFailureReason = errors.New("failure reason is required")

	// ErrInvalidTransition is the expected outcome of a redelivered message
	// whose delivery already settled. Callers should treat it as "already done"
	// and ack, not as a failure.
	ErrInvalidTransition = errors.New("invalid delivery status transition")
)
