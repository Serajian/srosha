package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type operatorRig struct {
	ops         *usecase.Operators
	log         *auditLog
	repo        fakeSources
	users       *fakeUsers
	notifs      *fakeNotifications
	customer    *user.User
	admin       *user.User
	superAdmin  *user.User
	sourceID    string
	credentials *fakeCredentials

	// messageID names the one message newOperatorRig seeds, with a body and a
	// delivery address a masking test can look for and must never find.
	messageID string
}

// testListLimit is generous enough that no ordinary fixture in this file
// truncates by accident. operator_listlimit_test.go builds its own rig with a
// limit small enough to trigger truncation on purpose.
const testListLimit = 50

func newOperatorRig(t *testing.T) *operatorRig { return newOperatorRigWithLimit(t, testListLimit) }

func newOperatorRigWithLimit(t *testing.T, listLimit int32) *operatorRig {
	t.Helper()

	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	customer, err := user.New(
		shared.ID("01K0ACCT0000000000000000C1"), "customer@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New customer: %v", err)
	}
	admin, err := user.New(
		shared.ID("01K0ACCT0000000000000000A1"), "admin@acme.test", user.RoleAdmin, at,
	)
	if err != nil {
		t.Fatalf("user.New admin: %v", err)
	}
	superAdmin, err := user.New(
		shared.ID("01K0ACCT0000000000000000S1"), "root@acme.test", user.RoleSuperAdmin, at,
	)
	if err != nil {
		t.Fatalf("user.New superAdmin: %v", err)
	}

	// A default address, so this rig's ordinary Approve calls succeed: most
	// of this file's tests are about the decision flow, not about addresses.
	// The guard on an unreachable source has its own dedicated tests below.
	src, err := source.New(
		"01J8XQ2M4E7N9V3B5C6D7F8SRC", customer.ID, "acme-billing",
		map[shared.Channel]string{shared.ChannelEmail: "billing@acme.test"}, at,
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}

	users := newFakeUsers()
	for _, u := range []*user.User{customer, admin, superAdmin} {
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatalf("seeding users: %v", err)
		}
	}

	log := &auditLog{}
	repo := fakeSources{byID: map[string]*source.Source{src.ID: src}}

	// One message, with a body and a delivery no operator method may ever hand
	// back. newOperatorRig seeds it here rather than a test mutating a read's
	// result, per the rule every fake in this package now follows.
	notifs := newFakeNotifications()
	deliveries := newFakeDeliveries()
	notifs.deliveries = deliveries

	msg, err := notification.New(
		shared.ID("01J8XQ2M4E7N9V3B5C6D7F8MSG"),
		notification.Origin{ID: src.ID, Name: src.Name, MaxPriority: shared.PriorityHigh},
		notification.Request{Body: "the message body", Priority: shared.PriorityNormal},
		at,
	)
	if err != nil {
		t.Fatalf("notification.New: %v", err)
	}
	if err := notifs.Create(context.Background(), msg); err != nil {
		t.Fatalf("seeding the message: %v", err)
	}

	set, err := delivery.NewSet(
		msg.ID,
		[]shared.Recipient{{Channel: shared.ChannelEmail, Address: "billing@acme.test"}},
		nil, seqIDs(), at,
	)
	if err != nil {
		t.Fatalf("delivery.NewSet: %v", err)
	}
	if err := deliveries.CreateByList(context.Background(), set); err != nil {
		t.Fatalf("seeding the delivery: %v", err)
	}

	// One credential, so Senders has something to read and a masking-style test
	// can look for it. own-support-bot on telegram, the same shape addSender
	// registers through the portal.
	creds := newFakeCredentials(nil)
	sender, err := credential.New(
		shared.ID("01J8XQ2M4E7N9V3B5C6D7F8SND"), src.ID, shared.ChannelTelegram,
		"our-support-bot", false, at,
	)
	if err != nil {
		t.Fatalf("credential.New: %v", err)
	}
	creds.byID[sender.ID] = *sender

	return &operatorRig{
		ops: usecase.NewOperators(
			repo, users, notifs, deliveries, creds, log,
			usecase.NewGate(log, nil, seqIDs(), fixedNow(at)), fixedNow(at), listLimit,
		),
		log:         log,
		repo:        repo,
		users:       users,
		notifs:      notifs,
		customer:    customer,
		admin:       admin,
		superAdmin:  superAdmin,
		sourceID:    src.ID,
		messageID:   msg.ID.String(),
		credentials: creds,
	}
}

// A customer reaching the use case is refused there, not only at the guard. The
// page is one boundary; this is the other, and it is the one that survives a
// route being moved.
func TestACustomerCannotApprove(t *testing.T) {
	rig := newOperatorRig(t)

	if err := rig.ops.Approve(context.Background(), rig.customer, rig.sourceID); err == nil {
		t.Fatal("a customer approved a source")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused approval still wrote an audit row")
	}
}

