package webhook

import "time"

const (
	// MaxBatchInterval bounds how long an outcome may sit waiting to be sent.
	MaxBatchInterval = 5 * time.Minute

	// MaxBatchSize bounds one call, so a large fan-out cannot become a single
	// request the receiver times out on.
	MaxBatchSize = 1000
)
