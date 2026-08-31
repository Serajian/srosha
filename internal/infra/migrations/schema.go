package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrBehind is a database whose schema is older than the binary asking.
var ErrBehind = errors.New("database schema is behind this build")

// Querier is the one question this check asks. Declared here rather than taking
// a pool, so the check can run against a connection somebody else opened and
// this package never owns one.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// EnsureCurrent reports whether the database is at least at `want`.
//
// This is why it exists: `select 1` proves a connection, a role and the right
// to query, and says nothing about whether a single table is there. A service
// that starts against an empty database answers every request with an error
// while reporting itself healthy -- which is a worse failure than being down,
// because nothing points at the cause.
//
// At least, and not exactly: a database ahead of this binary is the ordinary
// middle of a rolling release, where the migration has run and this replica is
// the old code. Expand-then-contract is what makes that safe, and refusing it
// here would turn a normal deploy into an outage.
func EnsureCurrent(ctx context.Context, q Querier, want int64) error {
	var have int64
	err := q.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied`,
	).Scan(&have)
	if err != nil {
		// The table itself is missing on a database nothing has ever migrated,
		// which is the commonest way to be behind rather than a fault of its
		// own. Saying so beats reporting a missing relation.
		return fmt.Errorf("%w: could not read the applied version: %w", ErrBehind, err)
	}

	if have < want {
		return fmt.Errorf("%w: database is at %d, this build expects %d",
			ErrBehind, have, want)
	}
	return nil
}
