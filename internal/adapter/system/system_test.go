package system_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/system"
	"github.com/Serajian/srosha/internal/core/shared"
)

// Two binaries in two places would otherwise stamp the same event differently,
// and an expiry written in one zone would be compared against a now in another.
func TestClockIsUTC(t *testing.T) {
	now := system.Clock()

	got := now()
	if got.Location() != time.UTC {
		t.Errorf("Location() = %v, want UTC", got.Location())
	}
	if time.Since(got) > time.Minute || time.Since(got) < -time.Minute {
		t.Errorf("now() = %v, which is not now", got)
	}
}

func generator(t *testing.T, now shared.NowFunc) *system.IDs {
	t.Helper()

	g, err := system.NewIDs(now)
	if err != nil {
		t.Fatalf("NewIDs: %v", err)
	}
	return g
}

func TestNewIDsRefusesNoClock(t *testing.T) {
	if _, err := system.NewIDs(nil); err == nil {
		t.Error("a generator with no clock was accepted")
	}
}

// Every id has to be one the database's ulid domain accepts and ParseID reads
// back, or a row cannot be written with it at all.
func TestEveryIDIsOneTheRestOfTheServiceAccepts(t *testing.T) {
	g := generator(t, system.Clock())

	for range 100 {
		id := g.Generate()

		parsed, err := shared.ParseID(id.String())
		if err != nil {
			t.Fatalf("ParseID(%q): %v", id, err)
		}
		if parsed != id {
			t.Errorf("ParseID changed it: %q became %q", id, parsed)
		}
		if len(id) != 26 {
			t.Errorf("id is %d chars, want 26: %q", len(id), id)
		}
	}
}

// The whole reason for a ULID: ids sort by time, so ORDER BY id is the creation
// order and a cursor needs no second column.
func TestIDsSortByWhenTheyWereMade(t *testing.T) {
	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	g := generator(t, func() time.Time { return at })

	first := g.Generate()

	at = at.Add(time.Second)
	second := g.Generate()

	at = at.Add(time.Hour)
	third := g.Generate()

	if first >= second || second >= third {
		t.Errorf("ids did not sort by time: %q, %q, %q", first, second, third)
	}
}

// Without monotonic entropy the random halves decide the order of two ids made
// in the same millisecond, and "sorted by id" stops meaning "in the order they
// were made". A message to four recipients is written inside one millisecond.
func TestIDsMadeInOneMillisecondStillOrderByWhichCameFirst(t *testing.T) {
	frozen := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	g := generator(t, func() time.Time { return frozen })

	const n = 200
	made := make([]shared.ID, 0, n)
	for range n {
		made = append(made, g.Generate())
	}

	sorted := slices.Clone(made)
	slices.Sort(sorted)

	for i := range made {
		if made[i] != sorted[i] {
			t.Fatalf("id %d is out of order: made %q, sorted %q", i, made[i], sorted[i])
		}
	}
}

// An id that repeated would hand one row's identity to another.
func TestEveryIDIsDifferent(t *testing.T) {
	frozen := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	// The frozen clock is the hard case: every id shares a timestamp, so only
	// the entropy tells them apart.
	g := generator(t, func() time.Time { return frozen })

	seen := make(map[shared.ID]bool, 10_000)
	for range 10_000 {
		id := g.Generate()
		if seen[id] {
			t.Fatalf("id repeated: %q", id)
		}
		seen[id] = true
	}
}

// The gateway answers every request on its own goroutine, and the entropy
// reader keeps state between calls. Run with -race.
func TestConcurrentCallersGetDistinctIDs(t *testing.T) {
	g := generator(t, system.Clock())

	const callers, each = 20, 100

	var wg sync.WaitGroup
	out := make(chan shared.ID, callers*each)

	for range callers {
		wg.Go(func() {
			for range each {
				out <- g.Generate()
			}
		})
	}
	wg.Wait()
	close(out)

	seen := make(map[shared.ID]bool, callers*each)
	for id := range out {
		if seen[id] {
			t.Fatalf("id repeated across goroutines: %q", id)
		}
		seen[id] = true
	}
	if len(seen) != callers*each {
		t.Errorf("got %d ids, want %d", len(seen), callers*each)
	}
}
