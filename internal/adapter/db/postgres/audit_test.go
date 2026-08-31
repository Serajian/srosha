//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

func TestAnAuditRowIsWritten(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("AUD"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	err := postgres.NewAuditRepository(pool).Record(ctx, usecase.AuditEntry{
		ID:         shared.ID(ulid("AU1")),
		At:         at,
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Verb:       "source.create",
		TargetType: "source",
		TargetID:   "01K0SRC0000000000000000000",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	var verb, email string
	row := pool.QueryRow(
		ctx, `SELECT verb, actor_email FROM audit_log WHERE actor_id = $1`, actor.ID.String(),
	)
	if err := row.Scan(&verb, &email); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if verb != "source.create" || email != "ops@acme.test" {
		t.Errorf("row = %q by %q", verb, email)
	}
}

// List is the whole reason the table is worth reading, and Note and
// ActorEmail are the two columns nothing else on the row can stand in for --
// TargetID says what changed, not who did it or why. If the mapping between
// gen.AuditLog and usecase.AuditEntry drops either on the way back, this must
// fail.
func TestListRoundTripsNoteAndActorEmail(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("LST"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	repo := postgres.NewAuditRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)
	entry := usecase.AuditEntry{
		ID:         shared.ID(ulid("AU3")),
		At:         at,
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Verb:       usecase.ActSourceRefuse,
		TargetType: "source",
		TargetID:   "01K0SRC0000000000000000000",
		Note:       "no working address",
	}
	if err := repo.Record(ctx, entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}

	row := got[0]
	if row.Note != "no working address" {
		t.Errorf("note = %q, want it to survive the round trip", row.Note)
	}
	if row.ActorEmail != "ops@acme.test" {
		t.Errorf("actor email = %q, want it to survive the round trip", row.ActorEmail)
	}
	if row.ID != entry.ID || row.Verb != entry.Verb || row.TargetID != entry.TargetID {
		t.Errorf("row = %+v, want it to match what was recorded", row)
	}
}

// Newest first, and capped at the limit passed in -- the two things List
// promises beyond a bare SELECT.
func TestListIsNewestFirstAndCapped(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("ORD"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	repo := postgres.NewAuditRepository(pool)
	base := time.Now().UTC().Truncate(time.Microsecond)
	verbs := []string{"source.create", "source.approve", "source.suspend"}
	for i, verb := range verbs {
		entry := usecase.AuditEntry{
			ID:         shared.ID(ulid(string(rune('A' + i)))),
			At:         base.Add(time.Duration(i) * time.Second),
			ActorID:    actor.ID,
			ActorEmail: actor.Email,
			Verb:       verb,
			TargetType: "source",
			TargetID:   "01K0SRC0000000000000000000",
		}
		if err := repo.Record(ctx, entry); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}

	got, err := repo.List(ctx, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want the cap of 2", len(got))
	}
	if got[0].Verb != "source.suspend" || got[1].Verb != "source.approve" {
		t.Errorf("verbs = [%q, %q], want the two newest, newest first",
			got[0].Verb, got[1].Verb)
	}
}

// ListByTarget is what /sources/:id's own decision history reads through,
// and its whole point is the verb filter: usecase.sourceDecisionVerbs names
// the four operator verbs, and this proves the statement itself -- not just
// the use case above it -- refuses to hand back a row outside that set, even
// when the row names the very target being asked about. A customer's own
// source.create sits right beside the operator's source.approve on the same
// target_id here, and only the approve may come back.
//
// It also proves the OTHER half of the filter: a row for a different source,
// carrying a verb that IS in the set, must not leak in through target_id
// alone.
func TestListByTargetFiltersToTheGivenVerbsAndTarget(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	operator := aUser(t, ulid("OP1"), "ops@acme.test")
	customer := aUser(t, ulid("CU1"), "billing@acme.test")
	for _, u := range []*user.User{operator, customer} {
		if err := postgres.NewUserRepository(pool).Create(ctx, u); err != nil {
			t.Fatalf("Create user: %v", err)
		}
	}

	repo := postgres.NewAuditRepository(pool)
	base := time.Now().UTC().Truncate(time.Microsecond)
	const target = "01K0SRC0000000000000000000"
	const otherTarget = "01K0SRC0000000000000000001"

	rows := []usecase.AuditEntry{
		{
			ID: shared.ID(ulid("R01")), At: base, ActorID: customer.ID,
			ActorEmail: customer.Email, Verb: usecase.ActSourceCreate,
			TargetType: "source", TargetID: target,
		},
		{
			ID: shared.ID(ulid("R02")), At: base.Add(time.Second), ActorID: operator.ID,
			ActorEmail: operator.Email, Verb: usecase.ActSourceApprove,
			TargetType: "source", TargetID: target,
		},
		{
			ID: shared.ID(ulid("R03")), At: base.Add(2 * time.Second), ActorID: operator.ID,
			ActorEmail: operator.Email, Verb: usecase.ActSourceSuspend,
			TargetType: "source", TargetID: target,
		},
		{
			// Same verb, DIFFERENT target -- must not leak in on the verb alone.
			ID: shared.ID(ulid("R04")), At: base.Add(3 * time.Second), ActorID: operator.ID,
			ActorEmail: operator.Email, Verb: usecase.ActSourceApprove,
			TargetType: "source", TargetID: otherTarget,
		},
	}
	for _, e := range rows {
		if err := repo.Record(ctx, e); err != nil {
			t.Fatalf("Record %s: %v", e.ID, err)
		}
	}

	verbs := []string{
		usecase.ActSourceApprove, usecase.ActSourceRefuse,
		usecase.ActSourceSuspend, usecase.ActSourceRestore,
	}
	got, err := repo.ListByTarget(ctx, "source", target, verbs, 10)
	if err != nil {
		t.Fatalf("ListByTarget: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (approve and suspend, not create): %+v", len(got), got)
	}
	if got[0].Verb != usecase.ActSourceSuspend || got[1].Verb != usecase.ActSourceApprove {
		t.Errorf("verbs = [%q, %q], want the two newest of THIS target, newest first",
			got[0].Verb, got[1].Verb)
	}
	for _, e := range got {
		if e.TargetID != target {
			t.Errorf("row for target %q came back asking about %q", e.TargetID, target)
		}
		if e.ActorEmail == customer.Email {
			t.Errorf("the customer's row reached a read meant only for operator verbs: %+v", e)
		}
	}
}

// The actor has to exist. An audit row naming nobody is a row that answers
// nothing.
func TestAnAuditRowNeedsARealActor(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	err := postgres.NewAuditRepository(pool).Record(context.Background(), usecase.AuditEntry{
		ID:         shared.ID(ulid("AU2")),
		At:         time.Now().UTC(),
		ActorID:    shared.ID(ulid("NOB")),
		ActorEmail: "nobody@acme.test",
		Verb:       "source.create",
		TargetType: "source",
		TargetID:   "x",
	})
	if err == nil {
		t.Fatal("Record: want the foreign key to refuse it")
	}
}

// Nothing else in this plan puts the gate and a real database side by side, and
// a gate whose only test uses a stand-in is a gate nobody has watched write a
// row.
func TestTheGateWritesThroughToPostgres(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("GAT"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	gate := usecase.NewGate(
		postgres.NewAuditRepository(pool),
		func() shared.ID { return shared.ID(ulid("GA1")) },
		func() time.Time { return at },
	)

	ran := false
	err := gate.Do(ctx, actor, usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !ran {
		t.Fatal("the action did not run")
	}

	var verb string
	row := pool.QueryRow(ctx, `SELECT verb FROM audit_log WHERE actor_id = $1`, actor.ID.String())
	if err := row.Scan(&verb); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if verb != "source.create" {
		t.Errorf("verb = %q", verb)
	}
}
