// These tests are in the package rather than beside it because what the sweep
// drops is the behavior under test, and the only way to see it is to look at
// the map -- which is deliberately not part of the public surface.
package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
)

// The core defines what a rate limiter is; this is the one that keeps its
// buckets in memory. A signature that drifts stops the gateway compiling.
var _ source.RateLimiter = (*Memory)(nil)

// clock is the injected time. Nothing here waits: a test that needed a real
// minute to pass would be a test nobody runs.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock {
	return &clock{t: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func limiter(t *testing.T, perMinute int, c *clock) *Memory {
	t.Helper()

	m, err := NewMemory(perMinute, c.now)
	if err != nil {
		t.Fatalf("NewMemory: %v", err)
	}
	return m
}

// spend calls Allow n times and reports how many were allowed.
func spend(t *testing.T, m *Memory, sourceID string, n int) int {
	t.Helper()

	allowed := 0
	for range n {
		ok, err := m.Allow(context.Background(), sourceID)
		if err != nil {
			t.Fatalf("Allow: %v", err)
		}
		if ok {
			allowed++
		}
	}
	return allowed
}

func held(m *Memory) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// A quota of zero is not a rate limit, it is every source refused forever. It
// is refused at construction so it is found at boot rather than on the first
// request.
func TestNewMemoryRefusesAQuotaOfZero(t *testing.T) {
	c := newClock()

	for _, perMinute := range []int{0, -1} {
		if _, err := NewMemory(perMinute, c.now); err == nil {
			t.Errorf("NewMemory(%d) was accepted", perMinute)
		}
	}
	if _, err := NewMemory(600, nil); err == nil {
		t.Error("a limiter with no clock was accepted")
	}
}

// The bucket starts full, so a customer sending a batch is not refused halfway
// through it. That is why the capacity is the whole quota rather than a second's
// worth of it.
func TestAFullMinuteCanBeSpentAtOnce(t *testing.T) {
	c := newClock()
	m := limiter(t, 600, c)

	if got := spend(t, m, "acme", 600); got != 600 {
		t.Errorf("allowed %d of 600 with no time passing", got)
	}
	if got := spend(t, m, "acme", 1); got != 0 {
		t.Error("the 601st request was allowed with an empty bucket")
	}
}

// This is what a fixed window gets wrong: 600 at 11:59:59 and 600 at 12:00:00
// are each within the limit, and the service ate 1200 in two seconds.
func TestThereIsNoWindowEdgeToStandOn(t *testing.T) {
	c := newClock()
	m := limiter(t, 600, c)

	spend(t, m, "acme", 600)

	// One second later a fixed window would have reset to a full quota. The
	// bucket has refilled by exactly one second's worth.
	c.advance(time.Second)

	if got := spend(t, m, "acme", 600); got != 10 {
		t.Errorf("allowed %d after one second, want 10 -- a window reset would give 600", got)
	}
}

func TestTheBucketRefillsOverTime(t *testing.T) {
	c := newClock()
	m := limiter(t, 600, c)

	spend(t, m, "acme", 600)

	// Cumulative: each step continues from where the last one left off.
	tests := []struct {
		name    string
		advance time.Duration
		want    int
	}{
		{"a second buys ten", time.Second, 10},
		{"three more buy thirty", 3 * time.Second, 30},
		{"a minute buys the whole quota back", time.Minute, 600},
		{"and it never holds more than the quota", 10 * time.Minute, 600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.advance(tt.advance)
			if got := spend(t, m, "acme", 700); got != tt.want {
				t.Errorf("allowed %d, want %d", got, tt.want)
			}
		})
	}
}

// The quota is per source. One customer running out must not touch another.
func TestSourcesDoNotShareABucket(t *testing.T) {
	c := newClock()
	m := limiter(t, 10, c)

	if got := spend(t, m, "acme", 10); got != 10 {
		t.Fatalf("acme was allowed %d of 10", got)
	}
	if got := spend(t, m, "acme", 1); got != 0 {
		t.Fatal("acme was allowed past its quota")
	}

	if got := spend(t, m, "globex", 10); got != 10 {
		t.Errorf("globex was allowed %d of 10 -- it paid for acme's traffic", got)
	}
}

// A bucket that has refilled completely answers exactly as one that never
// existed, so dropping it cannot change any later answer. That is what lets the
// sweep run with no expiry setting to get wrong.
func TestIdleSourcesAreForgottenAndNothingChanges(t *testing.T) {
	c := newClock()
	m := limiter(t, 600, c)

	spend(t, m, "acme", 600)
	if held(m) != 1 {
		t.Fatalf("holding %d buckets, want 1", held(m))
	}

	// Long enough for acme's bucket to refill and for a sweep to be due.
	c.advance(time.Hour)
	spend(t, m, "globex", 1)

	if held(m) != 1 {
		t.Errorf("holding %d buckets, want only globex's", held(m))
	}
	if got := spend(t, m, "acme", 600); got != 600 {
		t.Errorf("acme came back to %d of its quota, want all of it", got)
	}
}

// A source that is still spending must survive the sweep, or its quota would
// reset every time the map happened to be walked.
func TestASourceStillSpendingIsNotForgotten(t *testing.T) {
	c := newClock()
	m := limiter(t, 600, c)

	// Get the first sweep out of the way, so the next one is due exactly
	// sweepEvery from here.
	c.advance(sweepEvery)
	spend(t, m, "globex", 1)

	// Ten seconds before that next sweep, acme empties its bucket.
	c.advance(sweepEvery - 10*time.Second)
	spend(t, m, "acme", 600)

	// The sweep now runs. acme has refilled ten seconds' worth -- a hundred
	// tokens of six hundred -- so it is not full and must be kept.
	c.advance(10 * time.Second)
	spend(t, m, "globex", 1)

	if held(m) != 2 {
		t.Fatalf("holding %d buckets, want both", held(m))
	}
	if got := spend(t, m, "acme", 600); got != 100 {
		t.Errorf("acme was allowed %d, want the 100 it had refilled -- not a fresh bucket", got)
	}
}

// The gateway serves every request on its own goroutine, so this is not a
// theoretical concern. Run with -race.
func TestConcurrentCallersDoNotRaceOrOverspend(t *testing.T) {
	c := newClock()
	m := limiter(t, 100, c)

	const callers, each = 20, 20

	var wg sync.WaitGroup
	allowed := make(chan int, callers)

	for range callers {
		wg.Go(func() {
			count := 0
			for range each {
				ok, err := m.Allow(context.Background(), "acme")
				if err != nil {
					return
				}
				if ok {
					count++
				}
			}
			allowed <- count
		})
	}
	wg.Wait()
	close(allowed)

	total := 0
	for n := range allowed {
		total += n
	}
	if total != 100 {
		t.Errorf("allowed %d of 400 attempts, want exactly the quota of 100", total)
	}
}
