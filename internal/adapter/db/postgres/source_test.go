//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

func aSource(id string) *source.Source {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return &source.Source{
		ID:                 id,
		OwnerUserID:        testOwner,
		Name:               "Acme",
		MaxPriority:        shared.PriorityHigh,
		IsActive:           true,
		AllowCustomAddress: false,
		DefaultAddresses:   map[shared.Channel]string{shared.ChannelTelegram: "-100123"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

// letOut and suspendSource move a source the only two ways an operator now can:
// through the domain's own transitions and UpdateReview.
//
// They replace repo.Activate and repo.Deactivate, which were deleted with
// repo.Update once nothing in production called any of the three. Activate in
// particular set is_active while leaving approved_at and reviewed_at alone,
// which is a fifth state the table has no meaning for -- so a fixture built on
// it was setting up a row the service cannot produce.
func letOut(t *testing.T, repo *postgres.SourceRepository, id string) {
	t.Helper()

	ctx := context.Background()
	s, err := repo.ReadByID(ctx, id)
	if err != nil {
		t.Fatalf("ReadByID before approving: %v", err)
	}
	s.Approve(time.Now().UTC())
	if err := repo.UpdateReview(ctx, s); err != nil {
		t.Fatalf("UpdateReview approving: %v", err)
	}
}

func suspendSource(t *testing.T, repo *postgres.SourceRepository, id string) {
	t.Helper()

	ctx := context.Background()
	s, err := repo.ReadByID(ctx, id)
	if err != nil {
		t.Fatalf("ReadByID before suspending: %v", err)
	}
	if err := s.Suspend(time.Now().UTC()); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if err := repo.UpdateReview(ctx, s); err != nil {
		t.Fatalf("UpdateReview suspending: %v", err)
	}
}

func TestSourceRoundTrips(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	want := aSource(ulid("S1"))

	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ReadByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}

	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.MaxPriority != want.MaxPriority {
		t.Errorf("MaxPriority = %v, want %v", got.MaxPriority, want.MaxPriority)
	}
	// A new source is switched off and has never been approved. Anybody may
	// register one; an operator decides when it may send.
	if got.IsActive {
		t.Error("a new source came back able to send, with nobody having approved it")
	}
	if got.IsApproved() {
		t.Error("a new source came back already approved")
	}
	if got.DefaultAddresses[shared.ChannelTelegram] != "-100123" {
		t.Errorf("DefaultAddresses = %v", got.DefaultAddresses)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// The domain has to be able to say "source is not active" rather than "no such
// source", which it can only do if the row comes back at all.
func TestSuspendedSourceStillReadsBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S2"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A source is created waiting for approval, so being switched off is its
	// first state. Approving it is what an operator does, and what these
	// transitions start from.
	letOut(t, repo, s.ID)
	suspendSource(t, repo, s.ID)

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID on a suspended source: %v", err)
	}
	if got.IsActive {
		t.Error("still active after Deactivate")
	}
	if err := got.EnsureActive(); !errors.Is(err, source.ErrSourceInactive) {
		t.Errorf("EnsureActive() = %v, want ErrSourceInactive", err)
	}
}

func TestMissingSourceIsNotFound(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	_, err := postgres.NewSourceRepository(pool).ReadByID(context.Background(), ulid("ZZ"))
	if !errors.Is(err, source.ErrNotFound) {
		t.Fatalf("error = %v, want source.ErrNotFound", err)
	}
	if !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("error type is not ErrNotFound: %v", err)
	}
}

// The two statements that write a source both report "no such source" rather
// than succeeding silently. This was Update's test; Update is gone, and the
// question outlived it.
func TestWritingASourceThatIsNotThere(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	ghost := aSource(ulid("ZZ"))

	if err := repo.UpdateSettings(ctx, ghost); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("UpdateSettings = %v, want source.ErrNotFound", err)
	}
	if err := repo.UpdateReview(ctx, ghost); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("UpdateReview = %v, want source.ErrNotFound", err)
	}
}

