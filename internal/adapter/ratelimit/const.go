package ratelimit

import "time"

// sweepEvery bounds how often the bucket map is walked. The walk is free in
// correctness terms -- a bucket it drops is one that had refilled completely,
// which is indistinguishable from one that never existed -- so this only trades
// how long an idle source's two numbers linger against how often every source
// is visited under the lock.
const sweepEvery = 5 * time.Minute
