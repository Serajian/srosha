package shared

import (
	"errors"
	"time"
)

// SendKind is what a provider's refusal means for what happens next.
//
// Three values and not two booleans. A provider can refuse for reasons that are
// genuinely different -- try later, never, and "not this recipient" -- and
// modeling three states as flags makes the fourth one expensive: every caller
// grows another branch and every sender another field to remember.
type SendKind int

const (
	// SendTransient is the default, and it is the default on purpose: an
	// unclassified failure is more often a blip than a dead end, and the
	// delivery limit stops the loop either way.
	SendTransient SendKind = iota

	// SendPermanent means the provider refused the message and repeating it
	// changes nothing: a bad address, a body it will never accept.
	SendPermanent

	// SendUnreachable means the provider refused the RECIPIENT rather than the
	// message: a person who has not opened a conversation, a window that has
	// closed, a device token that is no longer registered.
	//
	// It stops the retries like SendPermanent, and it is separate because the
	// source can act on it and cannot act on the other. Nothing they write
	// differently would have helped; somebody has to talk to us first, or the
	// token has to be replaced.
	SendUnreachable
)

// Final reports whether this kind ends the delivery.
func (k SendKind) Final() bool { return k == SendPermanent || k == SendUnreachable }

// SendError says what a provider's refusal means. Without it a retry loop is
// useless: "chat not found" fails identically five times, while a 429 or a
// dropped connection deserves another go.
//
// Mapping each provider's errors into this shape is exactly what a sender
// adapter exists to hold.
type SendError struct {
	Kind SendKind

	// RetryAfter is the provider's own hint, when it gives one.
	RetryAfter time.Duration

	// Detail is the provider's words, for operators. Never handed to a client.
	Detail string

	Err error
}

func (e *SendError) Error() string { return e.Detail }
func (e *SendError) Unwrap() error { return e.Err }

// SendKindOf reads the kind out of an error. Anything unclassified is
// transient, which is the safe default: an unknown failure is more often a blip
// than a dead end.
func SendKindOf(err error) SendKind {
	var se *SendError
	if errors.As(err, &se) {
		return se.Kind
	}
	return SendTransient
}

// IsPermanentSend reports whether the send should stop, whichever way it was
// final.
func IsPermanentSend(err error) bool { return SendKindOf(err).Final() }

// SendRetryAfter returns the provider's hint, or zero when there is none.
func SendRetryAfter(err error) time.Duration {
	var se *SendError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}
