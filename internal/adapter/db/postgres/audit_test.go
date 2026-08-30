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
