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
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"

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
// so naming a parent empties everything that hangs off it.
//
// users is named separately because nothing links it to sources yet. Left out,
// a user written by one test is still there for the next one, and a test that
// asks "has anybody used this address" gets the previous test's answer.
func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), "TRUNCATE sources, users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Clean is not empty: a source has an owner and cannot exist without one,
	// so the row every source test needs is part of the clean state rather
	// than something twenty-five call sites each have to remember.
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, role, is_active, created_at, updated_at)
		 VALUES ($1, 'owner@acme.test', 'customer', true, now(), now())`,
		testOwner.String())
	if err != nil {
		t.Fatalf("seed the owner: %v", err)
	}
}

// testOwner owns every source these tests create. Sources belong to somebody,
// and which somebody is not what any of these tests are about.
const testOwner = shared.ID("01K0ACCT0000000000000000AB")

// ulid is a deterministic id for a test: readable in a failure message, and
// still a valid ULID as far as the database's domain is concerned.
//
// Crockford base32 leaves out I, L, O and U so that a written id cannot be read
// two ways, and the domain enforces that -- it refused an earlier version of
// this helper. They are mapped to their neighbors rather than dropped, so two
// different suffixes stay two different ids.
func ulid(suffix string) string {
	const prefix = "01J8XKQ2R7M3NB4PZC5VD6"
	return prefix + crockford.Replace(strings.ToUpper(suffix + "0000")[:4])
}

var crockford = strings.NewReplacer("I", "J", "L", "M", "O", "P", "U", "V")
