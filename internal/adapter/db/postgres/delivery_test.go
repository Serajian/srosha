//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
)

// fixture puts a source and a message in place, since every delivery hangs off
// one, and returns the repositories under test.
func fixture(t *testing.T, tag string) (
	*postgres.DeliveryRepository, *notification.Notification, shared.NowFunc,
) {
	t.Helper()

	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid(tag + "S"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	n := aMessage(t, ulid(tag+"N"), src.ID, "")
	if err := postgres.NewNotificationRepository(pool).Create(ctx, n); err != nil {
		t.Fatalf("Create message: %v", err)
	}

	now := func() time.Time { return time.Now().UTC() }
	return postgres.NewDeliveryRepository(pool, now), n, now
}

func recipients(addresses ...string) []shared.Recipient {
	out := make([]shared.Recipient, 0, len(addresses))
	for _, a := range addresses {
		out = append(out, shared.Recipient{Channel: shared.ChannelEmail, Address: a})
	}
	return out
}

func makeDeliveries(t *testing.T, n *notification.Notification, addresses ...string) []delivery.Delivery {
	t.Helper()

	i := 0
	ids := func() shared.ID {
		i++
		return shared.ID(ulid("D" + string(rune('0'+i))))
	}

	ds, err := delivery.NewSet(n.ID, recipients(addresses...), map[shared.Channel]string{
		shared.ChannelEmail: "default",
	}, ids, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatalf("delivery.New: %v", err)
	}
	return ds
}

func TestDeliveriesRoundTrip(t *testing.T) {
	repo, n, _ := fixture(t, "A")
	ctx := context.Background()

	want := makeDeliveries(t, n, "a@acme.com", "b@acme.com")
	if err := repo.CreateByList(ctx, want); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	got, err := repo.ListByNotificationID(ctx, n.ID)
	if err != nil {
		t.Fatalf("ListByNotificationID: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d deliveries, want 2", len(got))
	}
	if got[0].Status() != delivery.StatusPending || got[0].Attempts() != 0 {
		t.Errorf("a new delivery came back as %v/%d", got[0].Status(), got[0].Attempts())
	}
	if got[0].SenderName != "default" {
		t.Errorf("SenderName = %q", got[0].SenderName)
	}
}

// Recovery looks for rows that have been waiting too long. A fresh one must not
// be picked up, and a settled one must never be.
func TestListStaleFindsOnlyOldPendingRows(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("CS"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	n := aMessage(t, ulid("CN"), src.ID, "")
	if err := postgres.NewNotificationRepository(pool).Create(ctx, n); err != nil {
		t.Fatalf("Create message: %v", err)
	}

	// A clock two hours ahead makes everything written now look two hours old.
	ahead := func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	repo := postgres.NewDeliveryRepository(pool, ahead)

	ds := makeDeliveries(t, n, "old@acme.com")
	if err := repo.CreateByList(ctx, ds); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	stale, err := repo.ListStale(ctx, time.Hour, 10)
	if err != nil {
		t.Fatalf("ListStale: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("stale = %d, want the one waiting row", len(stale))
	}

	// Settle it, and it drops out.
	d := &stale[0]
	if err := d.MarkSent("p", 1, time.Now().UTC()); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := repo.Update(ctx, d); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stale, err = repo.ListStale(ctx, time.Hour, 10)
	if err != nil {
		t.Fatalf("ListStale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("a settled delivery is still listed as stale: %d", len(stale))
	}
}

// A fresh delivery is not recovery's business.
func TestListStaleIgnoresFreshRows(t *testing.T) {
	repo, n, _ := fixture(t, "E")
	ctx := context.Background()

	if err := repo.CreateByList(ctx, makeDeliveries(t, n, "new@acme.com")); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	stale, err := repo.ListStale(ctx, time.Hour, 10)
	if err != nil {
		t.Fatalf("ListStale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("a delivery written a moment ago is already stale: %d", len(stale))
	}
}

func TestPagingWalksEveryDeliveryExactlyOnce(t *testing.T) {
	repo, n, _ := fixture(t, "F")
	ctx := context.Background()

	if err := repo.CreateByList(ctx, makeDeliveries(t, n,
		"a@acme.com", "b@acme.com", "c@acme.com", "d@acme.com", "e@acme.com")); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	seen := map[shared.ID]int{}
	var after *shared.ID
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging did not end")
		}

		page, err := repo.PageByNotificationID(ctx, n.ID, shared.Cursor{After: after, Limit: 2})
		if err != nil {
			t.Fatalf("PageByNotificationID: %v", err)
		}
		for _, d := range page.Items {
			seen[d.ID]++
		}
		if !page.HasNext() {
			break
		}
		after = page.NextCursor
	}

	if len(seen) != 5 {
		t.Errorf("saw %d distinct deliveries, want 5", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("delivery %s came back %d times", id, n)
		}
	}
}

func TestMissingDeliveryIsNotFound(t *testing.T) {
	repo, _, _ := fixture(t, "G")

	_, err := repo.ReadByID(context.Background(), shared.ID(ulid("ZZ")))
	if !errors.Is(err, delivery.ErrNotFound) {
		t.Fatalf("error = %v, want delivery.ErrNotFound", err)
	}
}

// The race at-least-once actually produces: two workers each hold a pending
// copy, both send, and both write the result.
func TestTheSecondWorkerIsToldItLost(t *testing.T) {
	repo, n, now := fixture(t, "B")
	ctx := context.Background()

	ds := makeDeliveries(t, n, "a@acme.com")
	if err := repo.CreateByList(ctx, ds); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	first, err := repo.ReadByID(ctx, ds[0].ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	second, err := repo.ReadByID(ctx, ds[0].ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}

	if err := first.MarkSent("provider-1", 1, now()); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("first Update: %v", err)
	}

	// The second copy is still pending in memory, so the domain lets it move.
	if err := second.MarkFailed(delivery.FailurePermanent, "boom", 1, now()); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	err = repo.Update(ctx, second)
	if !errors.Is(err, delivery.ErrAlreadySettled) {
		t.Fatalf("second Update = %v, want ErrAlreadySettled", err)
	}

	stored, err := repo.ReadByID(ctx, ds[0].ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if stored.Status() != delivery.StatusSent {
		t.Errorf("stored status = %v, want the first writer's SENT", stored.Status())
	}
	if stored.ProviderMessageID() != "provider-1" {
		t.Errorf("ProviderMessageID = %q", stored.ProviderMessageID())
	}
}