// The refusal has an identity, not just a type. Without it, "not an operator"
// is indistinguishable from a source's own refusals -- ErrSourceInactive,
// ErrCustomAddressNotAllowed -- since all three surface as the same
// errs.ErrForbidden, and matching the message text is not allowed.
func TestACustomerIsRefusedWithAnIdentifiableError(t *testing.T) {
	rig := newOperatorRig(t)

	err := rig.ops.Approve(context.Background(), rig.customer, rig.sourceID)
	if !errors.Is(err, usecase.ErrNotOperator) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotOperator", err)
	}
}

// Approving is what makes a source able to send, and the test asks the domain
// rather than reading the column: EnsureActive is what the sending path calls.
func TestApprovingLetsASourceSend(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	before, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if before.EnsureActive() == nil {
		t.Fatal("a new source could already send")
	}

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	after, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if err := after.EnsureActive(); err != nil {
		t.Errorf("an approved source still cannot send: %v", err)
	}
}

// The queue is the panel's reason to exist: a decision has to take a source out
// of it, whichever way the decision went.
func TestADecisionEmptiesTheQueue(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	queue, _, err := rig.ops.Queue(ctx, rig.admin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("the queue has %d sources, want 1", len(queue))
	}

	if err := rig.ops.Refuse(ctx, rig.admin, rig.sourceID, "no working address"); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	queue, _, err = rig.ops.Queue(ctx, rig.admin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(queue) != 0 {
		t.Errorf("a decided source is still in the queue: %d", len(queue))
	}
}

// The reason reaches both places, and they are different places on purpose:
// the source carries what the customer reads, the audit row carries what is
// still readable after the next decision overwrites it.
func TestARefusalIsWrittenTwice(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Refuse(ctx, rig.admin, rig.sourceID, "no working address"); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	src, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if src.ReviewNote != "no working address" {
		t.Errorf("the customer will not see the reason: %q", src.ReviewNote)
	}

	if len(rig.log.entries) != 1 {
		t.Fatalf("wrote %d audit rows", len(rig.log.entries))
	}
	if rig.log.entries[0].Verb != usecase.ActSourceRefuse {
		t.Errorf("verb = %q", rig.log.entries[0].Verb)
	}
	if rig.log.entries[0].Note != "no working address" {
		t.Errorf("audit note = %q", rig.log.entries[0].Note)
	}
}

// A refusal with no reason never reaches the database.
func TestARefusalWithNoReasonChangesNothing(t *testing.T) {
	rig := newOperatorRig(t)

	if err := rig.ops.Refuse(context.Background(), rig.admin, rig.sourceID, ""); err == nil {
		t.Fatal("a source was refused with no reason")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused refusal still wrote an audit row")
	}
}

// An approved source cannot be refused -- refusing is a decision at the door,
// and Refuse must let that domain error through untouched, writing no row for
// a refusal that never happened.
func TestAnApprovedSourceCannotBeRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	rig.log.entries = nil // the approval's own row is not what this test counts

	if err := rig.ops.Refuse(ctx, rig.admin, rig.sourceID, "changed my mind"); err == nil {
		t.Fatal("an approved source was refused")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused refusal still wrote an audit row")
	}
}

