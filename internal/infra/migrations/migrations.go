// Package migrations applies a set of sql files to a database and reports what
// it has applied. It owns goose and knows nothing about what the files say.
//
// Separate from internal/infra/database, which opens the connection: that
// package's own charter is that it knows nothing about what this service
// stores, and migrations are exactly that. One opens the door, the other walks
// through it.
package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// lockProbe is how often the lock is retried while another migration holds it.
// goose takes a period and a count rather than a duration, and their product is
// the wait, so this is the divisor that turns one into the other.
const lockProbe = 5 * time.Second

// Config is what this package needs. Nothing here has a default: waiting for a
// lock is an operational decision and comes from the service's settings.
type Config struct {
	// LockTimeout bounds how long to wait for another migration to finish.
	// Waiting forever would turn two simultaneous deploys into a hung release
	// rather than a failed one.
	LockTimeout time.Duration
}

// Runner applies migrations under a Postgres advisory lock.
type Runner struct{ provider *goose.Provider }

// New builds a runner over an already-open handle.
//
// The handle must allow only one connection at a time. A session lock belongs
// to the session that took it, so with a pool goose could take the lock on one
// connection and migrate on another -- a lock that guards nothing. See
// database.OpenSQL, which is the only thing that should be passed here.
func New(db *sql.DB, files fs.FS, cfg Config) (*Runner, error) {
	if cfg.LockTimeout < lockProbe {
		return nil, fmt.Errorf(
			"migration lock timeout is %s, and the lock is retried every %s",
			cfg.LockTimeout, lockProbe)
	}

	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockTimeout(
			uint64(lockProbe.Seconds()),
			uint64(cfg.LockTimeout/lockProbe),
		),
	)
	if err != nil {
		return nil, err
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres, db, files, goose.WithSessionLocker(locker),
	)
	if err != nil {
		return nil, err
	}
	return &Runner{provider: provider}, nil
}

// Applied is one migration that ran.
type Applied struct {
	Version int64
	File    string
}

// Up applies everything pending and reports what it did, in order.
func (r *Runner) Up(ctx context.Context) ([]Applied, error) {
	results, err := r.provider.Up(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Applied, 0, len(results))
	for _, res := range results {
		out = append(out, Applied{Version: res.Source.Version, File: res.Source.Path})
	}
	return out, nil
}

// State is one migration and whether the database has it.
type State struct {
	Version   int64
	File      string
	Applied   bool
	AppliedAt time.Time
}

// Status reports what the database has, and changes nothing.
func (r *Runner) Status(ctx context.Context) ([]State, error) {
	rows, err := r.provider.Status(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]State, 0, len(rows))
	for _, s := range rows {
		out = append(out, State{
			Version:   s.Source.Version,
			File:      s.Source.Path,
			Applied:   s.State == goose.StateApplied,
			AppliedAt: s.AppliedAt,
		})
	}
	return out, nil
}
