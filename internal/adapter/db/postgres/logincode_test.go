//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/logincode"
	"github.com/Serajian/srosha/internal/core/shared"
)

func TestTheNewestCodeIsTheOneThatCounts(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewLoginCodeRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	older := logincode.New(shared.ID(ulid("CD1")), u.ID, "111111", at.Add(-time.Minute))
	newer := logincode.New(shared.ID(ulid("CD2")), u.ID, "222222", at)
	for _, c := range []*logincode.LoginCode{older, newer} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.ReadNewest(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadNewest: %v", err)
	}
	if got.Code != "222222" {
		t.Errorf("code = %q, want the newest", got.Code)
	}
}

func TestSpendingACodeIsWrittenBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewLoginCodeRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	c := logincode.New(shared.ID(ulid("CD3")), u.ID, "123456", at)
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_ = c.Check("000000", at.Add(time.Second))
	if err := repo.Spend(ctx, c); err != nil {
		t.Fatalf("Spend: %v", err)
	}

	got, err := repo.ReadNewest(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadNewest: %v", err)
	}
	if got.UsedAt == nil || got.Attempts != 1 {
		t.Errorf("read back attempts=%d used=%v", got.Attempts, got.UsedAt)
	}
}

func TestCountingRecentRequests(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewLoginCodeRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	old := logincode.New(shared.ID(ulid("CD4")), u.ID, "111111", at.Add(-time.Hour))
	recent := logincode.New(shared.ID(ulid("CD5")), u.ID, "222222", at)
	for _, c := range []*logincode.LoginCode{old, recent} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	n, err := repo.CountSince(ctx, u.ID, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if n != 1 {
		t.Errorf("counted %d, want only the recent one", n)
	}
}
