//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/jackc/pgx/v5/pgxpool"
)

func aUser(t *testing.T, id, email string) *user.User {
	t.Helper()

	u, err := user.New(
		shared.ID(id), email, user.RoleCustomer, time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

func TestAUserComesBackByEmailAndByID(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	u := aUser(t, ulid("USR"), "ops@acme.test")

	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byEmail, err := repo.ReadByEmail(ctx, "ops@acme.test")
	if err != nil {
		t.Fatalf("ReadByEmail: %v", err)
	}
	if byEmail.ID != u.ID || byEmail.Role != user.RoleCustomer || !byEmail.IsActive {
		t.Errorf("read back %+v", byEmail)
	}

	byID, err := repo.ReadByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if byID.Email != "ops@acme.test" {
		t.Errorf("email = %q", byID.Email)
	}
}

// Sign-in has to tell "nobody has this address" from "we could not read the
// row", and a nil with no error makes those the same answer.
func TestAnAddressNobodyHasUsed(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	_, err := postgres.NewUserRepository(pool).ReadByEmail(context.Background(), "nobody@acme.test")
	if !errors.Is(err, user.ErrNotFound) {
		t.Errorf("ReadByEmail = %v, want ErrNotFound", err)
	}
}

// One address is one account, whatever anybody types.
func TestTheSameAddressTwice(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	if err := repo.Create(ctx, aUser(t, ulid("US1"), "ops@acme.test")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, aUser(t, ulid("US2"), "ops@acme.test")); err == nil {
		t.Fatal("the same address was stored twice")
	}
}

// aStoredUser is the row every sign-in test needs first: login codes, sessions
// and audit rows all point at a user, and none of them can be written without
// one.
func aStoredUser(t *testing.T, pool *pgxpool.Pool, suffix, email string) *user.User {
	t.Helper()

	u := aUser(t, ulid(suffix), email)
	if err := postgres.NewUserRepository(pool).Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	return u
}
