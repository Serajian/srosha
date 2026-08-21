package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func seed(t *testing.T) (*rig, shared.ID) {
	t.Helper()
	r := newRig(t, nil)
	got, err := r.submitter.Submit(context.Background(), cmd())
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	return r, got.ID
}

func TestGetReturnsTheMessageAndItsDeliveries(t *testing.T) {
	r, id := seed(t)

	got, err := r.querier.Get(context.Background(), "acme", id, shared.Cursor{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Notification.ID != id {
		t.Errorf("id = %q, want %q", got.Notification.ID, id)
	}
	if len(got.Deliveries.Items) != 2 {
		t.Errorf("deliveries = %d, want 2", len(got.Deliveries.Items))
	}
	if got.Deliveries.HasNext() {
		t.Error("HasNext() = true with everything on one page")
	}
}

// A zero cursor means the first page, never an empty one.
func TestGetTreatsAZeroCursorAsTheFirstPage(t *testing.T) {
	r, id := seed(t)

	got, err := r.querier.Get(context.Background(), "acme", id, shared.Cursor{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(got.Deliveries.Items) == 0 {
		t.Error("a zero cursor returned nothing")
	}
}

func TestGetPagesWithTheCursor(t *testing.T) {
	r, id := seed(t)

	first, err := r.querier.Get(context.Background(), "acme", id, shared.Cursor{Limit: 1})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(first.Deliveries.Items) != 1 || !first.Deliveries.HasNext() {
		t.Fatalf("first page = %d items, hasNext = %v",
			len(first.Deliveries.Items), first.Deliveries.HasNext())
	}

	second, err := r.querier.Get(context.Background(), "acme", id,
		shared.Cursor{Limit: 1, After: first.Deliveries.NextCursor})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(second.Deliveries.Items) != 1 {
		t.Fatalf("second page = %d items, want 1", len(second.Deliveries.Items))
	}
	if second.Deliveries.Items[0].ID == first.Deliveries.Items[0].ID {
		t.Error("the second page repeated the first")
	}
	if second.Deliveries.HasNext() {
		t.Error("HasNext() = true on the last page")
	}
}

// Someone else's message is not found, not forbidden. Saying "it exists but is
// not yours" would let a caller discover which ids exist.
func TestGetHidesAnotherSourcesMessage(t *testing.T) {
	r, id := seed(t)

	_, err := r.querier.Get(context.Background(), "somebody-else", id, shared.Cursor{})

	if !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if errs.IsType(err, errs.ErrForbidden) {
		t.Error("reported as forbidden, which admits the message exists")
	}
}

func TestGetOnAnUnknownID(t *testing.T) {
	r, _ := seed(t)

	_, err := r.querier.Get(context.Background(), "acme",
		shared.ID("01J8XQ2M4E7N9V3B5C6D7F8ZZZ"), shared.Cursor{})

	if !errors.Is(err, notification.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
