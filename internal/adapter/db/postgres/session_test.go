//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/shared"
)

func TestASessionRoundTripsAndCanBeEnded(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewSessionRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	s := session.New(shared.ID(ulid("SE1")), u.ID, at)
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Read(ctx, s.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("user = %q", got.UserID)
	}

	if err := repo.Delete(ctx, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Read(ctx, s.ID); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Read after delete = %v, want ErrNotFound", err)
	}
}

// Touch moves the idle deadline and nothing else. A session that came back with
// its old last_seen_at would go idle while somebody was using it.
func TestTouchingASessionIsWrittenBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewSessionRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	s := session.New(shared.ID(ulid("SE2")), u.ID, at)
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	later := at.Add(time.Hour)
	s.Touch(later)
	if err := repo.Touch(ctx, s); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := repo.Read(ctx, s.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("last seen = %v, want %v", got.LastSeenAt, later)
	}
	if !got.ExpiresAt.Equal(s.ExpiresAt) {
		t.Error("touching a session moved its absolute deadline")
	}
}

// Deleting a user takes their sessions with them, so deactivating somebody and
// then removing them cannot leave a browser signed in.
func TestSessionsGoWithTheirUser(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aStoredUser(t, pool, "SIU", "ops@acme.test")
	repo := postgres.NewSessionRepository(pool)

	s := session.New(shared.ID(ulid("SE3")), u.ID, time.Now().UTC().Truncate(time.Microsecond))
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID.String()); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := repo.Read(ctx, s.ID); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Read = %v, want the session gone with its user", err)
	}
}
