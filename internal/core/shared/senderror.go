package shared

import (
	"errors"
	"time"
)

// SendError says whether another attempt is worth making. Without it a retry
// loop is useless: "chat not found" fails identically five times, while a 429
// or a dropped connection deserves another go.
//
// Mapping each provider's errors into this shape is exactly what a sender
// adapter exists to hold.
type SendError struct {
	// Permanent means no number of retries will change the answer.
	Permanent bool

	// RetryAfter is the provider's own hint, when it gives one.
	RetryAfter time.Duration

	// Detail is the provider's words, for operators. Never handed to a client.
	Detail string

	Err error
}

func (e *SendError) Error() string { return e.Detail }
func (e *SendError) Unwrap() error { return e.Err }

// IsPermanentSend reports whether the send should stop. An unclassified error
// counts as transient: an unknown failure is more often a blip than a dead end,
// and the broker's own delivery limit stops the loop either way.
func IsPermanentSend(err error) bool {
	var se *SendError
	if errors.As(err, &se) {
		return se.Permanent
	}
	return false
}

// SendRetryAfter returns the provider's hint, or zero when there is none.
func SendRetryAfter(err error) time.Duration {
	var se *SendError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}
