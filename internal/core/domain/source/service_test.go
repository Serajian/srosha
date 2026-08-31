package source_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// waiting is a source as it is the moment somebody registers one: switched off,
// never approved.
func waiting() *source.Source {
	return &source.Source{
		ID:               "01K0SRC0000000000000000000",
		OwnerUserID:      shared.ID("01K0ACCT0000000000000000AB"),
		Name:             "acme-billing",
		MaxPriority:      shared.PriorityNormal,
		IsActive:         false,
		DefaultAddresses: map[shared.Channel]string{},
		CreatedAt:        time.Now().UTC(),
	}
}

type oneSource struct{ src *source.Source }

func (r oneSource) Create(context.Context, *source.Source) error { return nil }

func (r oneSource) ReadByID(_ context.Context, id string) (*source.Source, error) {
	if r.src == nil || r.src.ID != id {
		return nil, errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return r.src, nil
}

func (r oneSource) ListByOwner(context.Context, shared.ID) ([]source.Source, error) {
	return nil, nil
}

func (r oneSource) UpdateSettings(context.Context, *source.Source) error { return nil }

func (r oneSource) UpdateReview(context.Context, *source.Source) error { return nil }

func (r oneSource) ListForReview(context.Context) ([]source.Source, error) { return nil, nil }

func (r oneSource) ListAll(context.Context) ([]source.Source, error) { return nil, nil }

type allowAll struct{}

func (allowAll) Allow(context.Context, string) (bool, error) { return true, nil }

// A customer configures a source before it is approved, not after. Otherwise
// registering one ends in waiting, and the operator approves a shell with
// nothing set up in it.
func TestAnUnapprovedSourceCanStillBeConfigured(t *testing.T) {
	src := waiting()
	svc := source.NewService(oneSource{src: src}, allowAll{})

	got, err := svc.Manage(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Manage: %v", err)
	}
	if got.ID != src.ID {
		t.Errorf("id = %q", got.ID)
	}
}

// And it still may not send.
func TestAnUnapprovedSourceMayNotSend(t *testing.T) {
	src := waiting()
	svc := source.NewService(oneSource{src: src}, allowAll{})

	if _, err := svc.Admit(context.Background(), src.ID); err == nil {
		t.Fatal("Admit: an unapproved source was allowed to send")
	}
}

// Manage is not a way around the active check, only a different question. A
// source that is not there is still not there.
func TestManagingASourceThatIsNotThere(t *testing.T) {
	svc := source.NewService(oneSource{}, allowAll{})

	if _, err := svc.Manage(context.Background(), "01K0SRC0000000000000000000"); err == nil {
		t.Fatal("Manage: want an error for a source that does not exist")
	}
}

// The three fields a customer owns change. The ceiling does not -- and this
// asserts on the entity rather than trusting the statement, because a use case
// that reads and re-sends the whole row is exactly how a ceiling gets carried
// somewhere it should not.
func TestChangingSettingsLeavesTheCeilingAlone(t *testing.T) {
	src := waiting()
	src.MaxPriority = shared.PriorityCritical
	src.AllowCustomAddress = true

	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Reconfigure("acme-alerts", "pages the on-call", nil, at); err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	got := src

	if got.Name != "acme-alerts" || got.Description != "pages the on-call" {
		t.Errorf("name = %q, description = %q", got.Name, got.Description)
	}
	if got.MaxPriority != shared.PriorityCritical {
		t.Errorf("the ceiling moved to %v", got.MaxPriority)
	}
	if !got.AllowCustomAddress {
		t.Error("allow_custom_address was cleared by an edit")
	}
	if got.IsActive {
		t.Error("an edit switched the source on")
	}
	if !got.UpdatedAt.Equal(at) {
		t.Errorf("updated_at = %v", got.UpdatedAt)
	}
}

// A default address that no longer exists is the ordinary reason to edit a
// source, so the map has to be replaceable rather than append-only.
func TestADefaultAddressCanBeReplaced(t *testing.T) {
	src := waiting()
	src.DefaultAddresses = map[shared.Channel]string{shared.ChannelEmail: "old@acme.test"}

	err := src.Reconfigure(
		src.Name, "",
		map[shared.Channel]string{shared.ChannelEmail: "new@acme.test"},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("Reconfigure: %v", err)
	}
	got := src
	if got.DefaultAddresses[shared.ChannelEmail] != "new@acme.test" {
		t.Errorf("address = %q", got.DefaultAddresses[shared.ChannelEmail])
	}
}

// An edit is all of it or none of it. A bad address must not leave the name
// already changed, because the customer would fix the address and never know
// the rename had gone through on its own.
func TestABadAddressLeavesTheWholeEditUndone(t *testing.T) {
	src := waiting()
	err := src.Reconfigure(
		"renamed", "",
		map[shared.Channel]string{shared.ChannelEmail: "not-an-address"},
		time.Now().UTC(),
	)
	if err == nil {
		t.Fatal("a bad address was accepted")
	}
	if src.Name != "acme-billing" {
		t.Errorf("the name changed anyway: %q", src.Name)
	}
}

// A refused source is not a new one. Without a third fact they are the same
// row, and an operator is handed the same decision every day.
func TestARefusedSourceIsNotWaiting(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	if !src.IsReviewed() {
		t.Error("a refused source is still in the queue")
	}
	if src.IsApproved() {
		t.Error("refusing approved it")
	}
	if src.IsActive {
		t.Error("refusing left it able to send")
	}
	if src.ReviewNote != "no working address" {
		t.Errorf("note = %q", src.ReviewNote)
	}
}

// A refusal with no reason is the silent failure the column exists to prevent.
func TestARefusalNeedsAReason(t *testing.T) {
	src := waiting()

	if err := src.Refuse("   ", time.Now().UTC()); err == nil {
		t.Fatal("a source was refused with no reason")
	}
	if src.IsReviewed() {
		t.Error("the refusal was refused and applied anyway")
	}
}

// Approving after a refusal clears the note: the state is the current
// decision. What was said before lives in the audit log.
func TestApprovingClearsAnEarlierRefusal(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	src.AllowCustomAddress = true // otherwise Approve refuses: nowhere to send
	if err := src.Approve(at.Add(time.Hour)); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if src.ReviewNote != "" {
		t.Errorf("the old refusal is still on it: %q", src.ReviewNote)
	}
	if !src.IsActive || !src.IsApproved() {
		t.Error("approving did not let it send")
	}
}

// Suspending a source that was approved keeps approved_at, so the queue can
// still tell "turned away" from "worked once and was switched off".
func TestSuspendingKeepsTheApproval(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	src.AllowCustomAddress = true // otherwise Approve refuses: nowhere to send
	if err := src.Approve(at); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := src.Suspend(at.Add(time.Hour)); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	if src.IsActive {
		t.Error("it can still send")
	}
	if !src.IsApproved() {
		t.Error("suspending forgot that it was ever approved")
	}
}

// Restoring a refused source is letting it out for the first time, and
// approved_at is what records that -- a source restored straight from a
// refusal must not read as active and never approved, which is a fifth state
// the spec's table does not have.
func TestRestoringARefusedSourceRecordsTheApproval(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	src.AllowCustomAddress = true // otherwise Restore refuses: nowhere to send
	if err := src.Restore(at.Add(time.Hour)); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if !src.IsApproved() {
		t.Error("restoring a refused source did not record an approval")
	}
	if !src.IsActive {
		t.Error("restoring did not let it send")
	}
}

// A source nobody has approved cannot be suspended, which is Refuse's guard
// read the other way round.
//
// Without it, suspending something still in the queue leaves is_active=f,
// approved_at=null, reviewed_at=set and review_note="" -- byte for byte a
// refusal with no reason, which is the exact state review_note exists to make
// impossible. The customer then reads "This source was not approved." and an
// empty sentence after it.
func TestAnUnapprovedSourceCannotBeSuspended(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	err := src.Suspend(at)
	if err == nil {
		t.Fatal("a source that was never approved was suspended")
	}
	if !errors.Is(err, source.ErrNotApproved) {
		t.Errorf("err = %v, want it to wrap source.ErrNotApproved", err)
	}

	if src.IsReviewed() {
		t.Error("the refused suspension still marked the source reviewed")
	}
	if src.ReviewedAt != nil || src.ApprovedAt != nil {
		t.Error("the refused suspension moved the source's timestamps")
	}
}

// Restore is the way BACK, so there has to be somewhere to come back from.
// Restoring a source nobody has decided about would approve it while the
// audit row said source.restore -- a first decision recorded under a verb
// that says it was the second.
func TestASourceNobodyHasDecidedAboutCannotBeRestored(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	err := src.Restore(at)
	if err == nil {
		t.Fatal("a source still in the queue was restored")
	}
	if !errors.Is(err, source.ErrNotReviewed) {
		t.Errorf("err = %v, want it to wrap source.ErrNotReviewed", err)
	}
	if src.IsActive || src.IsApproved() {
		t.Error("the refused restore let the source send anyway")
	}
}

// Refusing an already-approved source would leave approved_at set, which is
// indistinguishable from suspended. Refusing is a decision at the door; a
// source that is already through it is suspended instead.
func TestRefusingAnApprovedSourceIsRefused(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	src.AllowCustomAddress = true // otherwise Approve refuses: nowhere to send
	if err := src.Approve(at); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	err := src.Refuse("changed my mind", at.Add(time.Hour))
	if err == nil {
		t.Fatal("an approved source was refused")
	}
	if !errors.Is(err, source.ErrAlreadyApproved) {
		t.Errorf("error = %v, want ErrAlreadyApproved", err)
	}
	if !src.IsActive {
		t.Error("the failed refusal switched the source off anyway")
	}
}

// A source with nowhere to send cannot be approved: activating it would make
// it look like it works when every message it sends is going to fail.
func TestApprovingWithNowhereToSendIsRefused(t *testing.T) {
	src := waiting() // no default addresses, custom addresses not allowed
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	err := src.Approve(at)
	if !errors.Is(err, source.ErrNoReachableAddress) {
		t.Errorf("err = %v, want it to wrap source.ErrNoReachableAddress", err)
	}
	if src.IsActive || src.IsApproved() || src.IsReviewed() {
		t.Error("the refused approval changed the source anyway")
	}
}

// A default address on any one channel is enough: the source can send to it.
func TestApprovingWithADefaultAddressSucceeds(t *testing.T) {
	src := waiting()
	src.DefaultAddresses = map[shared.Channel]string{shared.ChannelEmail: "ops@acme.test"}
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Approve(at); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !src.IsActive {
		t.Error("a source with a default address was not let out")
	}
}

// No default is fine too, as long as a message may name where to go.
func TestApprovingWithCustomAddressAllowedSucceedsWithNoDefaults(t *testing.T) {
	src := waiting()
	src.AllowCustomAddress = true
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Approve(at); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !src.IsActive {
		t.Error("a source allowed a custom address was not let out")
	}
}

// Restore is guarded the same way as Approve: a source switched off has
// exactly as little to send to as one still in the queue.
func TestRestoringWithNowhereToSendIsRefused(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	err := src.Restore(at.Add(time.Hour))
	if !errors.Is(err, source.ErrNoReachableAddress) {
		t.Errorf("err = %v, want it to wrap source.ErrNoReachableAddress", err)
	}
	if src.IsActive {
		t.Error("the refused restore let the source send anyway")
	}
}
