package delivery

import "errors"

// Sentinel errors of the delivery aggregate.
var (
	ErrNoRecipients       = errors.New("at least one recipient is required")
	ErrDuplicateRecipient = errors.New("recipient listed more than once")

	ErrNotFound = errors.New("delivery not found")

	// ErrAlreadySettled says the row had already moved when we tried to write.
	// ErrInvalidTransition is the same conclusion reached from the copy we
	// hold; this one is reached from the row itself, when two workers each held
	// a pending copy and both sent.
	ErrAlreadySettled = errors.New("delivery was already settled by somebody else")

	ErrUnknownStatus        = errors.New("unknown delivery status")
	ErrUnknownFailureReason = errors.New("unknown failure reason")

	ErrMissingNotification  = errors.New("notification id is required")
	ErrMissingIDFunc        = errors.New("delivery id generator is required")
	ErrMissingFailureReason = errors.New("failure reason is required")

	// Expected on a redelivered message: treat it as "already done" and ack.
	ErrInvalidTransition = errors.New("invalid delivery status transition")
)