// A customer sees their own and nobody else's. This is the whole of the
// ownership rule, and it is one WHERE clause -- so nothing above it has to
// remember to filter.
func TestOnlyTheOwnersSourcesComeBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	stranger := shared.ID("01K0ACCT0000000000000000AC")
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, role, is_active, created_at, updated_at)
		 VALUES ($1, 'them@acme.test', 'customer', true, now(), now())`, stranger.String())
	if err != nil {
		t.Fatalf("seed the stranger: %v", err)
	}

	repo := postgres.NewSourceRepository(pool)
	for _, c := range []struct {
		id    string
		owner shared.ID
	}{
		{ulid("SO1"), testOwner}, {ulid("SO2"), testOwner}, {ulid("SO3"), stranger},
	} {
		s := aSource(c.id)
		s.OwnerUserID = c.owner
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.ListByOwner(ctx, testOwner)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sources, want the owner's two", len(got))
	}
	for _, s := range got {
		if s.OwnerUserID != testOwner {
			t.Errorf("somebody else's source came back: %q", s.ID)
		}
	}
}

// An owner with nothing gets an empty list, not an error: having no sources yet
// is the ordinary state of a new account, and the page that renders it says so.
func TestAnOwnerWithNoSources(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	got, err := postgres.NewSourceRepository(pool).ListByOwner(context.Background(), testOwner)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d sources for an owner with none", len(got))
	}
}

// The portal's statement cannot reach the ceiling, and this is the assertion
// that holds even if every layer above it is rewritten: it hands
// UpdateSettings a source whose ceiling has been raised in memory, and reads
// the row back to find the stored one untouched.
func TestUpdateSettingsCannotCarryTheCeiling(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S7"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	letOut(t, repo, s.ID)

	// Everything a customer must not be able to move, moved in memory.
	s.Name = "acme-alerts"
	s.Description = "pages the on-call"
	s.MaxPriority = shared.PriorityCritical
	s.AllowCustomAddress = true
	s.IsActive = false
	s.UpdatedAt = time.Now().UTC()

	if err := repo.UpdateSettings(ctx, s); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}

	if got.Name != "acme-alerts" || got.Description != "pages the on-call" {
		t.Errorf("the settings did not land: %q / %q", got.Name, got.Description)
	}
	if got.MaxPriority == shared.PriorityCritical {
		t.Error("the statement carried max_priority")
	}
	if got.AllowCustomAddress {
		t.Error("the statement carried allow_custom_address")
	}
	if !got.IsActive {
		t.Error("the statement switched the source off")
	}
}

// The queue is what nobody has decided about. A refused source has been decided
// about, so it must not come back -- which is the entire reason reviewed_at
// exists as a column separate from approved_at.
func TestARefusedSourceLeavesTheQueue(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S8"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	queue, err := repo.ListForReview(ctx)
	if err != nil {
		t.Fatalf("ListForReview: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("a new source is not in the queue: %d rows", len(queue))
	}

	if err := s.Refuse("no working address", time.Now().UTC()); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	if err := repo.UpdateReview(ctx, s); err != nil {
		t.Fatalf("UpdateReview: %v", err)
	}

	queue, err = repo.ListForReview(ctx)
	if err != nil {
		t.Fatalf("ListForReview: %v", err)
	}
	if len(queue) != 0 {
		t.Errorf("a refused source is back in the queue: %d rows", len(queue))
	}

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if got.ReviewNote != "no working address" {
		t.Errorf("the reason did not survive: %q", got.ReviewNote)
	}
}

// The mirror of TestUpdateSettingsCannotCarryTheCeiling: a decision must not be
// able to rename somebody's source.
func TestUpdateReviewCannotRename(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	repo := postgres.NewSourceRepository(pool)
	ctx := context.Background()
	s := aSource(ulid("S9"))

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	before := s.Name
	s.Name = "renamed by an operator"
	s.Approve(time.Now().UTC())

	if err := repo.UpdateReview(ctx, s); err != nil {
		t.Fatalf("UpdateReview: %v", err)
	}

	got, err := repo.ReadByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if got.Name != before {
		t.Errorf("a decision renamed the source to %q", got.Name)
	}
	if !got.IsActive {
		t.Error("the approval did not land")
	}
}
