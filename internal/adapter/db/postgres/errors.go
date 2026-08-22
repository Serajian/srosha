package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Serajian/srosha/pkg/errs"
)

// uniqueViolation is Postgres' code for a broken unique constraint. Matching on
// the code rather than the message is the point: the message is written for a
// person and changes between versions.
const uniqueViolation = "23505"

// noRows says the query matched nothing. It is not always a failure -- some
// ports answer "not seen before" with a nil entity -- so the caller decides.
func noRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// violates reports whether err is a unique violation on the named constraint.
// Named, because a table has several and they mean different things: a repeated
// idempotency key is the client retrying, while a repeated key hash is a bug.
func violates(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == uniqueViolation && pgErr.ConstraintName == constraint
}

// failed wraps whatever the driver said. The message is what a client may see
// and names only the operation; the reason carries the driver's text, which can
// hold a host, a port and a constraint name, and stays in the log.
func failed(op string, err error) error {
	return errs.UnavailableErr("the request could not be completed").
		WithStr(op).
		WithErr(err)
}
