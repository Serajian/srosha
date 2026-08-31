package postgres

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository implements usecase.AuditLog.
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
		Note:       e.Note,
	})
	if err != nil {
		return failed("record audit", err)
	}
	return nil
}

// List is the newest rows first, capped at limit. No filter -- see
// usecase.Operators.Audit for why.
func (r *AuditRepository) List(ctx context.Context, limit int32) ([]usecase.AuditEntry, error) {
	rows, err := r.q(ctx).ListAudit(ctx, limit)
	if err != nil {
		return nil, failed("list audit", err)
	}

	out := make([]usecase.AuditEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.AuditEntry{
			ID:         shared.ID(row.ID),
			At:         row.At,
			ActorID:    shared.ID(row.ActorID),
			ActorEmail: row.ActorEmail,
			Verb:       row.Verb,
			TargetType: row.TargetType,
			TargetID:   row.TargetID,
			Note:       row.Note,
		})
	}
	return out, nil
}
