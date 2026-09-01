package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Serajian/srosha/pkg/errs"
)

// Filesystem is how much room is left where this service writes.
//
// Declared here rather than imported, because this is the package that asks the
// question. Total comes back with it so the alert can say what the free bytes
// are a share of -- 4GB left is a different sentence on a 20GB disk and on a
// 1TB one.
type Filesystem interface {
	Free(path string) (available, total uint64, err error)
}

// StoredBytes is how much of that disk is srosha's own.
//
// Two of them, because the two stores are two services with two volumes and
// neither knows about the other. Both are allowed to fail: this is the part of
// the alert that is nice to have, and an alert that cannot be sent because a
// detail could not be gathered is worse than an alert missing a detail.
type StoredBytes interface {
	Bytes(ctx context.Context) (uint64, error)
}

// Capacity tells the operator before the disk fills, not after.
//
// The failure it exists for is not subtle: when the disk fills, Postgres stops
// being able to write and takes the service with it. What is subtle is that
// nothing announces it. Disk usage is not a request anybody makes, so there is
// no handler that could notice, and on 2026-09-01 this host sat at 88% with
// nobody aware of it.
//
// Note what it does NOT do: nothing here deletes anything or refuses anything.
// Retention already drops what nobody will ask about again, on a schedule
// somebody chose. This only says so out loud.
type Capacity struct {
	fs      Filesystem
	alert   Alerter
	log     *slog.Logger
	sources []namedStore

	// path is the mount point to ask about, and it is "/" rather than a
	// volume: the volumes of postgres and nats live on the same filesystem, so
	// asking about the root asks about all of them at once.
	path string

	// floor is the free-bytes threshold. Free bytes and not a percentage: 90%
	// of a 96GB disk is 10GB of room and 90% of a 20GB disk is 2GB, and it is
	// the room that decides whether anything still works.
	floor uint64

	// low is what was true last time, so a crossing is announced once instead
	// of every time the job runs. Same shape as the readiness watcher, and for
	// the same reason: an alert that repeats on a timer is one people learn to
	// scroll past.
	low bool
}

type namedStore struct {
	name  string
	store StoredBytes
}

func NewCapacity(
	fs Filesystem, alert Alerter, log *slog.Logger, path string, floor uint64,
) *Capacity {
	return &Capacity{fs: fs, alert: alert, log: log, path: path, floor: floor}
}

// Watch adds a store whose size appears in the alert's detail.
//
// Nothing is required to be registered. With none, the alert still says how
// much room is left, which is the part that matters.
func (c *Capacity) Watch(name string, s StoredBytes) {
	if s == nil {
		return
	}
	c.sources = append(c.sources, namedStore{name: name, store: s})
}

// Check runs on the scheduler and is the whole of it.
func (c *Capacity) Check(ctx context.Context) error {
	available, total, err := c.fs.Free(c.path)
	if err != nil {
		return errs.InternalErr("the request could not be completed").
			WithStr("read free space at " + c.path).
			WithErr(err)
	}

	switch {
	case available < c.floor && !c.low:
		c.low = true
		c.alert.Notify(ctx, "disk is running out", c.detail(ctx, available, total))
		c.log.WarnContext(ctx, "disk is running out",
			"path", c.path, "available", available, "floor", c.floor)

	case available >= c.floor && c.low:
		c.low = false
		c.alert.Notify(ctx, "disk has room again", c.detail(ctx, available, total))
		c.log.InfoContext(ctx, "disk has room again",
			"path", c.path, "available", available)
	}
	return nil
}

// detail says how much is left, and how much of what is gone is ours.
//
// That second half is what makes the alert actionable at three in the morning.
// This host is shared with dozens of unrelated applications, so the first
// question is always whether the problem is srosha's -- and the answer is
// usually no, which changes what the reader does next.
func (c *Capacity) detail(ctx context.Context, available, total uint64) string {
	d := fmt.Sprintf("%s free of %s at %s", gb(available), gb(total), c.path)

	var ours uint64
	for _, s := range c.sources {
		n, err := s.store.Bytes(ctx)
		if err != nil {
			// Said in the alert rather than swallowed: a missing number and a
			// number that is missing because something is broken read very
			// differently to somebody deciding what to do next.
			d += fmt.Sprintf("\n%s: unknown", s.name)
			c.log.WarnContext(ctx, "capacity: store size unavailable",
				"store", s.name, "error", err)
			continue
		}
		ours += n
		d += fmt.Sprintf("\n%s: %s", s.name, gb(n))
	}

	if len(c.sources) > 0 && ours > 0 && total > available {
		d += fmt.Sprintf("\nsrosha is %s of the %s in use",
			gb(ours), gb(total-available))
	}
	return d
}

func gb(b uint64) string {
	const unit = 1 << 30
	if b < unit {
		return fmt.Sprintf("%d MB", b>>20)
	}
	return fmt.Sprintf("%.1f GB", float64(b)/unit)
}
