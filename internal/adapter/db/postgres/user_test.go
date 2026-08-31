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

// A role written comes back, and nothing else on the row moves along with it.
func TestARoleWrittenComesBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	u := aStoredUser(t, pool, "USR", "ops@acme.test")

	u.Role = user.RoleAdmin
	u.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.UpdateRole(ctx, u); err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}

	got, err := repo.ReadByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if got.Role != user.RoleAdmin {
		t.Errorf("role = %q, want admin", got.Role)
	}
	if !got.IsActive {
		t.Error("a role change switched the account off")
	}
}

// SetActive(false) does not remove the row -- an account switched off must
// still come back, with is_active false, so it can be switched back on.
func TestSetActiveFalseDoesNotRemoveTheRow(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	u := aStoredUser(t, pool, "USR", "ops@acme.test")

	u.IsActive = false
	u.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.SetActive(ctx, u); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	got, err := repo.ReadByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("the row was removed rather than switched off: %v", err)
	}
	if got.IsActive {
		t.Error("is_active did not persist as false")
	}
	if got.Role != user.RoleCustomer {
		t.Errorf("role = %q, a deactivation changed it", got.Role)
	}
}

// List answers every account, for the page that manages them. truncate seeds
// one user of its own (the owner every source test needs), so this counts
// against that baseline rather than assuming an empty table.
func TestListAnswersEveryAccount(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)

	before, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	aStoredUser(t, pool, "US1", "a@acme.test")
	aStoredUser(t, pool, "US2", "b@acme.test")

	got, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != len(before)+2 {
		t.Fatalf("List returned %d users, want %d", len(got), len(before)+2)
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
