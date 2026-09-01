package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type auditLog struct {
	entries []usecase.AuditEntry
	err     error
}

func (a *auditLog) Record(_ context.Context, e usecase.AuditEntry) error {
	if a.err != nil {
		return a.err
	}
	a.entries = append(a.entries, e)
	return nil
}

// List hands back copies, newest first, capped at limit -- as postgres would.
// Returning the stored slice's own entries would let a caller's mutation reach
// storage before any Record call, the aliasing bug this package has already
// hit three times.
func (a *auditLog) List(_ context.Context, limit int32) ([]usecase.AuditEntry, error) {
	if a.err != nil {
		return nil, a.err
	}
	n := len(a.entries)
	if int32(n) > limit {
		n = int(limit)
	}
	out := make([]usecase.AuditEntry, n)
	for i := 0; i < n; i++ {
		out[i] = a.entries[len(a.entries)-1-i]
	}
	return out, nil
}

// ListByTarget mirrors postgres's own statement: target_type, target_id and
// the given verb set, newest first, capped at limit. A fake that ignored the
// verb list would let usecase.sourceDecisionVerbs drift without any unit test
// noticing -- this is what makes TestSourceHistory... in
// operator_listlimit_test.go a real test of that list, not a test of a fake
// that always agrees.
func (a *auditLog) ListByTarget(
	_ context.Context, targetType, targetID string, verbs []string, limit int32,
) ([]usecase.AuditEntry, error) {
	if a.err != nil {
		return nil, a.err
	}
	allowed := make(map[string]bool, len(verbs))
	for _, v := range verbs {
		allowed[v] = true
	}

	var matched []usecase.AuditEntry
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := a.entries[i]
		if e.TargetType == targetType && e.TargetID == targetID && allowed[e.Verb] {
			matched = append(matched, e)
		}
	}
	if int32(len(matched)) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

var gateNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func anActor(t *testing.T) *user.User {
	t.Helper()

	u, err := user.New(
		shared.ID("01K0ACCT0000000000000000AB"), "ops@acme.test", user.RoleAdmin, gateNow,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

func anOperator(t *testing.T) *user.User {
	t.Helper()

	u, err := user.New(
		shared.ID("01K0OPER0000000000000000AB"), "operator@acme.test", user.RoleAdmin, gateNow,
	)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

func newGate(t *testing.T, log *auditLog) *usecase.Gate {
	t.Helper()

	return usecase.NewGate(log, nil, seqIDs(), fixedNow(gateNow))
}

func TestAChangeLeavesExactlyOneRow(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(log.entries) != 1 {
		t.Fatalf("wrote %d rows, want 1", len(log.entries))
	}
	e := log.entries[0]
	if e.Verb != "source.create" || e.TargetID != "01K0SRC0000000000000000000" {
		t.Errorf("entry = %+v", e)
	}
	if e.ActorEmail != "ops@acme.test" {
		t.Errorf("actor email = %q, want it copied onto the row", e.ActorEmail)
	}
	if !e.At.Equal(gateNow) {
		t.Errorf("at = %v", e.At)
	}
}

// The log records ATTEMPTS, not outcomes. Somebody investigating needs to see
// that a key revocation was tried at all, and a row written only on success
// would hide exactly the attempts worth looking at.
func TestAnAttemptIsRecordedEvenWhenItFails(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	boom := errors.New("the write failed")
	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "key.revoke", TargetType: "key", TargetID: "01K0KEY00000000000000000AB",
	}, func(context.Context) error { return boom })

	if !errors.Is(err, boom) {
		t.Errorf("Do = %v, want the action's own error", err)
	}
	if len(log.entries) != 1 {
		t.Fatalf("wrote %d rows, want the attempt recorded", len(log.entries))
	}
	if log.entries[0].Verb != "key.revoke" {
		t.Errorf("entry = %+v", log.entries[0])
	}
}

// An unrecorded change is worse than a refused one: the point of the gate is
// that nothing happens without a trace.
func TestAChangeThatCannotBeRecordedIsRefused(t *testing.T) {
	log := &auditLog{err: errors.New("the log is unreachable")}
	g := newGate(t, log)

	ran := false
	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error {
		ran = true
		return nil
	})

	if err == nil {
		t.Fatal("Do: want an error")
	}
	if ran {
		t.Error("the change ran even though it could not be recorded")
	}
}

