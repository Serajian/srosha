package usecase_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

func retentionRig(t *testing.T, age time.Duration, batch int, at time.Time) (*usecase.Retention, *fakeNotifications) {
	t.Helper()

	rows := newFakeNotifications()
	return usecase.NewRetention(
		notification.NewService(rows, seqIDs(), fixedNow(at), testRetention),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		age, batch,
	), rows
}

// write puts a message in directly, aged by hand: what matters here is when it
// was created, and Submit would only ever say "now".
func write(t *testing.T, rows *fakeNotifications, id shared.ID, createdAt time.Time) {
	t.Helper()

	rows.put(notification.Restore(notification.Notification{
		ID: id, SourceID: "acme", SourceName: "Acme", Body: "b",
		RequestedPriority: shared.PriorityNormal,
		EffectivePriority: shared.PriorityNormal,
		CreatedAt:         createdAt,
	}, nil))
}

func TestPurgeDropsWhatIsOldEnough(t *testing.T) {
	at := now
	r, rows := retentionRig(t, 30*24*time.Hour, 10, at)

	write(t, rows, "01J8XQ2M4E7N9V3B5C6D7F8001", at.Add(-40*24*time.Hour))
	write(t, rows, "01J8XQ2M4E7N9V3B5C6D7F8002", at.Add(-31*24*time.Hour))
	write(t, rows, "01J8XQ2M4E7N9V3B5C6D7F8003", at.Add(-29*24*time.Hour))
	write(t, rows, "01J8XQ2M4E7N9V3B5C6D7F8004", at)

	if err := r.Purge(context.Background()); err != nil {
		t.Fatalf("Purge() error = %v", err)
	}
	if got := rows.count(); got != 2 {
		t.Errorf("%d messages left, want the two inside the window", got)
	}
}

// A table that fell behind once must be able to catch up, so a run keeps going
// until a batch comes back short.
func TestPurgeKeepsGoingUntilNothingIsLeft(t *testing.T) {
	at := now
	r, rows := retentionRig(t, time.Hour, 3, at)

	for i := range 10 {
		write(t, rows, shared.ID(fmt.Sprintf("01J8XQ2M4E7N9V3B5C6D7F9%03d", i)), at.Add(-2*time.Hour))
	}

	if err := r.Purge(context.Background()); err != nil {
		t.Fatalf("Purge() error = %v", err)
	}
	if got := rows.count(); got != 0 {
		t.Errorf("%d messages left after a purge that should have cleared them", got)
	}
}

// A shutdown stops it between batches rather than in the middle of one, and
// what was already deleted stays deleted.
func TestPurgeStopsWhenItIsToldTo(t *testing.T) {
	at := now
	r, rows := retentionRig(t, time.Hour, 3, at)

	for i := range 10 {
		write(t, rows, shared.ID(fmt.Sprintf("01J8XQ2M4E7N9V3B5C6D7FA%02d", i)), at.Add(-2*time.Hour))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Purge(ctx); err != nil {
		t.Errorf("Purge() on a canceled context = %v, want it to stop quietly", err)
	}
	if got := rows.count(); got != 10 {
		t.Errorf("%d messages left, want none deleted before the first batch", got)
	}
}

func TestPurgeWithNothingToDo(t *testing.T) {
	at := now
	r, rows := retentionRig(t, 30*24*time.Hour, 10, at)

	write(t, rows, "01J8XQ2M4E7N9V3B5C6D7F8005", at)

	if err := r.Purge(context.Background()); err != nil {
		t.Fatalf("Purge() error = %v", err)
	}
	if got := rows.count(); got != 1 {
		t.Errorf("%d messages left, want the recent one kept", got)
	}
}
