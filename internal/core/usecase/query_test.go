package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
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

// --- List --------------------------------------------------------------------

// Without this, Get can only be asked with an id the caller kept -- and the
// callback that would have carried it is best effort and never retried.
func TestListAnswersWhatDidISend(t *testing.T) {
	r := newRig(t, nil)

	for range 3 {
		if _, err := r.submitter.Submit(context.Background(), cmd()); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}

	got, err := r.querier.List(context.Background(), "acme", usecase.ListQuery{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("listed %d messages, want 3", len(got.Items))
	}
}

// Newest first, which is the opposite of every other listing here: a source
// asking this wants what it just sent.
func TestListPutsTheNewestFirst(t *testing.T) {
	r := newRig(t, nil)

	var ids []shared.ID
	for range 3 {
		res, err := r.submitter.Submit(context.Background(), cmd())
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		ids = append(ids, res.ID)
	}

	got, err := r.querier.List(context.Background(), "acme", usecase.ListQuery{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for i, want := range []shared.ID{ids[2], ids[1], ids[0]} {
		if got.Items[i].ID != want {
			t.Errorf("position %d = %s, want %s", i, got.Items[i].ID, want)
		}
	}
}

func TestListWalksBackwardsAPageAtATime(t *testing.T) {
	r := newRig(t, nil)

	for range 5 {
		if _, err := r.submitter.Submit(context.Background(), cmd()); err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}

	first, err := r.querier.List(context.Background(), "acme",
		usecase.ListQuery{Cursor: shared.Cursor{Limit: 2}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("first page = %d items, next %v", len(first.Items), first.NextCursor)
	}

	second, err := r.querier.List(context.Background(), "acme",
		usecase.ListQuery{Cursor: shared.Cursor{Limit: 2, After: first.NextCursor}})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(second.Items) != 2 {
		t.Fatalf("second page = %d items, want 2", len(second.Items))
	}
	if second.Items[0].ID >= first.Items[1].ID {
		t.Errorf("the second page did not carry on from the first")
	}
}

// One source's messages are not another's.
func TestListShowsOnlyTheCallersMessages(t *testing.T) {
	r := newRig(t, nil)

	if _, err := r.submitter.Submit(context.Background(), cmd()); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	got, err := r.querier.List(context.Background(), "somebody-else", usecase.ListQuery{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got.Items) != 0 {
		t.Errorf("listed %d of another source's messages", len(got.Items))
	}
}

// A window reaching past what is kept would come back short and look complete.
// The caller could not tell "you sent nothing then" from "we deleted it", so it
// is refused -- and the refusal says how far back they can go.
func TestAWindowLongerThanMessagesAreKept(t *testing.T) {
	r := newRig(t, nil)

	_, err := r.querier.List(context.Background(), "acme", usecase.ListQuery{
		Window: notification.WindowLastMonth,
	})
	if !errs.IsType(err, errs.ErrInvalidInput) {
		t.Fatalf("List() = %v, want invalid input", err)
	}
	if !errors.Is(err, notification.ErrWindowTooLong) {
		t.Errorf("List() = %v, want ErrWindowTooLong", err)
	}
	if !strings.Contains(err.Error(), "7 days") {
		t.Errorf("List() = %q, want the real limit in the message", err)
	}
}
