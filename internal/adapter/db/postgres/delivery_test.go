//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
func TestClaimStaleTakesOnlyOldPendingRows(t *testing.T) {
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

	stale, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
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

	stale, err = repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("a settled delivery is still listed as stale: %d", len(stale))
	}
}

// A fresh delivery is not recovery's business.
func TestClaimStaleIgnoresFreshRows(t *testing.T) {
	repo, n, _ := fixture(t, "E")
	ctx := context.Background()

	if err := repo.CreateByList(ctx, makeDeliveries(t, n, "new@acme.com")); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}

	stale, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
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

// The whole point of the claim: one sweep takes the rows, the next finds none.
// Recovery sends directly, so two sweeps holding the same delivery is somebody
// getting the message twice.
func TestASecondSweepFindsNothingToTake(t *testing.T) {
	repo, n, _ := staleFixture(t, "CQ")
	ctx := context.Background()

	first, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("the first sweep took nothing")
	}

	second, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("a second sweep took %d rows the first is holding", len(second))
	}
	_ = n
}

// A dispatcher that dies holding a row would otherwise strand it for ever.
func TestAClaimExpires(t *testing.T) {
	repo, _, _ := staleFixture(t, "CR")
	ctx := context.Background()

	if _, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10); err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}

	// A lease of nothing: every claim is already expired.
	again, err := repo.ClaimStale(ctx, time.Hour, 0, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(again) == 0 {
		t.Error("an expired claim was still holding the row")
	}
}

// Without the release, a transient failure would hold the row for the whole
// lease, and the lease would quietly become the retry interval.
func TestAReleasedRowCanBeTakenAgain(t *testing.T) {
	repo, _, _ := staleFixture(t, "CT")
	ctx := context.Background()

	got, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("took nothing")
	}

	if err := repo.Release(ctx, &got[0]); err != nil {
		t.Fatalf("Release: %v", err)
	}

	again, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if len(again) != 1 || again[0].ID != got[0].ID {
		t.Errorf("took %d rows after a release, want the released one back", len(again))
	}
}

// Age is the retry counter. A claim that moved updated_at would mean the row
// never reaches GIVE_UP.
func TestClaimingDoesNotAgeTheRow(t *testing.T) {
	repo, _, _ := staleFixture(t, "CU")
	ctx := context.Background()

	got, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	before := got[0].UpdatedAt()

	if err := repo.Release(ctx, &got[0]); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := repo.ClaimStale(ctx, time.Hour, time.Hour, 10)
	if err != nil {
		t.Fatalf("ClaimStale: %v", err)
	}
	if !again[0].UpdatedAt().Equal(before) {
		t.Errorf("updated_at moved from %v to %v", before, again[0].UpdatedAt())
	}
}

// staleFixture writes one delivery that already looks an hour old.
func staleFixture(t *testing.T, suffix string) (*postgres.DeliveryRepository, *notification.Notification, string) {
	t.Helper()

	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid(suffix + "S"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}
	n := aMessage(t, ulid(suffix+"N"), src.ID, "")
	if err := postgres.NewNotificationRepository(pool).Create(ctx, n); err != nil {
		t.Fatalf("Create message: %v", err)
	}

	// A clock two hours ahead makes everything written now look two hours old.
	ahead := func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	repo := postgres.NewDeliveryRepository(pool, ahead)

	if err := repo.CreateByList(ctx, makeDeliveries(t, n, "old@acme.com")); err != nil {
		t.Fatalf("CreateByList: %v", err)
	}
	return repo, n, src.ID
}

// SKIP LOCKED is for the instant two sweeps arrive together, which is the case
// a sequential test cannot reach. Every row must go to exactly one of them.
func TestTwoSweepsAtOnceSplitTheRows(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	src := aSource(ulid("CVS"))
	if err := postgres.NewSourceRepository(pool).Create(ctx, src); err != nil {
		t.Fatalf("Create source: %v", err)
	}

	ahead := func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	repo := postgres.NewDeliveryRepository(pool, ahead)

	// One delivery per message, each with an id of its own: makeDeliveries hands
	// out a fixed one, which twenty messages would collide on.
	const messages = 20
	for i := range messages {
		n := aMessage(t, ulid(fmt.Sprintf("N%02d", i)), src.ID, "")
		if err := postgres.NewNotificationRepository(pool).Create(ctx, n); err != nil {
			t.Fatalf("Create message: %v", err)
		}

		ds, err := delivery.NewSet(n.ID, recipients("old@acme.com"),
			map[shared.Channel]string{shared.ChannelEmail: "default"},
			func() shared.ID { return shared.ID(ulid(fmt.Sprintf("D%02d", i))) },
			time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			t.Fatalf("delivery.NewSet: %v", err)
		}
		if err := repo.CreateByList(ctx, ds); err != nil {
			t.Fatalf("CreateByList: %v", err)
		}
	}

	const sweepers = 4
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[shared.ID]int{}
		took int
	)
	for range sweepers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			got, err := repo.ClaimStale(ctx, time.Hour, time.Hour, messages)
			if err != nil {
				t.Errorf("ClaimStale: %v", err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for i := range got {
				seen[got[i].ID]++
				took++
			}
		}()
	}
	wg.Wait()

	if took != messages {
		t.Errorf("%d rows were taken in total, want %d", took, messages)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("delivery %s was taken %d times", id, n)
		}
	}
}
