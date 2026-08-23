//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
)

var errFromTheCore = errors.New("the core changed its mind")

// A message written without its deliveries is a message nobody will ever send.
// This is the guarantee submit relies on for that.
func TestFailingWorkLeavesNothingBehind(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sources := postgres.NewSourceRepository(pool)
	notifs := postgres.NewNotificationRepository(pool)
	uow := postgres.NewUnitOfWork(pool)

	src := aSource(ulid("U1"))
	ctx := context.Background()

	err := uow.Atomically(ctx, func(ctx context.Context) error {
		if err := sources.Create(ctx, src); err != nil {
			return err
		}
		if err := notifs.Create(ctx, aMessage(t, ulid("U2"), src.ID, "")); err != nil {
			return err
		}
		return errFromTheCore
	})
	if !errors.Is(err, errFromTheCore) {
		t.Fatalf("Atomically() = %v, want the core's own error unwrapped", err)
	}

	if _, err := sources.ReadByID(ctx, src.ID); err == nil {
		t.Error("the source survived a rolled-back transaction")
	}
	if _, err := notifs.ReadByID(ctx, shared.ID(ulid("U2"))); !errors.Is(
		err,
		notification.ErrNotFound,
	) {
		t.Error("the message survived a rolled-back transaction")
	}
}

func TestSuccessfulWorkIsVisibleAfterwards(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sources := postgres.NewSourceRepository(pool)
	notifs := postgres.NewNotificationRepository(pool)
	uow := postgres.NewUnitOfWork(pool)

	src := aSource(ulid("U3"))
	ctx := context.Background()

	err := uow.Atomically(ctx, func(ctx context.Context) error {
		if err := sources.Create(ctx, src); err != nil {
			return err
		}
		return notifs.Create(ctx, aMessage(t, ulid("U4"), src.ID, ""))
	})
	if err != nil {
		t.Fatalf("Atomically: %v", err)
	}

	if _, err := notifs.ReadByID(ctx, shared.ID(ulid("U4"))); err != nil {
		t.Errorf("the committed message is not there: %v", err)
	}
}

// A sentinel the caller matches on has to survive the transaction wrapper. The
// duplicate key is the one that matters: submit answers it as success.
func TestASentinelSurvivesTheTransaction(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sources := postgres.NewSourceRepository(pool)
	notifs := postgres.NewNotificationRepository(pool)
	uow := postgres.NewUnitOfWork(pool)

	src := aSource(ulid("U5"))
	ctx := context.Background()

	if err := sources.Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	if err := notifs.Create(ctx, aMessage(t, ulid("U6"), src.ID, "order-42")); err != nil {
		t.Fatalf("first message: %v", err)
	}

	err := uow.Atomically(ctx, func(ctx context.Context) error {
		return notifs.Create(ctx, aMessage(t, ulid("U7"), src.ID, "order-42"))
	})
	if !errors.Is(err, notification.ErrDuplicateKey) {
		t.Fatalf("Atomically() = %v, want ErrDuplicateKey through it", err)
	}
}

// Nesting joins rather than beginning a second transaction, so the inner work
// cannot wait on locks the outer one is holding.
func TestNestingJoinsTheRunningTransaction(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sources := postgres.NewSourceRepository(pool)
	uow := postgres.NewUnitOfWork(pool)

	src := aSource(ulid("U8"))
	ctx := context.Background()

	err := uow.Atomically(ctx, func(ctx context.Context) error {
		if err := sources.Create(ctx, src); err != nil {
			return err
		}
		// Same row, read from inside a nested block: only visible if this is
		// the same transaction rather than a second one.
		return uow.Atomically(ctx, func(ctx context.Context) error {
			_, err := sources.ReadByID(ctx, src.ID)
			return err
		})
	})
	if err != nil {
		t.Fatalf("Atomically: %v", err)
	}
}
