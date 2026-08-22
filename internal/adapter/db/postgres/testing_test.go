//go:build integration

// These tests need a real database. The build tag keeps them out of `go test
// ./...` entirely, so the ordinary loop stays fast and works on a machine with
// no postgres; `make test-integration` builds with the tag.
//
// Run the dependencies first: make dev-up && make migrate-up
package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dsn is where the local dependencies listen. Overridable so the same tests can
// be pointed at a throwaway database in CI.
func dsn() string {
	if v := os.Getenv("NOTIF_TEST_DB_DSN"); v != "" {
		return v
	}
	return "postgres://srosha:srosha@127.0.0.1:7001/srosha?sslmode=disable"
}

// connect skips rather than fails when nothing is listening. Somebody running
// with the tag but without the containers should be told what is missing, not
// handed a wall of connection errors.
func connect(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn())
	if err != nil {
		t.Skipf("no database: %v (run: make dev-up && make migrate-up)", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("no database: %v (run: make dev-up && make migrate-up)", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// truncate leaves each test a clean database. CASCADE follows the foreign keys,
// so naming sources alone empties everything that hangs off it.
func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), "TRUNCATE sources CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// ulid is a deterministic id for a test: readable in a failure message, and
// still a valid ULID as far as the database's domain is concerned.
func ulid(suffix string) string {
	const prefix = "01J8XKQ2R7M3NB4PZC5VD6"
	return prefix + (suffix + "0000")[:4]
}
