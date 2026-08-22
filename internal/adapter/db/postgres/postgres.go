// Package postgres implements the core's repository ports over the queries sqlc
// generated. Nothing here decides anything: it turns rows into entities and
// entities into rows, and reports what the database said in the core's own
// vocabulary.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
)

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
