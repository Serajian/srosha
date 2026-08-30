package postgres

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository implements usecase.AuditLog.
//
// It has one method and will keep one. There is no read here yet because
// nothing reads it yet -- an investigation runs a SELECT until the admin panel
// gives it a page.
type AuditRepository struct{ base }

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{base{pool: pool}}
}

func (r *AuditRepository) Record(ctx context.Context, e usecase.AuditEntry) error {
	err := r.q(ctx).RecordAudit(ctx, gen.RecordAuditParams{
		ID:         e.ID.String(),
		At:         e.At,
		ActorID:    e.ActorID.String(),
		ActorEmail: e.ActorEmail,
		Verb:       e.Verb,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
	})
	if err != nil {
		return failed("record audit", err)
	}
	return nil
}
