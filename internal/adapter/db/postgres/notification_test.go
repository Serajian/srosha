//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
)

// aMessage builds one through New, so nothing is stored that the domain would
// not have produced.
func aMessage(t *testing.T, id, sourceID, key string) *notification.Notification {
	t.Helper()

	n, err := notification.New(
		shared.ID(id),
		notification.Origin{ID: sourceID, Name: "Acme", MaxPriority: shared.PriorityCritical},
		notification.Request{
			IdempotencyKey: key,
			Title:          "Payment received",
			Body:           "…",
			Priority:       shared.PriorityHigh,
			Metadata:       map[string]string{"order": "42"},
		},
		time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("notification.New: %v", err)
	}
	return n
}

func TestNotificationRoundTrips(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	src := aSource(ulid("N0"))
	if err := postgres.NewSourceRepository(pool).Create(context.Background(), src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	repo := postgres.NewNotificationRepository(pool)
	ctx := context.Background()
	want := aMessage(t, ulid("N1"), src.ID, "order-42")

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ReadByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}

	if got.Title != want.Title || got.Body != want.Body {
		t.Errorf("text did not survive: %q / %q", got.Title, got.Body)
	}
	if got.RequestedPriority != want.RequestedPriority ||
		got.EffectivePriority != want.EffectivePriority {
		t.Errorf("priorities = %v/%v, want %v/%v",
			got.RequestedPriority, got.EffectivePriority,
			want.RequestedPriority, want.EffectivePriority)
	}
	if got.Metadata()["order"] != "42" {
		t.Errorf("metadata = %v", got.Metadata())
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// The whole point of the ON CONFLICT: the second writer is told it lost, not
// handed a constraint violation.
func TestSecondWriteWithTheSameKeyIsADuplicateNotAFailure(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	src := aSource(ulid("N2"))
	if err := postgres.NewSourceRepository(pool).Create(context.Background(), src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	repo := postgres.NewNotificationRepository(pool)
	ctx := context.Background()

	if err := repo.Create(ctx, aMessage(t, ulid("N3"), src.ID, "order-42")); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	err := repo.Create(ctx, aMessage(t, ulid("N4"), src.ID, "order-42"))
	if !errors.Is(err, notification.ErrDuplicateKey) {
		t.Fatalf("second Create = %v, want ErrDuplicateKey", err)
	}

	// And the original is the one that survived.
	got, err := repo.ReadByIdempotencyKey(ctx, src.ID, "order-42")
	if err != nil {
		t.Fatalf("ReadByIdempotencyKey: %v", err)
	}
	if got == nil || got.ID != shared.ID(ulid("N3")) {
		t.Errorf("stored message = %v, want the first one", got)
	}
}

// An empty key must reach the column as NULL. Stored as "" it would be a value,
// and the partial unique index would let one keyless message block every other.
func TestMessagesWithNoKeyDoNotBlockEachOther(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	src := aSource(ulid("N5"))
	if err := postgres.NewSourceRepository(pool).Create(context.Background(), src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	repo := postgres.NewNotificationRepository(pool)
	ctx := context.Background()

	if err := repo.Create(ctx, aMessage(t, ulid("N6"), src.ID, "")); err != nil {
		t.Fatalf("first keyless message: %v", err)
	}
	if err := repo.Create(ctx, aMessage(t, ulid("N7"), src.ID, "")); err != nil {
		t.Fatalf("second keyless message: %v", err)
	}
}

// "Never seen" and "could not look" have to stay distinguishable, so an unused
// key is a nil entity and not an error.
func TestUnusedKeyIsNilNotAnError(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	got, err := postgres.NewNotificationRepository(pool).
		ReadByIdempotencyKey(context.Background(), ulid("ZZ"), "never-used")
	if err != nil {
		t.Fatalf("error = %v, want none", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestMissingNotificationIsNotFound(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	_, err := postgres.NewNotificationRepository(pool).
		ReadByID(context.Background(), shared.ID(ulid("ZZ")))
	if !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("error = %v, want notification.ErrNotFound", err)
	}
}
