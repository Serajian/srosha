package retry

import "time"

// DefaultAttempts is how many times a call is made in total, not how many times
// it is retried. Three is two waits, which is enough to ride out a restart and
// short enough that a caller's own deadline still means something.
const DefaultAttempts = 3

// base and maxWait bound the exponential backoff. srosha sends no timing hint
// of its own -- a rate limit answers with a sentence and nothing else -- so
// these are entirely this package's choice.
const (
	base    = 100 * time.Millisecond
	maxWait = 5 * time.Second
)

// rateLimitBase is the first wait after being rate limited, and it is longer
// than the others on purpose: coming straight back spends the same quota again.
const rateLimitBase = time.Second

// jitterFraction is how much of a wait is randomized. Without it, every client
// that failed together comes back together.
const jitterFraction = 0.2
