//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
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
