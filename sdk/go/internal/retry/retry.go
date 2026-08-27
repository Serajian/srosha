// Package retry decides which failures are worth another attempt, and how long
// to wait between them. It knows nothing about what the call was.
package retry

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Do calls fn until it succeeds, until the error is not worth repeating, or
// until the attempts run out.
//
// It is the caller's job to make sure fn is safe to repeat. For Submit that
// means an idempotency key, which the client always sends.
func Do(ctx context.Context, attempts int, fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}

	var err error
	for attempt := range attempts {
		if err = fn(); err == nil {
			return nil
		}
		if !worthRepeating(err) || attempt == attempts-1 {
			return err
		}

		select {
		case <-ctx.Done():
			// The caller's deadline wins over ours. Their error is returned
			// rather than the call's, because giving up was their decision.
			return ctx.Err()
		case <-time.After(wait(err, attempt)):
		}
	}
	return err
}

// worthRepeating is the whole of what this package decides.
//
// Unavailable and DeadlineExceeded are the service being unreachable or slow,
// and say nothing about the request. ResourceExhausted is a limit that empties
// with time. Everything else -- a bad address, a missing body, a key the
// service does not know -- would fail exactly the same way again.
func worthRepeating(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		// Not a status: a connection that never got anywhere. Worth another go.
		return true
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}

// wait grows exponentially and is jittered, so that clients which failed
// together do not return together.
func wait(err error, attempt int) time.Duration {
	start := base
	if st, ok := status.FromError(err); ok && st.Code() == codes.ResourceExhausted {
		start = rateLimitBase
	}

	d := time.Duration(float64(start) * math.Pow(2, float64(attempt)))
	if d > maxWait {
		d = maxWait
	}

	//nolint:gosec // jitter, not a secret
	spread := jitterFraction * float64(d) * (rand.Float64()*2 - 1)
	return time.Duration(float64(d) + spread)
}
