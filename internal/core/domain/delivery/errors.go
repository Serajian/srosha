package delivery

import "errors"

// Sentinel errors of the delivery aggregate.
var (
	ErrNoRecipients       = errors.New("at least one recipient is required")
	ErrDuplicateRecipient = errors.New("recipient listed more than once")

	ErrNotFound = errors.New("delivery not found")

	ErrMissingNotification  = errors.New("notification id is required")
	ErrMissingIDFunc        = errors.New("delivery id generator is required")
	ErrMissingFailureReason = errors.New("failure reason is required")

	// Expected on a redelivered message: treat it as "already done" and ack.
	ErrInvalidTransition = errors.New("invalid delivery status transition")
)
