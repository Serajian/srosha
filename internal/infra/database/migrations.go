package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// OpenSQL returns a database/sql handle on the same Postgres the pool reaches.
//
// The pool is pgx's own type and the migration tool is not: goose takes a
// *sql.DB, so this opens one through pgx's stdlib driver rather than adding a
// second driver to the module. Same protocol, same dsn, one dependency.
//
// It is deliberately not part of Postgres: a migration runs once and exits,
// and a service's pool is tuned for the opposite -- many connections held for
// a long time. Sharing one would mean tuning it for neither.
func OpenSQL(dsn string, connectTimeout time.Duration) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("migration database: %w", err)
	}
	cfg.ConnectTimeout = connectTimeout

	db := stdlib.OpenDB(*cfg)

	// One connection, and it matters: a session lock belongs to the session
	// that took it. With a pool, goose could take the lock on one connection
	// and run the migration on another, which is a lock that guards nothing.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}
