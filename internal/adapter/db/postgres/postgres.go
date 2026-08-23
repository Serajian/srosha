// Package postgres implements the core's repository ports over the queries sqlc
// generated. Nothing here decides anything: it turns rows into entities and
// entities into rows, and reports what the database said in the core's own
// vocabulary.
//
// One file per domain, each holding its repository and the mapping only it
// uses. This file holds what all of them share: how a transaction is found, and
// how the driver's errors are read.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is Postgres' code for a broken unique constraint. Matching on
// the code rather than the message is the point: the message is written for a
// person and changes between versions.
const uniqueViolation = "23505"

// txKey is unexported and its own type, so nothing outside this package can put
// a transaction into a context or take one out by accident.
type txKey struct{}

// withTx is how UnitOfWork hands the transaction down. The core decides what
// must be atomic and knows nothing about how.
func withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// base is what every repository here shares: a pool, and the rule for finding
// the transaction when one is running.
type base struct {
	pool *pgxpool.Pool
}

// q answers the only question a repository asks before every statement: am I
// inside a transaction? If so the statement must join it, or the write lands
// outside the atomic block the core asked for and survives its rollback.
func (b base) q(ctx context.Context) *gen.Queries {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return gen.New(b.pool).WithTx(tx)
	}
	return gen.New(b.pool)
}

// --- the transaction itself ---------------------------------------------------

// UnitOfWork implements usecase.UnitOfWork. The core decides what must be
// atomic; this decides how, and the transaction travels down through ctx so the
// repositories join it without being handed anything.
type UnitOfWork struct{ pool *pgxpool.Pool }

func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork { return &UnitOfWork{pool: pool} }

// Atomically runs fn in one transaction and commits only if it returns nil.
//
// A transaction already in flight is joined rather than nested. Beginning a
// second one on another connection would let the inner work wait on locks the
// outer one holds, which is a deadlock that only appears under load.
//
// The rollback is deferred rather than written on each error path, so a panic
// unwinds the transaction too. After a successful commit it is a no-op.
func (u *UnitOfWork) Atomically(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return failed("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// fn's error is returned as it is. It came from the core and already says
	// what went wrong; wrapping it here would bury a sentinel the caller
	// matches on -- the duplicate idempotency key, for one.
	if err := fn(withTx(ctx, tx)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return failed("commit transaction", err)
	}
	return nil
}

// --- what the driver said ----------------------------------------------------
//
// These read pgx and Postgres, which is why they live here and not in a domain:
// the core must not learn what a SQLSTATE is. The errors they build are the
// domain's own, imported.

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

// badRow is for a row that will not map. That is an internal error and never an
// invalid input: it was valid when it was written, so if it is unreadable now
// something on our side changed and no client can fix it.
func badRow(table, id, column string, err error) error {
	return errs.InternalErr("stored data could not be read").
		WithStr(fmt.Sprintf("%s %q: column %s", table, id, column)).
		WithErr(err)
}

// --- columns that may be null ------------------------------------------------

// optional turns the domain's empty string into a null column. Storing "" would
// be worse than useless where a partial unique index is involved: it treats ""
// as a value, so two rows that named nothing at all would collide.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
