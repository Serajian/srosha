//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
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

// Newest first, and the cursor walks backwards through it -- a ULID orders by
// time, so id DESC is time DESC.
func TestPageBySourceWalksBackwardsThroughTime(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("PS"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	repo := postgres.NewNotificationRepository(pool)

	var ids []shared.ID
	for i := range 5 {
		n := aMessage(t, ulid(fmt.Sprintf("P%02d", i)), src.ID, "")
		if err := repo.Create(ctx, n); err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids = append(ids, n.ID)
	}

	first, err := repo.PageBySource(ctx, src.ID, time.Time{}, shared.Cursor{Limit: 2})
	if err != nil {
		t.Fatalf("PageBySource: %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %d items, next %v", len(first.Items), first.NextCursor)
	}
	if first.Items[0].ID != ids[4] || first.Items[1].ID != ids[3] {
		t.Errorf("first page = %s, %s, want the two newest", first.Items[0].ID, first.Items[1].ID)
	}

	second, err := repo.PageBySource(ctx, src.ID, time.Time{},
		shared.Cursor{Limit: 2, After: first.NextCursor})
	if err != nil {
		t.Fatalf("PageBySource: %v", err)
	}
	if len(second.Items) != 2 || second.Items[0].ID != ids[2] {
		t.Errorf("second page = %+v, want it to carry on", second.Items)
	}
}

// The lower bound is inclusive: a message created exactly at it is in. There is
// no upper bound -- every window reaches back from now, so there is nothing to
// bound the other end with.
func TestPageBySourceHonorsTheLowerBound(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("PW"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	repo := postgres.NewNotificationRepository(pool)

	old := aMessage(t, ulid("PW1"), src.ID, "")
	recent := aMessage(t, ulid("PW2"), src.ID, "")
	if err := repo.Create(ctx, old); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, recent); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The boundary sits between them: old is before it, recent is at or after.
	cut := recent.CreatedAt

	from, err := repo.PageBySource(ctx, src.ID, cut, shared.Cursor{})
	if err != nil {
		t.Fatalf("PageBySource: %v", err)
	}
	if len(from.Items) != 1 || from.Items[0].ID != recent.ID {
		t.Errorf("from the cut = %+v, want only the recent one", from.Items)
	}

	// A bound before both takes both, which is what the widest window becomes.
	all, err := repo.PageBySource(ctx, src.ID, old.CreatedAt.Add(-time.Hour), shared.Cursor{})
	if err != nil {
		t.Fatalf("PageBySource: %v", err)
	}
	if len(all.Items) != 2 {
		t.Errorf("before both = %d items, want 2", len(all.Items))
	}
}

func TestPageBySourceShowsOnlyOneSourcesMessages(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	mine := aSource(ulid("PM"))
	theirs := aSource(ulid("PT"))
	sources := postgres.NewSourceRepository(pool)
	for _, s := range []*source.Source{mine, theirs} {
		if err := sources.Create(ctx, s); err != nil {
			t.Fatalf("Create source: %v", err)
		}
	}

	repo := postgres.NewNotificationRepository(pool)
	if err := repo.Create(ctx, aMessage(t, ulid("PM1"), mine.ID, "")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, aMessage(t, ulid("PT1"), theirs.ID, "")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.PageBySource(ctx, mine.ID, time.Time{}, shared.Cursor{})
	if err != nil {
		t.Fatalf("PageBySource: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].SourceID != mine.ID {
		t.Errorf("listed %+v", got.Items)
	}
}

// The deliveries go with the message, by the foreign key. That is the whole
// reason there is one statement here and not two to keep in step.
func TestDeletingAMessageTakesItsDeliveries(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("DR"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	repo := postgres.NewNotificationRepository(pool)
	n := aMessage(t, ulid("DR1"), src.ID, "")
	if err := repo.Create(ctx, n); err != nil {
		t.Fatalf("Create: %v", err)
	}

	deliveries := postgres.NewDeliveryRepository(pool, func() time.Time { return time.Now().UTC() })
	if err := deliveries.CreateByList(ctx, makeDeliveries(t, n, "a@acme.com", "b@acme.com")); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	// Everything written a moment ago is older than "a moment from now".
	got, err := repo.DeleteOlderThan(ctx, time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if got != 1 {
		t.Fatalf("deleted %d messages, want 1", got)
	}

	left, err := deliveries.ListByNotificationID(ctx, n.ID)
	if err != nil {
		t.Fatalf("ListByNotificationID: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d deliveries outlived their message", len(left))
	}
}

// One batch, not all of them: an unbounded DELETE over a table collecting for a
// year is a single transaction holding locks on all of it.
func TestDeleteOlderThanTakesOneBatch(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("DB"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	repo := postgres.NewNotificationRepository(pool)

	for i := range 5 {
		if err := repo.Create(ctx, aMessage(t, ulid(fmt.Sprintf("B%02d", i)), src.ID, "")); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	cutoff := time.Now().UTC().Add(time.Hour)

	first, err := repo.DeleteOlderThan(ctx, cutoff, 2)
	if err != nil || first != 2 {
		t.Fatalf("DeleteOlderThan = %d, %v; want one batch of 2", first, err)
	}

	// And the run keeps going until a batch comes back short.
	var total int
	for {
		n, err := repo.DeleteOlderThan(ctx, cutoff, 2)
		if err != nil {
			t.Fatalf("DeleteOlderThan: %v", err)
		}
		total += n
		if n < 2 {
			break
		}
	}
	if first+total != 5 {
		t.Errorf("deleted %d in total, want 5", first+total)
	}
}

// The whole point of this statement: it never selects title or body, and a
// message whose deliveries were never written still shows up, because the
// join is LEFT and count(d.id) rather than count(*).
func TestListForOperatorLeavesOutContentAndKeepsUndelivered(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("LO"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	repo := postgres.NewNotificationRepository(pool)

	// One message with two deliveries, one of them failed.
	sent := aMessage(t, ulid("LO1"), src.ID, "")
	if err := repo.Create(ctx, sent); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deliveries := postgres.NewDeliveryRepository(pool, func() time.Time { return time.Now().UTC() })
	ds := makeDeliveries(t, sent, "a@acme.com", "b@acme.com")
	if err := ds[0].MarkFailed(
		delivery.FailurePermanent, "rejected", 1, time.Now().UTC().Truncate(time.Microsecond),
	); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := deliveries.CreateByList(ctx, ds); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}
	if err := deliveries.Update(ctx, &ds[0]); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A second message whose deliveries were never written -- the row an
	// operator debugging a failure is looking for, and the one an inner join
	// would have hidden.
	orphan := aMessage(t, ulid("LO2"), src.ID, "")
	if err := repo.Create(ctx, orphan); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ListForOperator(ctx, src.ID, 10)
	if err != nil {
		t.Fatalf("ListForOperator: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	// Newest first.
	if got[0].ID != orphan.ID || got[1].ID != sent.ID {
		t.Fatalf("order = %s, %s, want the orphan first", got[0].ID, got[1].ID)
	}

	withDeliveries := got[1]
	if withDeliveries.Total != 2 {
		t.Errorf("total = %d, want 2", withDeliveries.Total)
	}
	if withDeliveries.Failed != 1 {
		t.Errorf("failed = %d, want 1", withDeliveries.Failed)
	}
	if len(withDeliveries.Channels) != 1 || withDeliveries.Channels[0] != "email" {
		t.Errorf("channels = %v, want [email]", withDeliveries.Channels)
	}

	noDeliveries := got[0]
	if noDeliveries.Total != 0 || noDeliveries.Failed != 0 {
		t.Errorf("orphan totals = %d/%d, want 0/0", noDeliveries.Total, noDeliveries.Failed)
	}
	if len(noDeliveries.Channels) != 0 {
		t.Errorf("orphan channels = %v, want none", noDeliveries.Channels)
	}
}

// A message inside the window is not the sweep's business.
func TestDeleteOlderThanLeavesTheRecentAlone(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("DK"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	repo := postgres.NewNotificationRepository(pool)
	if err := repo.Create(ctx, aMessage(t, ulid("DK1"), src.ID, "")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.DeleteOlderThan(ctx, time.Now().UTC().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if got != 0 {
		t.Errorf("deleted %d messages that were inside the window", got)
	}
}
