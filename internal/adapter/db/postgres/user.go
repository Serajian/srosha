package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository implements user.Repository.
type UserRepository struct{ base }

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{base{pool: pool}}
}

func (r *UserRepository) Create(ctx context.Context, u *user.User) error {
	err := r.q(ctx).CreateUser(ctx, gen.CreateUserParams{
		ID:        u.ID.String(),
		Email:     u.Email,
		Role:      u.Role.String(),
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	})
	if err != nil {
		return failed("create user", err)
	}
	return nil
}

func (r *UserRepository) ReadByEmail(ctx context.Context, email string) (*user.User, error) {
	row, err := r.q(ctx).ReadUserByEmail(ctx, email)
	if err != nil {
		return nil, readUserErr("read user by email", err)
	}
	return toUser(row.ID, row.Email, row.Role, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

func (r *UserRepository) ReadByID(ctx context.Context, id shared.ID) (*user.User, error) {
	row, err := r.q(ctx).ReadUserByID(ctx, id.String())
	if err != nil {
		return nil, readUserErr("read user by id", err)
	}
	return toUser(row.ID, row.Email, row.Role, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

func (r *UserRepository) List(ctx context.Context, limit int32) ([]user.User, error) {
	rows, err := r.q(ctx).ListUsers(ctx, limit)
	if err != nil {
		return nil, failed("list users", err)
	}
	out := make([]user.User, 0, len(rows))
	for _, row := range rows {
		out = append(
			out,
			*toUser(row.ID, row.Email, row.Role, row.IsActive, row.CreatedAt, row.UpdatedAt),
		)
	}
	return out, nil
}

// UpdateRole writes the role, and nothing a reactivation would.
func (r *UserRepository) UpdateRole(ctx context.Context, u *user.User) error {
	rows, err := r.q(ctx).UpdateUserRole(ctx, gen.UpdateUserRoleParams{
		ID:        u.ID.String(),
		Role:      u.Role.String(),
		UpdatedAt: u.UpdatedAt,
	})
	return changed(rows, err, "update user role")
}

// SetActive writes whether somebody may sign in, and nothing a role change
// would.
func (r *UserRepository) SetActive(ctx context.Context, u *user.User) error {
	rows, err := r.q(ctx).SetUserActive(ctx, gen.SetUserActiveParams{
		ID:        u.ID.String(),
		IsActive:  u.IsActive,
		UpdatedAt: u.UpdatedAt,
	})
	return changed(rows, err, "set user active")
}

// readUserErr turns "no rows" into the domain's own sentinel. A caller has to
// tell an address nobody has used from a database that would not answer.
func readUserErr(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	return failed(what, err)
}

// toUser does not call user.New: New is for building a person, and this is
// restoring one that was already valid when it was written. A rule that
// tightens later must not make an old row unreadable.
func toUser(
	id, email, role string, isActive bool, createdAt, updatedAt time.Time,
) *user.User {
	return &user.User{
		ID:        shared.ID(id),
		Email:     email,
		Role:      user.Role(role),
		IsActive:  isActive,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
