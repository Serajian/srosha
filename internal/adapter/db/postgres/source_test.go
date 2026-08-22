//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func aSource(id string) *source.Source {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &source.Source{
		ID:                 id,
		Name:               "Acme",
		MaxPriority:        shared.PriorityHigh,
		IsActive:           true,
		AllowCustomAddress: false,
		DefaultAddresses:   map[shared.Channel]string{shared.ChannelTelegram: "-100123"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func TestSourceRoundTrips(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	want := aSource(ulid("S1"))

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ReadByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}

	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.MaxPriority != want.MaxPriority {
		t.Errorf("MaxPriority = %v, want %v", got.MaxPriority, want.MaxPriority)
	}
	if !got.IsActive {
		t.Error("a new source came back switched off")
	}
	if got.DefaultAddresses[shared.ChannelTelegram] != "-100123" {
		t.Errorf("DefaultAddresses = %v", got.DefaultAddresses)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// The domain has to be able to say "source is not active" rather than "no such
// source", which it can only do if the row comes back at all.
func TestSuspendedSourceStillReadsBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S2"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Deactivate(ctx, s.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID on a suspended source: %v", err)
	}
	if got.IsActive {
		t.Error("still active after Deactivate")
	}
	if err := got.EnsureActive(); !errors.Is(err, source.ErrSourceInactive) {
		t.Errorf("EnsureActive() = %v, want ErrSourceInactive", err)
	}
}

func TestMissingSourceIsNotFound(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	_, err := postgres.NewSourceRepository(pool).ReadByID(context.Background(), ulid("ZZ"))
	if !errors.Is(err, source.ErrNotFound) {
		t.Fatalf("error = %v, want source.ErrNotFound", err)
	}
	if !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("error type is not ErrNotFound: %v", err)
	}
}

// Renaming a customer must not bring a suspended one back.
func TestUpdateLeavesTheActiveFlagAlone(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S3"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Deactivate(ctx, s.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	s.Name = "Acme Renamed"
	s.MaxPriority = shared.PriorityCritical
	if err := repo.Update(ctx, s, time.Now().UTC()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if got.Name != "Acme Renamed" || got.MaxPriority != shared.PriorityCritical {
		t.Errorf("update did not land: %+v", got)
	}
	if got.IsActive {
		t.Error("renaming a source switched it back on")
	}
}

func TestChangingToTheStateItIsAlreadyIn(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S4"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := time.Now().UTC()
	if err := repo.Deactivate(ctx, s.ID, now); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	if err := repo.Deactivate(ctx, s.ID, now); !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("second Deactivate = %v, want a not-found", err)
	}
}

func TestUpdatingASourceThatIsNotThere(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	err := postgres.NewSourceRepository(pool).Update(context.Background(), aSource(ulid("ZZ")), time.Now())
	if !errors.Is(err, source.ErrNotFound) {
		t.Fatalf("error = %v, want source.ErrNotFound", err)
	}
}
