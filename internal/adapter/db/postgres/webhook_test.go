//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/webhook"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// aWebhook builds one through New, so nothing is stored that the domain would
// not have produced -- the url check included.
func aWebhook(t *testing.T, id, sourceID, url string) *webhook.Webhook {
	t.Helper()

	w, err := webhook.New(
		shared.ID(id), sourceID, webhook.Registration{CallbackURL: url}, webhook.Strict,
		time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		t.Fatalf("webhook.New: %v", err)
	}
	return w
}

// registered leaves one callback in the database and hands back both it and its
// repository, which is the opening of nearly every test here.
func registered(t *testing.T, tag string) (*postgres.WebhookRepository, *webhook.Webhook) {
	t.Helper()

	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, tag+"0")
	repo := postgres.NewWebhookRepository(pool)
	w := aWebhook(t, ulid(tag+"1"), sourceID, "https://acme.com/hooks/srosha")

	if err := repo.Create(context.Background(), w); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return repo, w
}

func TestWebhookRoundTrips(t *testing.T) {
	repo, want := registered(t, "W1")
	ctx := context.Background()

	got, err := repo.ReadBySourceID(ctx, want.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if got == nil {
		t.Fatal("ReadBySourceID() = nil, want the callback we just registered")
	}

	if got.ID != want.ID || got.CallbackURL != want.CallbackURL {
		t.Errorf("identity did not survive: %+v", got)
	}
	if !got.IsActive() {
		t.Error("a new callback came back switched off")
	}
	if got.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want 0", got.ConsecutiveFailures())
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// One source has one callback. The service reads before it writes, so this is
// the case where two registrations arrived at once.
func TestOneSourceCannotHaveTwoCallbacks(t *testing.T) {
	repo, first := registered(t, "W2")

	second := aWebhook(t, ulid("W23"), first.SourceID, "https://acme.com/other")

	err := repo.Create(context.Background(), second)
	if !errs.IsType(err, errs.ErrDuplicateEntry) {
		t.Fatalf("Create() = %v, want a duplicate", err)
	}
}

// Register decides between creating and redirecting from this answer, so a
// source that has registered nothing must not look like a failure.
func TestUnregisteredSourceIsNilNotAnError(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	got, err := postgres.NewWebhookRepository(pool).
		ReadBySourceID(context.Background(), ulid("ZZ"))
	if err != nil {
		t.Fatalf("ReadBySourceID() error = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("ReadBySourceID() = %+v, want nil", got)
	}
}

// A new address has not failed at anything yet. Leaving the old run and the old
// flag in place would mean a source that fixed a broken endpoint saw nothing
// change.
func TestRedirectGivesTheNewAddressACleanStart(t *testing.T) {
	repo, w := registered(t, "W3")
	ctx := context.Background()

	for range 3 {
		if _, err := repo.RecordFailure(ctx, w); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
	}
	w.Deactivate(time.Now().UTC())
	if err := repo.Deactivate(ctx, w); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	if err := w.Redirect(
		webhook.Registration{CallbackURL: "https://acme.com/hooks/v2"},
		webhook.Strict, time.Now().UTC().Truncate(time.Microsecond),
	); err != nil {
		t.Fatalf("Redirect: %v", err)
	}
	if err := repo.Redirect(ctx, w); err != nil {
		t.Fatalf("repo.Redirect: %v", err)
	}

	got, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if got.CallbackURL != "https://acme.com/hooks/v2" {
		t.Errorf("CallbackURL = %q, want the new address", got.CallbackURL)
	}
	if !got.IsActive() {
		t.Error("the new address is still switched off")
	}
	if got.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want the run cleared", got.ConsecutiveFailures())
	}
}

// This is why the count lives in SQL. Two callbacks for one source settle at
// once, each holding its own copy of the entity -- both copies say zero
// failures, so a count carried in from memory would write 1 twice and the
// endpoint would never reach the limit that switches it off.
func TestFailuresAreCountedInStorageNotInMemory(t *testing.T) {
	repo, w := registered(t, "W4")
	ctx := context.Background()

	one, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	two, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if one.ConsecutiveFailures() != 0 || two.ConsecutiveFailures() != 0 {
		t.Fatalf("both copies should start at zero: %d / %d",
			one.ConsecutiveFailures(), two.ConsecutiveFailures())
	}

	first, err := repo.RecordFailure(ctx, one)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	second, err := repo.RecordFailure(ctx, two)
	if err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	if first != 1 || second != 2 {
		t.Errorf("counts = %d and %d, want 1 and 2 -- an increment was lost", first, second)
	}
}

// An endpoint that fails now and then must never be switched off.
func TestSuccessClearsTheRun(t *testing.T) {
	repo, w := registered(t, "W5")
	ctx := context.Background()

	for range 2 {
		if _, err := repo.RecordFailure(ctx, w); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
	}

	w.RecordSuccess(time.Now().UTC())
	if err := repo.RecordSuccess(ctx, w); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	got, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if got.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want 0", got.ConsecutiveFailures())
	}
}

// Two callbacks crossing the failure limit together both ask for the same
// change. The second one changed nothing, and nothing is wrong.
func TestSwitchingToTheStateItIsAlreadyInIsNotAFailure(t *testing.T) {
	repo, w := registered(t, "W6")
	ctx := context.Background()

	w.Deactivate(time.Now().UTC())
	if err := repo.Deactivate(ctx, w); err != nil {
		t.Fatalf("first Deactivate: %v", err)
	}
	if err := repo.Deactivate(ctx, w); err != nil {
		t.Fatalf("second Deactivate() = %v, want nil", err)
	}

	got, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if got.IsActive() {
		t.Error("still active")
	}
}

// Switching it back on must clear the failure run, or the callback that was
// just fixed is switched off again by the first hiccup.
func TestActivateClearsTheFailureRun(t *testing.T) {
	repo, w := registered(t, "W8")
	ctx := context.Background()

	for range 3 {
		if _, err := repo.RecordFailure(ctx, w); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
	}
	w.Deactivate(time.Now().UTC())
	if err := repo.Deactivate(ctx, w); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	w.Activate(time.Now().UTC())
	if err := repo.Activate(ctx, w); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	got, err := repo.ReadBySourceID(ctx, w.SourceID)
	if err != nil {
		t.Fatalf("ReadBySourceID: %v", err)
	}
	if !got.IsActive() {
		t.Error("IsActive() = false")
	}
	if got.ConsecutiveFailures() != 0 {
		t.Errorf("ConsecutiveFailures() = %d, want the run cleared", got.ConsecutiveFailures())
	}
}

// Every write here is called with an entity that was just read, so nothing
// matching means the row is gone -- the source was deleted mid-flight.
func TestWritingToACallbackThatIsGone(t *testing.T) {
	repo, w := registered(t, "W7")
	pool := connect(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "DELETE FROM webhooks WHERE id = $1", w.ID.String()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{"redirect", func() error { return repo.Redirect(ctx, w) }},
		{"success", func() error { return repo.RecordSuccess(ctx, w) }},
		{"failure", func() error { _, err := repo.RecordFailure(ctx, w); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !errors.Is(err, webhook.ErrNotFound) {
				t.Fatalf("error = %v, want webhook.ErrNotFound", err)
			}
			if !errs.IsType(err, errs.ErrNotFound) {
				t.Errorf("type = %v, want not found", errs.TypeOf(err))
			}
		})
	}
}
