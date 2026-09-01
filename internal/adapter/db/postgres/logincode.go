package postgres

import (
	"context"
	"math"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LoginCodeRepository implements logincode.Repository.
type LoginCodeRepository struct{ base }

func NewLoginCodeRepository(pool *pgxpool.Pool) *LoginCodeRepository {
	return &LoginCodeRepository{base{pool: pool}}
}

func (r *LoginCodeRepository) Create(ctx context.Context, c *logincode.LoginCode) error {
	err := r.q(ctx).CreateLoginCode(ctx, gen.CreateLoginCodeParams{
		ID:        c.ID.String(),
		UserID:    c.UserID.String(),
		Code:      c.Code,
		ExpiresAt: c.ExpiresAt,
		Attempts:  attemptCount(c.Attempts),
		UsedAt:    c.UsedAt,
		CreatedAt: c.CreatedAt,
	})
	if err != nil {
		return failed("create login code", err)
	}
	return nil
}

func (r *LoginCodeRepository) ReadNewest(
	ctx context.Context, userID shared.ID,
) (*logincode.LoginCode, error) {
	row, err := r.q(ctx).ReadNewestLoginCode(ctx, userID.String())
	if err != nil {
		if noRows(err) {
			return nil, errs.NotFoundErr("no sign-in code").WithErr(logincode.ErrNotFound)
		}
		return nil, failed("read newest login code", err)
	}
	return toLoginCode(row), nil
}

func (r *LoginCodeRepository) Spend(ctx context.Context, c *logincode.LoginCode) error {
	rows, err := r.q(ctx).SpendLoginCode(ctx, gen.SpendLoginCodeParams{
		ID:       c.ID.String(),
		Attempts: attemptCount(c.Attempts),
		UsedAt:   c.UsedAt,
	})
	return wroteCode(rows, err, "spend login code")
}

// Forget removes a code that was never sent, so it costs nobody a request.
//
// A missing row is not an error here. Whatever it was meant to undo is already
// undone, and the caller is on a path where a second failure would replace the
// error that actually matters.
func (r *LoginCodeRepository) Forget(ctx context.Context, id shared.ID) error {
	if err := r.q(ctx).ForgetLoginCode(ctx, id.String()); err != nil {
		return failed("forget login code", err)
	}
	return nil
}

func (r *LoginCodeRepository) CountSince(
	ctx context.Context, userID shared.ID, since time.Time,
) (int, error) {
	n, err := r.q(ctx).CountLoginCodesSince(ctx, gen.CountLoginCodesSinceParams{
		UserID: userID.String(),
		Since:  since,
	})
	if err != nil {
		return 0, failed("count login codes", err)
	}
	return int(n), nil
}

// attemptCount narrows to the column's width. The domain counts in int and the
// column is int32, and a conversion that can silently wrap is worth the four
// lines: a negative attempt count would read as a code with guesses to spare.
func attemptCount(n int) int32 {
	if n < 0 {
		return 0
	}
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}

// wroteCode reports a statement that matched nothing as a missing code. It is
// only ever called with a code that was just read, so no match means the row is
// gone rather than unchanged.
func wroteCode(rows int64, err error, op string) error {
	if err != nil {
		return failed(op, err)
	}
	if rows == 0 {
		return errs.NotFoundErr("no sign-in code").WithErr(logincode.ErrNotFound).WithStr(op)
	}
	return nil
}

// toLoginCode restores a row rather than building a new code: New is for making
// one, and a rule that tightens later must not make an old row unreadable.
func toLoginCode(row gen.LoginCode) *logincode.LoginCode {
	return &logincode.LoginCode{
		ID:        shared.ID(row.ID),
		UserID:    shared.ID(row.UserID),
		Code:      row.Code,
		ExpiresAt: row.ExpiresAt,
		Attempts:  int(row.Attempts),
		UsedAt:    row.UsedAt,
		CreatedAt: row.CreatedAt,
	}
}
