package postgres

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepository implements session.Repository.
type SessionRepository struct{ base }

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{base{pool: pool}}
}

func (r *SessionRepository) Create(ctx context.Context, s *session.Session) error {
	err := r.q(ctx).CreateSession(ctx, gen.CreateSessionParams{
		ID:         s.ID.String(),
		UserID:     s.UserID.String(),
		ExpiresAt:  s.ExpiresAt,
		LastSeenAt: s.LastSeenAt,
		CreatedAt:  s.CreatedAt,
	})
	if err != nil {
		return failed("create session", err)
	}
	return nil
}

func (r *SessionRepository) Read(ctx context.Context, id shared.ID) (*session.Session, error) {
	row, err := r.q(ctx).ReadSession(ctx, id.String())
	if err != nil {
		if noRows(err) {
			return nil, errs.NotFoundErr("session not found").WithErr(session.ErrNotFound)
		}
		return nil, failed("read session", err)
	}
	return toSession(row), nil
}

func (r *SessionRepository) Touch(ctx context.Context, s *session.Session) error {
	rows, err := r.q(ctx).TouchSession(ctx, gen.TouchSessionParams{
		ID:         s.ID.String(),
		LastSeenAt: s.LastSeenAt,
	})
	return wroteSession(rows, err, "touch session")
}

func (r *SessionRepository) Delete(ctx context.Context, id shared.ID) error {
	rows, err := r.q(ctx).DeleteSession(ctx, id.String())
	return wroteSession(rows, err, "delete session")
}

// wroteSession reports a statement that matched nothing as a missing session:
// signing out twice, or a session another request already ended.
func wroteSession(rows int64, err error, op string) error {
	if err != nil {
		return failed(op, err)
	}
	if rows == 0 {
		return errs.NotFoundErr("session not found").WithErr(session.ErrNotFound).WithStr(op)
	}
	return nil
}

func toSession(row gen.Session) *session.Session {
	return &session.Session{
		ID:         shared.ID(row.ID),
		UserID:     shared.ID(row.UserID),
		ExpiresAt:  row.ExpiresAt,
		LastSeenAt: row.LastSeenAt,
		CreatedAt:  row.CreatedAt,
	}
}