func TestAnActorIsRequired(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	err := g.Do(context.Background(), nil, usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "x",
	}, func(context.Context) error { return nil })

	if err == nil {
		t.Fatal("Do with no actor succeeded")
	}
}

// A verb and a target do not say why. The reason has to be on the row, because
// where it lives on the source is overwritten by the next decision.
func TestTheReasonReachesTheAuditRow(t *testing.T) {
	log := &auditLog{}
	gate := usecase.NewGate(log, nil, seqIDs(), fixedNow(time.Now().UTC()))
	actor := anOperator(t)

	act := usecase.Act{
		Verb: usecase.ActSourceRefuse, TargetType: "source",
		TargetID: "01K0SRC0000000000000000000", Note: "no working address",
	}
	err := gate.Do(context.Background(), actor, act, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(log.entries) != 1 {
		t.Fatalf("wrote %d rows", len(log.entries))
	}
	if log.entries[0].Note != "no working address" {
		t.Errorf("note = %q", log.entries[0].Note)
	}
}

// heard records what the gate told an operator.
type heard struct{ subjects, details []string }

func (h *heard) Notify(_ context.Context, subject, detail string) {
	h.subjects = append(h.subjects, subject)
	h.details = append(h.details, detail)
}

// The audit records attempts and the alert records what happened. A source
// that failed to register must not reach an operator as one that did.
func TestTheGateAlertsOnlyAfterTheChangeSucceeds(t *testing.T) {
	log := &auditLog{}
	told := &heard{}
	g := usecase.NewGate(log, told, seqIDs(), fixedNow(gateNow))

	act := usecase.Act{Verb: usecase.ActSourceCreate, TargetType: "source", TargetID: "01K0"}
	err := g.Do(t.Context(), anActor(t), act, func(context.Context) error {
		return errors.New("the change failed")
	})
	if err == nil {
		t.Fatal("a failing change was reported as done")
	}

	if len(told.subjects) != 0 {
		t.Errorf("an operator was told about a change that did not happen: %v", told.subjects)
	}
	if len(log.entries) != 1 {
		t.Errorf("the attempt was not audited: %d rows", len(log.entries))
	}
}

// And it does alert when the change went through.
func TestTheGateAlertsWhenTheChangeHappens(t *testing.T) {
	told := &heard{}
	g := usecase.NewGate(&auditLog{}, told, seqIDs(), fixedNow(gateNow))

	act := usecase.Act{Verb: usecase.ActSourceCreate, TargetType: "source", TargetID: "01K0SRC"}
	if err := g.Do(t.Context(), anActor(t), act, func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(told.subjects) != 1 {
		t.Fatalf("told %d things, want 1: %v", len(told.subjects), told.subjects)
	}
}

// What an operator reads has to name the verb, what it happened to, and who
// did it -- those three are the whole message.
func TestTheAlertNamesTheVerbTheTargetAndTheActor(t *testing.T) {
	told := &heard{}
	g := usecase.NewGate(&auditLog{}, told, seqIDs(), fixedNow(gateNow))

	actor := anActor(t)
	act := usecase.Act{Verb: usecase.ActSourceCreate, TargetType: "source", TargetID: "01K0SRC"}
	if err := g.Do(t.Context(), actor, act, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !strings.Contains(told.subjects[0], usecase.ActSourceCreate) {
		t.Errorf("subject %q does not name the verb", told.subjects[0])
	}
	if !strings.Contains(told.details[0], "01K0SRC") {
		t.Errorf("detail %q does not name the target", told.details[0])
	}
	if !strings.Contains(told.details[0], actor.Email) {
		t.Errorf("detail %q does not name who did it", told.details[0])
	}
}

// A gate with no alerter is silent rather than broken, so nothing that was
// built before this existed has to learn about it.
func TestAGateWithNoAlerterIsSilent(t *testing.T) {
	g := usecase.NewGate(&auditLog{}, nil, seqIDs(), fixedNow(gateNow))

	act := usecase.Act{Verb: usecase.ActKeyIssue, TargetType: "key", TargetID: "01K0KEY"}
	if err := g.Do(t.Context(), anActor(t), act, func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("Do with no alerter: %v", err)
	}
}
