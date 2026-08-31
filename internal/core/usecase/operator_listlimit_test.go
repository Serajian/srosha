package usecase_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

// A listing that hits its cap says so -- this file is the whole reason
// truncate exists, and it earns tests of its own rather than one incidental
// assertion folded into files about something else.

// TestQueueAndAllSourcesSayWhenTheyAreTruncated seeds more unreviewed sources
// than the configured limit and checks both reads: the count is capped, and
// each says so.
func TestQueueAndAllSourcesSayWhenTheyAreTruncated(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 2)
	ctx := context.Background()

	// newOperatorRigWithLimit already seeds one unreviewed source. Three more
	// puts the pool at four, above the limit of two.
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		id := fmt.Sprintf("EXTRA-SRC-%d", i)
		s, err := source.New(id, rig.customer.ID, id, nil, at)
		if err != nil {
			t.Fatalf("source.New: %v", err)
		}
		rig.repo.byID[id] = s
	}

	queue, truncated, err := rig.ops.Queue(ctx, rig.admin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(queue) != 2 {
		t.Errorf("Queue returned %d rows, want the cap of 2", len(queue))
	}
	if !truncated {
		t.Error("Queue did not say it was truncated")
	}

	all, truncated, err := rig.ops.AllSources(ctx, rig.admin)
	if err != nil {
		t.Fatalf("AllSources: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("AllSources returned %d rows, want the cap of 2", len(all))
	}
	if !truncated {
		t.Error("AllSources did not say it was truncated")
	}
}

// TestQueueDoesNotSayTruncatedWhenEverythingFits is the other half: a cap
// that always claimed there was more would be exactly as dishonest as one
// that never did.
func TestQueueDoesNotSayTruncatedWhenEverythingFits(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 50)
	ctx := context.Background()

	queue, truncated, err := rig.ops.Queue(ctx, rig.admin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("got %d rows, want the one source newOperatorRig seeds", len(queue))
	}
	if truncated {
		t.Error("Queue said it was truncated when everything fit")
	}
}

// TestMessagesSaysWhenItIsTruncated seeds a second message on the same
// source, above a limit of one.
func TestMessagesSaysWhenItIsTruncated(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 1)
	ctx := context.Background()

	at := time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC)
	msg, err := notification.New(
		shared.ID("01J8XQ2M4E7N9V3B5C6D7F8MS2"),
		notification.Origin{
			ID:          rig.sourceID,
			Name:        "acme-billing",
			MaxPriority: shared.PriorityHigh,
		},
		notification.Request{Body: "second message", Priority: shared.PriorityNormal},
		at,
	)
	if err != nil {
		t.Fatalf("notification.New: %v", err)
	}
	if err := rig.notifs.Create(ctx, msg); err != nil {
		t.Fatalf("seeding a second message: %v", err)
	}

	got, truncated, err := rig.ops.Messages(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Messages returned %d rows, want the cap of 1", len(got))
	}
	if !truncated {
		t.Error("Messages did not say it was truncated")
	}
}

// TestPeopleSaysWhenItIsTruncated seeds enough accounts to cross a small
// limit. newOperatorRig already seeds three (customer, admin, superAdmin).
func TestPeopleSaysWhenItIsTruncated(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 2)
	ctx := context.Background()

	got, truncated, err := rig.ops.People(ctx, rig.superAdmin)
	if err != nil {
		t.Fatalf("People: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("People returned %d rows, want the cap of 2", len(got))
	}
	if !truncated {
		t.Error("People did not say it was truncated")
	}
}

// TestAuditSaysWhenItIsTruncated seeds rows straight into the log, standing
// in for acts a real gate would have recorded on other listeners.
func TestAuditSaysWhenItIsTruncated(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 1)
	ctx := context.Background()

	rig.log.entries = append(rig.log.entries,
		usecase.AuditEntry{
			ID: shared.ID("01J8XQ2M4E7N9V3B5C6D7F8AU1"), At: time.Now(),
			ActorID: rig.admin.ID, ActorEmail: rig.admin.Email,
			Verb: usecase.ActSourceApprove, TargetType: "source", TargetID: rig.sourceID,
		},
		usecase.AuditEntry{
			ID: shared.ID("01J8XQ2M4E7N9V3B5C6D7F8AU2"), At: time.Now(),
			ActorID: rig.admin.ID, ActorEmail: rig.admin.Email,
			Verb: usecase.ActSourceSuspend, TargetType: "source", TargetID: rig.sourceID,
		},
	)

	got, truncated, err := rig.ops.Audit(ctx, rig.superAdmin)
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Audit returned %d rows, want the cap of 1", len(got))
	}
	if !truncated {
		t.Error("Audit did not say it was truncated")
	}
}

