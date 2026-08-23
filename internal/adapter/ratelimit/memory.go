// Package ratelimit answers whether a source may send again right now.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"golang.org/x/time/rate"
)

// Memory holds one token bucket per source, in this process.
//
// In this process is the whole caveat, and it is not a small one: two gateways
// behind a load balancer keep two sets of buckets, so the quota a customer
// actually gets is the configured one times the number of instances. That is
// acceptable while one instance runs, and it is why this file is called memory
// -- the day a second instance goes up, the buckets have to move to redis, and
// only this file changes.
//
// A token bucket rather than a count per minute. A fixed window lets a customer
// spend a whole minute's quota at 11:59:59 and another at 12:00:00 -- both
// windows are within the limit, and the service ate twice the limit in two
// seconds. A bucket has no edge to stand on:
//
//	capacity  = the configured quota, so a natural batch is not refused
//	refill    = that quota spread over a minute, so the long run holds
type Memory struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter

	limit rate.Limit
	burst int

	// now is injected so a test can move an hour forward rather than wait one.
	now shared.NowFunc

	lastSweep time.Time
}

// NewMemory refuses a quota of zero rather than starting with one. Zero would
// mean a bucket that never refills, which is not a rate limit -- it is every
// source refused forever, discovered on the first request rather than at boot.
func NewMemory(perMinute int, now shared.NowFunc) (*Memory, error) {
	if perMinute <= 0 {
		return nil, errs.InternalErr("rate limit must be above zero").
			WithStr(fmt.Sprintf("got %d per minute", perMinute))
	}
	if now == nil {
		return nil, errs.InternalErr("rate limiter has no clock")
	}

	return &Memory{
		buckets:   make(map[string]*rate.Limiter),
		limit:     rate.Limit(float64(perMinute) / time.Minute.Seconds()),
		burst:     perMinute,
		now:       now,
		lastSweep: now(),
	}, nil
}

// Allow spends one unit of the source's quota, and reports whether there was one
// to spend.
//
// The context and the error are unused here and are not going to be removed:
// the port is written for the implementation that keeps buckets in redis, where
// a call travels and can fail. A limiter that cannot answer must be able to say
// so rather than guess.
//
// A refusal spends nothing. A source that has run out is not pushed further
// under by being refused.
func (m *Memory) Allow(_ context.Context, sourceID string) (bool, error) {
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweep(now)

	bucket, ok := m.buckets[sourceID]
	if !ok {
		bucket = rate.NewLimiter(m.limit, m.burst)
		m.buckets[sourceID] = bucket
	}
	return bucket.AllowN(now, 1), nil
}

// sweep drops every bucket that has refilled completely.
//
// This is the whole of the memory management, and it needs no expiry setting
// because there is nothing to tune: a full bucket answers exactly as a bucket
// that has never existed does, so throwing one away cannot change any future
// answer. A source that comes back simply gets a new full bucket.
//
// It runs inline rather than on a goroutine of its own. A goroutine is a thing
// that has to be shut down, which would mean a place in the registry with a
// tier and a Close -- a lot of ceremony for walking a map.
func (m *Memory) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < sweepEvery {
		return
	}
	m.lastSweep = now

	full := float64(m.burst)
	for id, bucket := range m.buckets {
		if bucket.TokensAt(now) >= full {
			delete(m.buckets, id)
		}
	}
}
