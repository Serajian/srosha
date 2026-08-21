// Package database opens and owns the connection pool. It knows how to reach
// Postgres and nothing about what this service stores there.
package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is bootstrap's job.
//
// Nothing here has a default. Every value is an operational decision, so it
// comes from config and is named in one place rather than two.
type Config struct {
	DSN string

	MaxConns        int
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration

	// HealthCheckPeriod is how often the pool notices a connection died under
	// it -- after a failover, or a firewall dropping an idle socket.
	HealthCheckPeriod time.Duration

	// ConnectTimeout bounds one attempt; Attempts and Backoff bound the loop.
	// Together they cover the seconds a database takes to accept connections
	// after its container starts, and no longer: past that the fault is real.
	ConnectTimeout  time.Duration
	ConnectAttempts int
	ConnectBackoff  time.Duration
}

func (c Config) validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	// An empty dsn parses into a local-socket default and then spends the whole
	// retry loop failing at something nobody configured.
	check(strings.TrimSpace(c.DSN) != "", "dsn is empty")
	check(c.MaxConns > 0 && c.MaxConns <= maxPoolConns,
		"max conns %d is outside 1..%d", c.MaxConns, maxPoolConns)
	check(c.MaxConnLifetime > 0, "max conn lifetime must be above zero")
	check(c.MaxConnIdleTime > 0, "max conn idle time must be above zero")
	check(c.HealthCheckPeriod > 0, "health check period must be above zero")
	check(c.ConnectTimeout > 0, "connect timeout must be above zero")
	check(c.ConnectAttempts > 0, "connect attempts must be above zero")
	check(c.ConnectBackoff > 0, "connect backoff must be above zero")

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("database: %w", errors.Join(errs...))
}

// Postgres owns the pool: it opens it, proves it works, answers for its health
// and closes it. Nothing else in the process may close it.
type Postgres struct {
	cfg  Config
	log  *slog.Logger
	pool *pgxpool.Pool
}

// New checks the configuration and touches nothing. Connect does the I/O, so a
// wiring mistake is found before anything is dialed.
func New(cfg Config, log *slog.Logger) (*Postgres, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Postgres{cfg: cfg, log: log}, nil
}

// Connect opens the pool and does not return until it has run a real query.
func (p *Postgres) Connect(ctx context.Context) error {
	if p.pool != nil {
		return errors.New("database: already connected")
	}

	cfg, err := pgxpool.ParseConfig(p.cfg.DSN)
	if err != nil {
		// The dsn carries the password, so it must not reach the message.
		return fmt.Errorf("database: dsn cannot be parsed: %w", p.redact(err))
	}

	cfg.MaxConns = int32(p.cfg.MaxConns) //nolint:gosec // bounded in validate
	cfg.MaxConnLifetime = p.cfg.MaxConnLifetime
	cfg.MaxConnIdleTime = p.cfg.MaxConnIdleTime
	cfg.HealthCheckPeriod = p.cfg.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = p.cfg.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("database: pool: %w", p.redact(err))
	}
	p.pool = pool

	if err := p.waitReady(ctx); err != nil {
		pool.Close()
		p.pool = nil
		return err
	}
	return nil
}

// Ping runs the same kind of query the service itself runs. pool.Ping would
// also acquire a connection and round trip, but through the simple protocol,
// while every real query here goes through the extended one. Behind a
// connection pooler the two can diverge, and a check that takes the easier path
// can report ready when the first real query would fail.
//
// It is what a readiness endpoint should call: a service that cannot reach its
// database is not ready, however alive its process is.
func (p *Postgres) Ping(ctx context.Context) error {
	if p.pool == nil {
		return errors.New("database: not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.ConnectTimeout)
	defer cancel()

	var one int
	if err := p.pool.QueryRow(ctx, "select 1").Scan(&one); err != nil {
		return fmt.Errorf("database: %w", err)
	}
	return nil
}

// Close releases the pool. It is safe to call twice, because shutdown paths
// cross and the second caller should not panic.
func (p *Postgres) Close() {
	if p.pool == nil {
		return
	}
	p.pool.Close()
	p.pool = nil
}

// Pool is the handle a repository needs. It is the driver's own type on purpose:
// infra hands out what it built, and the abstraction over it belongs to the
// adapter that consumes it.
func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }

// waitReady covers the seconds between a container starting and the database
// accepting connections.
func (p *Postgres) waitReady(ctx context.Context) error {
	var last error

	for attempt := 1; attempt <= p.cfg.ConnectAttempts; attempt++ {
		if last = p.Ping(ctx); last == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("database: %w", ctx.Err())
		}

		p.log.WarnContext(ctx, "database not ready yet",
			"attempt", attempt, "of", p.cfg.ConnectAttempts, "err", last)

		if attempt < p.cfg.ConnectAttempts {
			select {
			case <-ctx.Done():
				return fmt.Errorf("database: %w", ctx.Err())
			case <-time.After(p.cfg.ConnectBackoff):
			}
		}
	}
	return fmt.Errorf("database: not reachable after %d attempts: %w",
		p.cfg.ConnectAttempts, last)
}

// redact keeps the dsn out of an error. pgx already blanks the password in its
// own parse errors, but nothing guarantees that of every error on the way out,
// and one that quotes the dsn whole would carry the password with it.
func (p *Postgres) redact(err error) error {
	if p.cfg.DSN == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), p.cfg.DSN, "[REDACTED DSN]"))
}