// TestSourceHistorySaysWhenItIsTruncated is the same shape, for the one
// source's own history rather than the whole feed.
func TestSourceHistorySaysWhenItIsTruncated(t *testing.T) {
	rig := newOperatorRigWithLimit(t, 1)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := rig.ops.Suspend(ctx, rig.admin, rig.sourceID, "testing"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	got, truncated, err := rig.ops.SourceHistory(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("SourceHistory: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("SourceHistory returned %d rows, want the cap of 1", len(got))
	}
	if !truncated {
		t.Error("SourceHistory did not say it was truncated")
	}
}

// TestSourceHistoryFiltersToOperatorVerbsAndNeverLeaksACustomerAddress is the
// privacy guard change 2 exists for. /audit is super_admin-only because
// actor_email on a source.create or source.update row is the CUSTOMER's
// address (see Operators.Audit). SourceHistory is allowed under mayOperate,
// on a page an admin may reach, only because it is narrowed to the four verbs
// whose actor is always an operator. This seeds one row of each kind on the
// SAME source and asserts the customer's row -- and their address -- never
// comes back, however the page renders it.
//
// This is the test to break on purpose to prove the guard is real: widen
// usecase.sourceDecisionVerbs to include ActSourceCreate (or change
// SourceHistory to call audit.List instead of audit.ListByTarget) and this
// must fail.
func TestSourceHistoryFiltersToOperatorVerbsAndNeverLeaksACustomerAddress(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	// A customer's own act on this source. Nothing on this surface writes
	// one -- registering a source is the portal's -- so it is seeded
	// directly, standing in for what a real gate wrote on the other
	// listener.
	rig.log.entries = append(rig.log.entries, usecase.AuditEntry{
		ID: shared.ID("01J8XQ2M4E7N9V3B5C6D7F8CU1"), At: time.Now(),
		ActorID: rig.customer.ID, ActorEmail: rig.customer.Email,
		Verb: usecase.ActSourceCreate, TargetType: "source", TargetID: rig.sourceID,
		Note: "",
	})

	// An operator's own decision, through the real path -- so the row this
	// test expects back is exactly what a real gate would have written.
	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	got, _, err := rig.ops.SourceHistory(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("SourceHistory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1 (the approve, not the customer's create): %+v", len(got), got)
	}
	if got[0].Verb != usecase.ActSourceApprove {
		t.Errorf("verb = %q, want %q", got[0].Verb, usecase.ActSourceApprove)
	}
	for _, e := range got {
		if e.ActorEmail == rig.customer.Email {
			t.Fatalf("a customer's address reached an operator's read: %+v", e)
		}
	}
}

// An admin reads a source's own history the same as everything else it may
// see -- Messages, Deliveries, Senders -- because it is ordinary operator
// work, not the roster.
func TestAnAdminMayReadASourcesHistory(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if _, _, err := rig.ops.SourceHistory(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("an admin could not read a source's own history: %v", err)
	}
}

// A customer reaching this is refused the same as Messages and Deliveries.
func TestACustomerCannotReadASourcesHistory(t *testing.T) {
	rig := newOperatorRig(t)

	if _, _, err := rig.ops.SourceHistory(context.Background(), rig.customer, rig.sourceID); err == nil {
		t.Error("a customer read a source's own decision history")
	}
}

// A source with no decisions yet answers an empty, untruncated slice rather
// than an error -- the template's job is to say "no decisions have been
// recorded", not this method's.
func TestASourceWithNoHistoryAnswersEmpty(t *testing.T) {
	rig := newOperatorRig(t)

	got, truncated, err := rig.ops.SourceHistory(context.Background(), rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("SourceHistory: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want none: %+v", len(got), got)
	}
	if truncated {
		t.Error("an empty history was reported as truncated")
	}
}
