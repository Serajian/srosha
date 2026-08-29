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
	sources *usecase.Sources
	log     *auditLog
	actor   *user.User
	at      time.Time
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

	log := &auditLog{}
	repo := fakeSources{byID: map[string]*source.Source{}}

	return &sourceRig{
		sources: usecase.NewSources(
			repo, usecase.NewGate(log, seqIDs(), fixedNow(at)), seqIDs(), fixedNow(at),
		),
		log:   log,
		actor: actor,
		at:    at,
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
