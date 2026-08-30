package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type sourceRig struct {
	sources  *usecase.Sources
	log      *auditLog
	actor    *user.User
	stranger *user.User
	at       time.Time
}

// registered is one source the actor owns. A helper rather than something the
// rig seeds, so the tests that count what a person has still count only what
// they registered themselves.
func (r *sourceRig) registered(t *testing.T) string {
	t.Helper()

	src, err := r.sources.Register(
		context.Background(), r.actor, usecase.SourceRegistration{Name: "acme-billing"},
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.log.entries = nil // the registration's own row is not what the caller counts
	return src.ID
}

func newSourceRig(t *testing.T) *sourceRig {
	t.Helper()

	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	actor, err := user.New(
		shared.ID("01K0ACCT0000000000000000AB"), "me@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	stranger, err := user.New(
		shared.ID("01K0ACCT0000000000000000AC"), "them@acme.test", user.RoleCustomer, at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	log := &auditLog{}
	repo := fakeSources{byID: map[string]*source.Source{}}
	return &sourceRig{
		sources: usecase.NewSources(
			repo, usecase.NewGate(log, seqIDs(), fixedNow(at)), seqIDs(), fixedNow(at),
		),
		log:      log,
		actor:    actor,
		stranger: stranger,
		at:       at,
	}
}

// A registered source waits. Nothing it is given reaches anybody until an
// operator approves it, and the customer is told that rather than discovering
// it from a send that failed.
func TestARegisteredSourceWaitsForApproval(t *testing.T) {
	rig := newSourceRig(t)

	got, err := rig.sources.Register(context.Background(), rig.actor, usecase.SourceRegistration{
		Name: "acme-billing",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got.IsActive {
		t.Error("a source was registered already able to send")
	}
	if got.IsApproved() {
		t.Error("a source was registered already approved")
	}
	if got.OwnerUserID != rig.actor.ID {
		t.Errorf("owner = %q", got.OwnerUserID)
	}
}

// The gate's first caller. Nothing that changes anything may go around it.
func TestRegisteringASourceLeavesAnAuditRow(t *testing.T) {
	rig := newSourceRig(t)

	if _, err := rig.sources.Register(context.Background(), rig.actor,
		usecase.SourceRegistration{Name: "acme-billing"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if len(rig.log.entries) != 1 {
		t.Fatalf("wrote %d audit rows, want 1", len(rig.log.entries))
	}
	e := rig.log.entries[0]
	if e.Verb != usecase.ActSourceCreate {
		t.Errorf("verb = %q", e.Verb)
	}
	if e.ActorEmail != "me@acme.test" {
		t.Errorf("actor = %q, want the address copied onto the row", e.ActorEmail)
	}
}

// A customer sees their own and nobody else's, and asking for somebody else's
// by id answers the same as asking for one that does not exist.
func TestSomebodyElsesSourceIsNotFound(t *testing.T) {
	rig := newSourceRig(t)

	theirs, err := user.New(
		shared.ID("01K0ACCT0000000000000000AC"), "them@acme.test", user.RoleCustomer, rig.at,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	mine, err := rig.sources.Register(context.Background(), rig.actor,
		usecase.SourceRegistration{Name: "mine"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = rig.sources.One(context.Background(), theirs, mine.ID)
	if err == nil {
		t.Fatal("somebody read a source they do not own")
	}
	if !errors.Is(err, source.ErrNotFound) {
		t.Errorf("One = %v, want ErrNotFound -- any other answer says the id exists", err)
	}
}

// Mine is the same rule from the other side: a list, not a filter somebody has
// to remember to apply.
func TestMineIsOnlyMine(t *testing.T) {
	rig := newSourceRig(t)
	ctx := context.Background()

	for _, name := range []string{"one", "two"} {
		if _, err := rig.sources.Register(ctx, rig.actor,
			usecase.SourceRegistration{Name: name}); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}

	got, err := rig.sources.Mine(ctx, rig.actor)
	if err != nil {
		t.Fatalf("Mine: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2", len(got))
	}
}

func TestTooManySources(t *testing.T) {
	rig := newSourceRig(t)

	var err error
	for range usecase.MaxSourcesPerUser + 1 {
		_, err = rig.sources.Register(context.Background(), rig.actor,
			usecase.SourceRegistration{Name: "acme"})
	}
	if err == nil {
		t.Error("a customer registered more sources than the limit allows")
	}
}

// A source with no name is not a source. The domain refuses it, and the gate
// never sees it -- there is nothing to record about a change that could not be
// described.
func TestASourceNeedsAName(t *testing.T) {
	rig := newSourceRig(t)

	if _, err := rig.sources.Register(context.Background(), rig.actor,
		usecase.SourceRegistration{Name: "  "}); err == nil {
		t.Fatal("a source was registered with no name")
	}
	if len(rig.log.entries) != 0 {
		t.Errorf("wrote %d audit rows for something that never happened", len(rig.log.entries))
	}
}

// Somebody else's source answers ErrNotFound rather than a refusal, for the
// same reason One does: a refusal confirms the id exists.
func TestOnlyTheOwnerChangesASource(t *testing.T) {
	rig := newSourceRig(t)
	id := rig.registered(t)

	_, err := rig.sources.Update(
		context.Background(), rig.stranger, id,
		usecase.SourceSettings{Name: "theirs now"},
	)
	if err == nil {
		t.Fatal("a stranger changed somebody else's source")
	}
	if !errors.Is(err, source.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound so the id is not confirmed", err)
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused change still wrote an audit row")
	}
}

func TestAChangeLeavesAnAuditRow(t *testing.T) {
	rig := newSourceRig(t)
	id := rig.registered(t)
	before := len(rig.log.entries)

	_, err := rig.sources.Update(
		context.Background(), rig.actor, id,
		usecase.SourceSettings{Name: "acme-alerts", Description: "pages the on-call"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(rig.log.entries) != before+1 {
		t.Fatalf("the change wrote %d rows", len(rig.log.entries)-before)
	}
	if got := rig.log.entries[before].Verb; got != usecase.ActSourceUpdate {
		t.Errorf("verb = %q", got)
	}
}

// The customer's form cannot carry the ceiling, and this is the assertion that
// stays true if somebody later widens SourceSettings without thinking.
func TestAChangeCannotTouchWhatIsOurs(t *testing.T) {
	rig := newSourceRig(t)
	id := rig.registered(t)
	ctx := context.Background()

	before, err := rig.sources.One(ctx, rig.actor, id)
	if err != nil {
		t.Fatalf("One: %v", err)
	}
	before.MaxPriority = shared.PriorityCritical
	before.AllowCustomAddress = true

	got, err := rig.sources.Update(
		ctx, rig.actor, id, usecase.SourceSettings{Name: "renamed"},
	)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got.MaxPriority != shared.PriorityCritical {
		t.Errorf("the ceiling moved to %v", got.MaxPriority)
	}
	if !got.AllowCustomAddress {
		t.Error("allow_custom_address was cleared")
	}
	if got.IsActive {
		t.Error("the source switched itself on")
	}
	if got.OwnerUserID != rig.actor.ID {
		t.Errorf("the owner changed to %q", got.OwnerUserID)
	}
}
