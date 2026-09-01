package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SizeReporter answers how much disk this database occupies.
//
// Not a repository: it maps no entity and reads no row of ours. It is here
// because this is the package that holds a pool, and the alert that uses it
// wants a number rather than a table.
type SizeReporter struct{ base }

func NewSizeReporter(pool *pgxpool.Pool) *SizeReporter {
	return &SizeReporter{base{pool: pool}}
}

// Bytes is tables, indexes and toast together -- what the volume actually
// holds, not the sum of the rows.
func (r *SizeReporter) Bytes(ctx context.Context) (uint64, error) {
	n, err := r.q(ctx).DatabaseSize(ctx)
	if err != nil {
		return 0, failed("read the database size", err)
	}
	if n < 0 {
		return 0, nil
	}
	return uint64(n), nil
}
