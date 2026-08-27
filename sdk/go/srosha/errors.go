package srosha

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What a call can fail with.
//
// One per code the service answers with, and no finer. srosha sends two things
// and no more -- a code, and a sentence written for a person -- so this is the
// whole of what can be told apart without matching on text.
//
// That matters most for ErrInvalidRequest, which covers an address that is not
// an address, a missing body, a listing window past retention, and a channel
// with no identity behind it. A caller could act differently on some of those,
// and this package deliberately does not let them try: the only way to tell
// them apart today is reading the message, which breaks the day somebody
// rewords a sentence. If that distinction is ever needed, the answer is a
// machine-readable reason on the wire, not cleverness here.
var (
	// ErrInvalidRequest: the request is wrong and will be wrong again. Read
	// Error.Message.
	ErrInvalidRequest = errors.New("srosha: invalid request")

	// ErrUnauthorized: no key, or one the service does not know.
	ErrUnauthorized = errors.New("srosha: unauthorized")

	// ErrForbidden: a real key, for something it may not do.
	ErrForbidden = errors.New("srosha: forbidden")

	ErrNotFound = errors.New("srosha: not found")

	// ErrDuplicate: something with that name already exists.
	ErrDuplicate = errors.New("srosha: already exists")

	// ErrRateLimited: too many requests. Worth trying again after a wait, which
	// the client already does unless retries are switched off.
	ErrRateLimited = errors.New("srosha: rate limited")

	// ErrUnavailable: the service could not be reached, or could not answer.
	ErrUnavailable = errors.New("srosha: unavailable")

	// ErrTimeout: the deadline passed. Whether the message was accepted is not
	// known -- which is why Submit always sends an idempotency key.
	ErrTimeout = errors.New("srosha: timed out")

	// ErrInternal: a fault on the service's side, or an answer this build does
	// not recognize.
	ErrInternal = errors.New("srosha: internal error")
)

// Error is what every failed call returns.
//
// It wraps one of the sentinels above, so errors.Is finds it, and carries the
// sentence srosha wrote for a person to read. There is nothing else on the
// wire: the service keeps its own reason -- which names columns, hosts and
// rejected values -- in its logs and not in its answers.
type Error struct {
	kind error

	// Message is srosha's own words. Show it; do not match on it.
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.kind.Error()
	}
	return fmt.Sprintf("%v: %s", e.kind, e.Message)
}

// Unwrap is what makes errors.Is work, and it is one method rather than a
// hand-written Is. errors.As still finds *Error for a caller who wants the
// message out.
func (e *Error) Unwrap() error { return e.kind }

// wrap turns a gRPC status into this package's error, and is the only place
// that knows codes exist.
func wrap(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		// Not a status at all: a dial that never got anywhere, or a context
		// that expired before the call left.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &Error{kind: ErrTimeout, Message: err.Error()}
		}
		return &Error{kind: ErrUnavailable, Message: err.Error()}
	}
	return &Error{kind: kindOf(st.Code()), Message: st.Message()}
}

func kindOf(c codes.Code) error {
	switch c {
	case codes.InvalidArgument:
		return ErrInvalidRequest
	case codes.Unauthenticated:
		return ErrUnauthorized
	case codes.PermissionDenied:
		return ErrForbidden
	case codes.NotFound:
		return ErrNotFound
	case codes.AlreadyExists:
		return ErrDuplicate
	case codes.ResourceExhausted:
		return ErrRateLimited
	case codes.Unavailable:
		return ErrUnavailable
	case codes.DeadlineExceeded, codes.Canceled:
		return ErrTimeout
	default:
		// Including codes.Internal, and including anything a newer service
		// might answer with that this build has never seen.
		return ErrInternal
	}
}