// Suspending stops a source that already got through, and the reason lands on
// the audit row, not on the source -- a suspension is not a refusal, and the
// customer's page says something different for each.
func TestSuspendingStopsAnApprovedSourceWithoutTouchingItsReviewNote(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if err := rig.ops.Suspend(ctx, rig.admin, rig.sourceID, "abuse report"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	src, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if err := src.EnsureActive(); err == nil {
		t.Error("a suspended source can still send")
	}
	if src.ReviewNote != "" {
		t.Errorf("a suspension wrote onto the source's own review note: %q", src.ReviewNote)
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Verb != usecase.ActSourceSuspend {
		t.Errorf("verb = %q", last.Verb)
	}
	if last.Note != "abuse report" {
		t.Errorf("audit note = %q, want the suspension's reason", last.Note)
	}
}

// A suspension's note is not passed straight to the audit row: it goes through
// the same trim a refusal's reason does, so whitespace typed around it does
// not become part of the permanent record.
func TestASuspensionNoteIsTrimmed(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	// Approved first: only a source that got through can be suspended.
	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if err := rig.ops.Suspend(ctx, rig.admin, rig.sourceID, "  abuse report  "); err != nil {
		t.Fatalf("Suspend: %v", err)
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Note != "abuse report" {
		t.Errorf("audit note = %q, want it trimmed", last.Note)
	}
}

// Nothing in the domain bounds a suspension's note -- Suspend the domain
// method takes none -- so the use case has to, or an operator's arbitrarily
// long string reaches audit_log.note unchecked.
func TestASuspensionNoteThatIsTooLongIsRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wrote := len(rig.log.entries)

	tooLong := strings.Repeat("a", usecase.MaxOperatorNoteLen+1)
	if err := rig.ops.Suspend(ctx, rig.admin, rig.sourceID, tooLong); err == nil {
		t.Fatal("a source was suspended with an over-long note")
	}
	if len(rig.log.entries) != wrote {
		t.Error("a refused suspension still wrote an audit row")
	}
}

// The two guards Refuse already had, on the other two verbs, and the audit
// row is what proves they run BEFORE the gate: a decision the domain refuses
// must leave no record of having happened.
func TestSuspendingAndRestoringAQueuedSourceIsRefusedAndRecordsNothing(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Suspend(ctx, rig.admin, rig.sourceID, "abuse report"); err == nil {
		t.Error("a source still in the queue was suspended")
	} else if !errors.Is(err, source.ErrNotApproved) {
		t.Errorf("err = %v, want it to wrap source.ErrNotApproved", err)
	}

	if err := rig.ops.Restore(ctx, rig.admin, rig.sourceID); err == nil {
		t.Error("a source still in the queue was restored")
	} else if !errors.Is(err, source.ErrNotReviewed) {
		t.Errorf("err = %v, want it to wrap source.ErrNotReviewed", err)
	}

	if len(rig.log.entries) != 0 {
		t.Errorf("%d audit rows were written for decisions that did not happen",
			len(rig.log.entries))
	}
}

// Restore is the way back from either Suspend or Refuse, and it is what turns
// a source the queue never approved into one that has now been let out.
func TestRestoringARefusedSourceLetsItSend(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Refuse(ctx, rig.admin, rig.sourceID, "no working address"); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	if err := rig.ops.Restore(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	src, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if err := src.EnsureActive(); err != nil {
		t.Errorf("a restored source still cannot send: %v", err)
	}
	if !src.IsApproved() {
		t.Error("a restored source was never marked approved")
	}
}

// A customer reaching any read path is refused the same as at a write -- the
// queue and the full list are an operator's tools, not a customer's.
func TestACustomerCannotReadTheQueueOrTheFullList(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if _, _, err := rig.ops.Queue(ctx, rig.customer); err == nil {
		t.Error("a customer read the queue")
	}
	if _, _, err := rig.ops.AllSources(ctx, rig.customer); err == nil {
		t.Error("a customer read every source")
	}
	if _, err := rig.ops.Source(ctx, rig.customer, rig.sourceID); err == nil {
		t.Error("a customer read a source through the operator path")
	}
}

// A decision touches only the review columns. UpdateReview writes exactly
// those, and the fake mirrors that faithfully -- so a use case that carried a
// rename along with an approval would leave it behind here too.
func TestADecisionNeverRenamesTheSource(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.Approve(ctx, rig.admin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	src, err := rig.ops.Source(ctx, rig.admin, rig.sourceID)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if src.Name != "acme-billing" {
		t.Errorf("name = %q, a decision renamed the source", src.Name)
	}
}

// A super_admin is an operator too, and every decision is open to both roles
// alike -- there is no third check that only one of them passes.
func TestASuperAdminMayDecideToo(t *testing.T) {
	rig := newOperatorRig(t)

	if err := rig.ops.Approve(context.Background(), rig.superAdmin, rig.sourceID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
}

// A source with nowhere to send cannot be approved -- letting it out would
// make it look like it works when every message it sends is going to fail.
// Reached through the use case, and the refusal writes no audit row for a
// decision that never happened, same as every other domain guard here.
func TestApprovingASourceWithNowhereToSendIsRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	unreachable, err := source.New(
		"01J8XQ2M4E7N9V3B5C6D7F8UNR", rig.customer.ID, "acme-unreachable", nil,
		time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}
	if err := rig.repo.Create(ctx, unreachable); err != nil {
		t.Fatalf("seeding the source: %v", err)
	}
	wrote := len(rig.log.entries)

	err = rig.ops.Approve(ctx, rig.admin, unreachable.ID)
	if !errors.Is(err, source.ErrNoReachableAddress) {
		t.Errorf("err = %v, want it to wrap source.ErrNoReachableAddress", err)
	}
	if len(rig.log.entries) != wrote {
		t.Error("a refused approval still wrote an audit row")
	}
}

// Restore is guarded the same way as Approve.
func TestRestoringASourceWithNowhereToSendIsRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	unreachable, err := source.New(
		"01J8XQ2M4E7N9V3B5C6D7F8UN2", rig.customer.ID, "acme-unreachable-2", nil,
		time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("source.New: %v", err)
	}
	if err := rig.repo.Create(ctx, unreachable); err != nil {
		t.Fatalf("seeding the source: %v", err)
	}
	if err := rig.ops.Refuse(ctx, rig.admin, unreachable.ID, "no working address"); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	wrote := len(rig.log.entries)

	err = rig.ops.Restore(ctx, rig.admin, unreachable.ID)
	if !errors.Is(err, source.ErrNoReachableAddress) {
		t.Errorf("err = %v, want it to wrap source.ErrNoReachableAddress", err)
	}
	if len(rig.log.entries) != wrote {
		t.Error("a refused restore still wrote an audit row")
	}
}
