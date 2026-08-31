# Admin Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An operator can decide whether a source may send, undo that decision,
manage who is an operator, and see what happened to a customer's messages
without reading them.

**Architecture:** A second struct in `internal/adapter/api/web` with its own
engine and its own listener on `:8092`, and one new use case,
`usecase.Operators`, beside `Sources` and `Keys`. Refusal becomes a third state
on `sources` rather than an absence, so a turned-away source does not return to
the queue for ever.

**Tech Stack:** Go 1.26, gin, sqlc, goose, html/template, Postgres.

**Spec:** `docs/superpowers/specs/2026-08-30-admin-panel-design.md`

## Global Constraints

- The admin listener is `:8092`, private, never published — not in `ports:` and
  not on `dokploy-network`.
- The admin guard reads the role from the **live `users` row** on every request,
  never from the session or the cookie. `operator()` in `session.go` already
  does this; do not add a second path.
- `admin` does the day-to-day. Changing a role and deactivating an account are
  `super_admin` only, checked in the use case as well as on the route group.
- The operator's message view never selects `title` or `body`, and its type has
  no field for a raw address.
- A refusal requires a reason. An approval does not.
- Nothing is deleted, anywhere in this plan.
- The service has never been deployed: schema changes go into the migration that
  creates the table, not into new ones.
- `entity.go` declares exactly one type. Limits live in `const.go`.
- Every change goes through `usecase.Gate`.
- No `git commit` without a direct order from the user, and no `git push` unless
  their message is exactly `push kon`.

## File Structure

```
migrations/00003_create_sources.sql      reviewed_at, review_note, the queue index
migrations/00011_create_audit_log.sql    note

internal/core/domain/source/entity.go    Approve / Refuse / Suspend / Restore
internal/core/domain/source/port.go      UpdateReview, ListForReview, ListAll
internal/core/domain/user/port.go        List, UpdateRole, SetActive

internal/core/usecase/operator.go        Operators: the source decisions
internal/core/usecase/operator_people.go Operators: roles and accounts
internal/core/usecase/operator_read.go   Operators: messages, deliveries, audit
internal/core/usecase/gate.go            Act.Note -> AuditEntry.Note

internal/adapter/api/web/admin.go        AdminDeps, NewAdmin, the route table
internal/adapter/api/web/admin_review.go the queue and one source
internal/adapter/api/web/admin_people.go roles and accounts
internal/adapter/api/web/admin_audit.go  who did what
internal/adapter/api/web/admin_const.go  its paths, page names, form fields

public/templates/admin/                  layout + one file per page
public/static/admin/admin.css            its own stylesheet
```

Three `operator*.go` files rather than one, because one file carrying decisions,
people and reads would be the god-object the portal was already refactored out
of once.

---

### Task 1: A source can be refused

**Files:**
- Modify: `migrations/00003_create_sources.sql`
- Modify: `internal/core/domain/source/entity.go`, `const.go`, `port.go`
- Modify: `internal/adapter/db/postgres/queries/source.sql`, `source.go`
- Test: `internal/core/domain/source/service_test.go`, `internal/adapter/db/postgres/source_test.go`

**Interfaces:**
- Produces: `(*source.Source).Approve(now time.Time)`,
  `(*source.Source).Refuse(note string, now time.Time) error`,
  `(*source.Source).Suspend(now time.Time)`,
  `(*source.Source).Restore(now time.Time)`,
  `(*source.Source).IsReviewed() bool`,
  `source.Repository.UpdateReview(ctx, *Source) error`,
  `source.Repository.ListForReview(ctx) ([]Source, error)`,
  `source.Repository.ListAll(ctx) ([]Source, error)`

- [ ] **Step 1: the two columns, in the table that creates them**

In `migrations/00003_create_sources.sql`, after `approved_at`:

```sql
    -- When an operator last decided about this source, whichever way. NULL
    -- means nobody has looked at it yet, and that is exactly the review queue.
    --
    -- Distinct from approved_at, which records only the first yes. A refused
    -- source has this set and that null, which is what stops it coming back to
    -- the queue for ever and being decided again by somebody who cannot tell it
    -- from a stranger.
    reviewed_at          TIMESTAMPTZ,

    -- Why, in the operator's words, and the customer reads it. Overwritten by
    -- the next decision: this is the current decision, not a history. The
    -- history is audit_log.
    review_note          TEXT        NOT NULL DEFAULT '',
```

- [ ] **Step 2: the queue index stops being about approval**

Replace line 55 of the same file:

```sql
-- The review queue: what nobody has decided about, oldest first. Deliberately
-- NOT "where approved_at is null", which would also list everything ever
-- refused and hand an operator the same decision again every day.
CREATE INDEX sources_unreviewed_idx ON sources (created_at) WHERE reviewed_at IS NULL;
```

- [ ] **Step 3: run `make sqlc` and rebuild the dev database**

```bash
make sqlc
```

The dev database is already at version 11 with the old shape, and editing a
migration that has already run does not re-run it. Do NOT drop the database --
it holds a user and a source somebody is testing with. Bring its schema up to
the edited file by hand, leaving goose's marker where it is:

```bash
docker exec srosha-postgres-dev psql -U srosha -d srosha -c "
  ALTER TABLE sources ADD COLUMN reviewed_at TIMESTAMPTZ;
  ALTER TABLE sources ADD COLUMN review_note TEXT NOT NULL DEFAULT '';
  DROP INDEX IF EXISTS sources_unapproved_idx;
  CREATE INDEX sources_unreviewed_idx ON sources (created_at) WHERE reviewed_at IS NULL;"
```

Then prove there is no drift, because a hand-patched database is only as good as
the check that it matches:

```bash
docker exec srosha-postgres-dev psql -U srosha -d postgres -c "CREATE DATABASE srosha_scratch;"
goose -dir migrations postgres "<the dev url with /srosha_scratch>" up
```

Compare `information_schema.columns` for `sources` between the two databases;
they must be identical. Drop the scratch database afterwards. This is the same
move `docs/changes/2026-08-30-source-settings.md` records for the `description`
column, including the diff.

- [ ] **Step 4: write the failing entity tests**

In `internal/core/domain/source/service_test.go`:

```go
// A refused source is not a new one. Without a third fact they are the same
// row, and an operator is handed the same decision every day.
func TestARefusedSourceIsNotWaiting(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	if !src.IsReviewed() {
		t.Error("a refused source is still in the queue")
	}
	if src.IsApproved() {
		t.Error("refusing approved it")
	}
	if src.IsActive {
		t.Error("refusing left it able to send")
	}
	if src.ReviewNote != "no working address" {
		t.Errorf("note = %q", src.ReviewNote)
	}
}

// A refusal with no reason is the silent failure the column exists to prevent.
func TestARefusalNeedsAReason(t *testing.T) {
	src := waiting()

	if err := src.Refuse("   ", time.Now().UTC()); err == nil {
		t.Fatal("a source was refused with no reason")
	}
	if src.IsReviewed() {
		t.Error("the refusal was refused and applied anyway")
	}
}

// Approving after a refusal clears the note: the state is the current
// decision. What was said before lives in the audit log.
func TestApprovingClearsAnEarlierRefusal(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	if err := src.Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}
	src.Approve(at.Add(time.Hour))

	if src.ReviewNote != "" {
		t.Errorf("the old refusal is still on it: %q", src.ReviewNote)
	}
	if !src.IsActive || !src.IsApproved() {
		t.Error("approving did not let it send")
	}
}

// Suspending a source that was approved keeps approved_at, so the queue can
// still tell "turned away" from "worked once and was switched off".
func TestSuspendingKeepsTheApproval(t *testing.T) {
	src := waiting()
	at := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)

	src.Approve(at)
	src.Suspend(at.Add(time.Hour))

	if src.IsActive {
		t.Error("it can still send")
	}
	if !src.IsApproved() {
		t.Error("suspending forgot that it was ever approved")
	}
}
```

- [ ] **Step 5: run them, watch them fail**

```bash
go test ./internal/core/domain/source/ -run "TestARefused|TestARefusal|TestApprovingClears|TestSuspendingKeeps"
```

Expected: FAIL, `src.Refuse undefined`.

- [ ] **Step 6: the entity fields and methods**

In `entity.go`, beside `ApprovedAt`:

```go
	// ReviewedAt is when an operator last decided about this source, whichever
	// way they decided. Nil is the review queue.
	ReviewedAt *time.Time

	// ReviewNote is why, in the operator's words. The customer reads it, which
	// is the whole reason a refusal is not silent.
	ReviewNote string
```

and the four transitions:

```go
// Approve lets this source send. It is the only method here that turns
// IsActive on.
func (s *Source) Approve(now time.Time) {
	if s.ApprovedAt == nil {
		s.ApprovedAt = &now
	}
	s.ReviewedAt = &now
	s.ReviewNote = ""
	s.IsActive = true
	s.UpdatedAt = now
}

// Refuse turns a source away, with a reason the customer will read.
//
// The reason is required and the refusal does not happen without one: a source
// that silently never works is the failure this whole state exists to prevent.
func (s *Source) Refuse(note string, now time.Time) error {
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return errs.InvalidInputErr("a refusal needs a reason").WithErr(ErrNoReason)
	}
	if len(trimmed) > maxReviewNoteLen {
		return errs.InvalidInputErr("that reason is too long").
			WithErr(ErrNoReason).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), maxReviewNoteLen))
	}

	s.ReviewedAt = &now
	s.ReviewNote = trimmed
	s.IsActive = false
	s.UpdatedAt = now
	return nil
}

// Suspend stops a source that was working. ApprovedAt is left alone, so this
// stays distinguishable from a source that was turned away at the door.
func (s *Source) Suspend(now time.Time) {
	s.ReviewedAt = &now
	s.IsActive = false
	s.UpdatedAt = now
}

// Restore is the way back from Suspend.
func (s *Source) Restore(now time.Time) {
	s.ReviewedAt = &now
	s.ReviewNote = ""
	s.IsActive = true
	s.UpdatedAt = now
}

// IsReviewed reports whether an operator has ever decided about this source.
// It is the queue's question, and not the same one as IsApproved.
func (s *Source) IsReviewed() bool { return s.ReviewedAt != nil }
```

In `const.go`:

```go
// maxReviewNoteLen bounds an operator's reason. A sentence or two, because the
// customer reads it on a page and a refusal is not a support ticket.
const maxReviewNoteLen = 500
```

In `errors.go`, beside the others:

```go
var ErrNoReason = errors.New("source: a refusal needs a reason")
```

- [ ] **Step 7: run them, watch them pass**

- [ ] **Step 8: the queries**

Append to `internal/adapter/db/postgres/queries/source.sql`:

```sql
-- UpdateReview writes an operator's decision and nothing a customer owns. The
-- mirror of UpdateSourceSettings: that one cannot touch the switch, this one
-- cannot touch the name.
--
-- name: UpdateReview :execrows
UPDATE sources
SET is_active   = @is_active,
    approved_at = @approved_at,
    reviewed_at = @reviewed_at,
    review_note = @review_note,
    updated_at  = @updated_at::timestamptz
WHERE id = @id;

-- ListForReview is the queue: what nobody has decided about, oldest first,
-- because the person who has waited longest is the one to answer next.
--
-- name: ListForReview :many
SELECT * FROM sources WHERE reviewed_at IS NULL ORDER BY created_at;

-- ListAllSources is every source, newest first. No filter: the operator's page
-- filters in the handler, because the states are four and the counts are small.
--
-- name: ListAllSources :many
SELECT * FROM sources ORDER BY created_at DESC;
```

- [ ] **Step 9: the port and the adapter**

In `internal/core/domain/source/port.go`, on `Repository`:

```go
	// UpdateReview writes an operator's decision: the switch, the approval
	// record, and the reason. It cannot reach a customer's own columns, which
	// is the same promise UpdateSettings makes in the other direction.
	UpdateReview(ctx context.Context, s *Source) error

	// ListForReview is the queue. ListAll is everything, for the page that
	// filters.
	ListForReview(ctx context.Context) ([]Source, error)
	ListAll(ctx context.Context) ([]Source, error)
```

In `internal/adapter/db/postgres/source.go`, following `UpdateSettings` exactly:

```go
func (r *SourceRepository) UpdateReview(ctx context.Context, s *source.Source) error {
	rows, err := r.q(ctx).UpdateReview(ctx, gen.UpdateReviewParams{
		ID:         s.ID,
		IsActive:   s.IsActive,
		ApprovedAt: s.ApprovedAt,
		ReviewedAt: s.ReviewedAt,
		ReviewNote: s.ReviewNote,
		UpdatedAt:  s.UpdatedAt,
	})
	if err != nil {
		return failed("update review", err)
	}
	if rows == 0 {
		return errs.NotFoundErr("source not found").WithErr(source.ErrNotFound)
	}
	return nil
}

func (r *SourceRepository) ListForReview(ctx context.Context) ([]source.Source, error) {
	rows, err := r.q(ctx).ListForReview(ctx)
	if err != nil {
		return nil, failed("list for review", err)
	}
	return toSources(rows)
}

func (r *SourceRepository) ListAll(ctx context.Context) ([]source.Source, error) {
	rows, err := r.q(ctx).ListAllSources(ctx)
	if err != nil {
		return nil, failed("list all sources", err)
	}
	return toSources(rows)
}
```

`toSources` is a helper extracted from the existing `ListByOwner` loop; make
`ListByOwner` use it too rather than keeping two copies. Add `ReviewedAt` and
`ReviewNote` to `toSource`.

- [ ] **Step 10: the integration test**

In `internal/adapter/db/postgres/source_test.go`:

```go
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
```

- [ ] **Step 11: run everything, and the other fakes**

```bash
go build ./... && go test ./...
go test -tags=integration ./internal/adapter/db/postgres/
```

Every fake implementing `source.Repository` needs the three new methods:
`oneSource` in `internal/core/domain/source/service_test.go`, `fakeSources` in
`internal/core/usecase/fakes_test.go`, `memSources` in
`internal/adapter/api/web/portal_test.go`.

---

### Task 2: The audit log can say why

**Files:**
- Modify: `migrations/00011_create_audit_log.sql`
- Modify: `internal/core/usecase/gate.go`
- Modify: `internal/adapter/db/postgres/queries/audit.sql`, `audit.go`
- Test: `internal/core/usecase/gate_test.go`

**Interfaces:**
- Consumes: `usecase.Act`, `usecase.AuditEntry` from Task 1's tree (unchanged there).
- Produces: `usecase.Act` and `usecase.AuditEntry` both carrying `Note string`.

- [ ] **Step 1: the column**

In `migrations/00011_create_audit_log.sql`, after `target_id`:

```sql
    -- Why, when the verb does not say it on its own. A copy rather than a
    -- join, for the same reason actor_email is one: sources.review_note is
    -- overwritten by the next decision, so a year later the reason for the
    -- first refusal would be gone.
    note        TEXT        NOT NULL DEFAULT '',
```

- [ ] **Step 2: the failing test**

In `internal/core/usecase/gate_test.go`:

```go
// A verb and a target do not say why. The reason has to be on the row, because
// where it lives on the source is overwritten by the next decision.
func TestTheReasonReachesTheAuditRow(t *testing.T) {
	log := &auditLog{}
	gate := usecase.NewGate(log, seqIDs(), fixedNow(time.Now().UTC()))
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
```

`anOperator(t)` builds a `*user.User` with `user.RoleAdmin`; add it beside the
existing helpers if it is not already there.

- [ ] **Step 3: run it, watch it fail**

Expected: FAIL, `unknown field Note in struct literal of type usecase.Act`.

- [ ] **Step 4: the field, in both types**

In `gate.go`, on `Act`:

```go
	// Note is why, when the verb does not say it on its own -- a refusal's
	// reason, or what a role change was for. Empty is ordinary: approving needs
	// no justification.
	Note string
```

on `AuditEntry` the same field, and one line in `Gate.Do`:

```go
		Note:       act.Note,
```

- [ ] **Step 5: the query and the adapter**

Add `note` to the INSERT in `queries/audit.sql` and to the params in
`audit.go`'s `Record`. Run `make sqlc`.

- [ ] **Step 6: run the tests**

```bash
go test ./internal/core/usecase/ && go test -tags=integration ./internal/adapter/db/postgres/
```

---

### Task 3: The operator's decisions

**Files:**
- Create: `internal/core/usecase/operator.go`, `internal/core/usecase/operator_test.go`
- Modify: `internal/core/usecase/const.go`

**Interfaces:**
- Consumes: Task 1's `source.Repository` methods and `(*source.Source)`
  transitions; Task 2's `Act.Note`.
- Produces:
  `usecase.NewOperators(repo source.Repository, users user.Repository, gate *Gate, now shared.NowFunc) *Operators`,
  and on it:
  `Queue(ctx, actor) ([]source.Source, error)`,
  `AllSources(ctx, actor) ([]source.Source, error)`,
  `Source(ctx, actor, id string) (*source.Source, error)`,
  `Approve(ctx, actor, id string) error`,
  `Refuse(ctx, actor, id, note string) error`,
  `Suspend(ctx, actor, id, note string) error`,
  `Restore(ctx, actor, id string) error`

- [ ] **Step 1: the verbs**

In `internal/core/usecase/const.go`:

```go
	ActSourceApprove = "source.approve"
	ActSourceRefuse  = "source.refuse"
	ActSourceSuspend = "source.suspend"
	ActSourceRestore = "source.restore"
```

- [ ] **Step 2: the failing tests**

In `internal/core/usecase/operator_test.go`:

```go
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

	queue, err := rig.ops.Queue(ctx, rig.admin)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if len(queue) != 1 {
		t.Fatalf("the queue has %d sources, want 1", len(queue))
	}

	if err := rig.ops.Refuse(ctx, rig.admin, rig.sourceID, "no working address"); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	queue, err = rig.ops.Queue(ctx, rig.admin)
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
```

`newOperatorRig(t)` builds `customer`, `admin` and `superAdmin` users, a
`fakeSources` holding one source registered by the customer, a `fakeUsers`, an
`auditLog`, and `usecase.NewOperators(...)`. Model it on `newSourceRig` in
`source_test.go` and put it at the top of this file.

- [ ] **Step 3: run them, watch them fail**

- [ ] **Step 4: the use case**

`operator.go`. Every method starts by checking the role, and every change goes
through the gate:

```go
// Operators is what somebody who works here does to other people's sources.
//
// A separate type from Sources rather than more methods on it. Sources says
// what it is in its own first line -- what a customer does with the things they
// own -- and an operator is not that caller: it checks a role where the other
// checks ownership. One type serving both would mean every method knowing about
// two audiences, and that branch is where the mistake gets written.
type Operators struct {
	repo  source.Repository
	users user.Repository
	gate  *Gate
	now   shared.NowFunc
}

// mayOperate is the check every method here begins with.
//
// The route group has a guard too. This is not the same check twice: the guard
// keeps a page off somebody's screen, and this keeps the operation off the
// database whichever route reaches it.
func (o *Operators) mayOperate(actor *user.User) error {
	if actor == nil || !actor.Role.IsOperator() {
		return errs.ForbiddenErr("this is not yours to do").
			WithStr("not an operator")
	}
	return nil
}

func (o *Operators) Approve(ctx context.Context, actor *user.User, id string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	src.Approve(o.now())

	act := Act{Verb: ActSourceApprove, TargetType: "source", TargetID: src.ID}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

func (o *Operators) Refuse(ctx context.Context, actor *user.User, id, note string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	// Validated before the gate, so a refusal with no reason writes no audit
	// row for something that did not happen.
	if err := src.Refuse(note, o.now()); err != nil {
		return err
	}

	act := Act{
		Verb: ActSourceRefuse, TargetType: "source", TargetID: src.ID, Note: src.ReviewNote,
	}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

// forDecision is the role check and the read, together, so no method here can
// do one and forget the other.
func (o *Operators) forDecision(
	ctx context.Context, actor *user.User, id string,
) (*source.Source, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}
	return o.repo.ReadByID(ctx, id)
}
```

`Suspend` and `Restore` follow the same shape, with `ActSourceSuspend` and
`ActSourceRestore`. `Suspend` takes a note and passes it to the act; it does not
write it to the source, because a suspension is not a refusal and the customer's
page says something different for each. `Queue`, `AllSources` and `Source` check
the role and read.

- [ ] **Step 5: run them, watch them pass**

---

### Task 4: Roles and accounts, super_admin only

**Files:**
- Create: `internal/core/usecase/operator_people.go`, `operator_people_test.go`
- Modify: `internal/core/domain/user/port.go`, `entity.go`
- Modify: `internal/core/usecase/const.go`
- Modify: `internal/adapter/db/postgres/queries/user.sql`, `user.go`
- Test: `internal/adapter/db/postgres/user_test.go`

**Interfaces:**
- Produces: `user.Repository.List(ctx) ([]User, error)`,
  `user.Repository.UpdateRole(ctx, *User) error`,
  `user.Repository.SetActive(ctx, *User) error`,
  `(*user.User).ChangeRole(r Role, now time.Time) error`,
  and on `Operators`:
  `People(ctx, actor) ([]user.User, error)`,
  `Person(ctx, actor, id shared.ID) (*user.User, error)`,
  `SetRole(ctx, actor, id shared.ID, role user.Role, note string) error`,
  `SetPersonActive(ctx, actor, id shared.ID, on bool, note string) error`

- [ ] **Step 1: the verbs**

```go
	ActUserRole       = "user.role"
	ActUserDeactivate = "user.deactivate"
	ActUserActivate   = "user.activate"
```

- [ ] **Step 2: the failing tests**

```go
// An admin who could change roles could promote anybody, including themselves
// out of whatever bound they are under. This is the only thing super_admin
// means, and without it the value is a string nobody reads.
func TestAnAdminCannotChangeRoles(t *testing.T) {
	rig := newOperatorRig(t)

	err := rig.ops.SetRole(
		context.Background(), rig.admin, rig.customer.ID, user.RoleSuperAdmin, "promoting",
	)
	if err == nil {
		t.Fatal("an admin changed somebody's role")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused role change still wrote an audit row")
	}
}

func TestASuperAdminCanChangeRoles(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.customer.ID, user.RoleAdmin, "joins the team")
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}

	got, err := rig.ops.Person(ctx, rig.superAdmin, rig.customer.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if got.Role != user.RoleAdmin {
		t.Errorf("role = %q", got.Role)
	}
	if len(rig.log.entries) != 1 || rig.log.entries[0].Verb != usecase.ActUserRole {
		t.Errorf("audit = %+v", rig.log.entries)
	}
	if rig.log.entries[0].Note != "joins the team" {
		t.Errorf("note = %q", rig.log.entries[0].Note)
	}
}

// The last way in must not be closable. A super_admin removing their own role,
// or switching off their own account, locks everybody out of the panel with no
// way back except SQL.
func TestASuperAdminCannotDemoteThemselves(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.superAdmin.ID, user.RoleCustomer, "oops")
	if err == nil {
		t.Fatal("a super_admin demoted themselves")
	}

	err = rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.superAdmin.ID, false, "oops")
	if err == nil {
		t.Fatal("a super_admin switched off their own account")
	}
}
```

- [ ] **Step 3: run them, watch them fail**

- [ ] **Step 4: the domain, the port, the adapter**

`(*user.User).ChangeRole` validates the role and stamps `UpdatedAt`:

```go
// ChangeRole is the one field a person cannot set for themselves.
func (u *User) ChangeRole(r Role, now time.Time) error {
	if !r.Valid() {
		return errs.InvalidInputErr("unknown role").
			WithErr(ErrUnknownRole).
			WithStr(fmt.Sprintf("got %q", r))
	}
	u.Role = r
	u.UpdatedAt = now
	return nil
}
```

The three port methods, three queries (`ListUsers`, `UpdateUserRole`,
`SetUserActive`, each `:execrows` where it writes), and the adapter methods in
the shape the file already uses.

- [ ] **Step 5: `operator_people.go`**

`maySetPeople` is `mayOperate` plus `actor.Role == user.RoleSuperAdmin`, and
both `SetRole` and `SetPersonActive` refuse `id == actor.ID` before anything
else:

```go
// Nobody closes the last door behind themselves. A super_admin who demoted
// their own account, or switched it off, would leave the panel reachable only
// by an UPDATE run by hand -- which is the state this whole surface exists to
// end.
if id == actor.ID {
	return errs.InvalidInputErr("you cannot do this to your own account").
		WithStr("actor is the target")
}
```

- [ ] **Step 6: run them, and the adapter's own test**

Add to `internal/adapter/db/postgres/user_test.go` a test that a role written
comes back, and that `SetActive(false)` does not remove the row.

---

### Task 5: What an operator sees of a customer's messages

**Files:**
- Create: `internal/core/usecase/operator_read.go`, `operator_read_test.go`
- Modify: `internal/adapter/db/postgres/queries/notification.sql`, `notification.go`
- Modify: `internal/core/domain/notification/port.go`

There is no `internal/core/usecase/port.go` in this repository; the two types
below live in `operator_read.go` with the methods that return them.

**`delivery.sql` and `delivery.go` are NOT touched, and this is the point.**
`delivery.Repository.ListByNotificationID` already exists and already returns
the whole row. Deliveries carry no message content — `title` and `body` are on
`notifications`, not here — so the existing query is already safe to read, and
the only thing an operator must not see on a delivery is the raw `address`,
which the use case masks. Adding a second, narrower delivery query would be
duplication that guarantees nothing the type does not already guarantee.

**Interfaces:**
- Produces:
  ```go
  // OperatorMessage is a message as an operator may see it: when, on what, how
  // it went. There is no Title and no Body field, so a page that tried to show
  // one would not compile.
  type OperatorMessage struct {
      ID        string
      Channels  []string
      Failed    int
      Total     int
      CreatedAt time.Time
  }

  // OperatorDelivery carries MaskedAddress and no raw address, for the same
  // reason.
  type OperatorDelivery struct {
      ID            string
      Channel       string
      MaskedAddress string
      SenderName    string
      Status        string
      FailureReason string
      LastError     string
      Attempts      int
      CreatedAt     time.Time
      UpdatedAt     time.Time
  }
  ```
  and `Operators.Messages(ctx, actor, sourceID string) ([]OperatorMessage, error)`,
  `Operators.Deliveries(ctx, actor, messageID string) ([]OperatorDelivery, error)`

- [ ] **Step 1: the queries that cannot leak**

```sql
-- ListMessagesForOperator is the same two levels a customer's own query has,
-- with the content left out. title and body are not selected: a column that is
-- never read cannot be rendered by mistake.
--
-- LEFT JOIN, and count(d.id) rather than count(*): a message whose deliveries
-- were never written is exactly the one an operator is looking for, and an
-- inner join would hide it. array_remove strips the NULL that a message with no
-- deliveries would otherwise contribute to the channel list.
--
-- name: ListMessagesForOperator :many
SELECT n.id, n.created_at,
       array_remove(array_agg(DISTINCT d.channel), NULL)::text[] AS channels,
       count(d.id) FILTER (WHERE d.status = 'FAILED') AS failed,
       count(d.id) AS total
FROM notifications n
LEFT JOIN deliveries d ON d.notification_id = n.id
WHERE n.source_id = @source_id
GROUP BY n.id, n.created_at
ORDER BY n.created_at DESC
LIMIT @row_limit;
```

Deliveries are read through the EXISTING `delivery.Repository.ListByNotificationID`.
It returns `address` in the clear, and that is correct: masking is the core's
job, not the statement's. What stops the address reaching a page is that
`OperatorDelivery` has no field to put it in.

- [ ] **Step 2: the failing test**

```go
// The operator's view carries no message content. Asserted on what comes back
// rather than on the query, because the query is what a later edit changes.
func TestAnOperatorSeesNoMessageContent(t *testing.T) {
	rig := newOperatorRig(t)

	got, err := rig.ops.Deliveries(context.Background(), rig.admin, rig.messageID)
	if err != nil {
		t.Fatalf("Deliveries: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no deliveries came back")
	}

	if strings.Contains(got[0].MaskedAddress, "billing@acme.test") {
		t.Errorf("the full address came back: %q", got[0].MaskedAddress)
	}
	if !strings.Contains(got[0].MaskedAddress, "…") {
		t.Errorf("the address is not masked: %q", got[0].MaskedAddress)
	}
}
```

- [ ] **Step 3: masking, in the core**

```go
// mask keeps enough of an address to recognise and not enough to use.
//
// In the use case rather than in SQL: an adapter returns facts and the core
// decides, and how an address is shown to a person is a decision.
func mask(address string) string {
	const keep = 2
	if len(address) <= keep*2 {
		return "…"
	}
	return address[:keep] + "…" + address[len(address)-keep:]
}
```

- [ ] **Step 4: run it**

- [ ] **Step 5: and again on the page, in Task 7**

The struct having no field is one guarantee; the spec asks for the other, on
what is actually rendered. When Task 7's templates exist, add:

```go
// The struct cannot carry the body. This asserts on what a person actually
// sees, which is the thing the promise was about.
func TestTheOperatorsLogShowsNoMessageBody(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	got := get(t, a, "/sources/"+a.sourceID+"/log", cookie)
	whole(t, "/sources/"+a.sourceID+"/log", got)

	for _, secret := range []string{"the message body", "billing@acme.test"} {
		if strings.Contains(got.body, secret) {
			t.Errorf("the page carries %q", secret)
		}
	}
}
```

The fake behind `newTestAdmin` must seed a message whose body is literally
"the message body" and a delivery addressed to `billing@acme.test`, or the
test passes by having nothing to leak.

---

### Task 6: Reading the audit log

**Files:**
- Modify: `internal/adapter/db/postgres/queries/audit.sql`, `audit.go`
- Modify: `internal/core/usecase/gate.go`, `operator_read.go`
- Test: `internal/adapter/db/postgres/audit_test.go`

**Interfaces:**
- Produces: `usecase.AuditLog` gaining
  `List(ctx context.Context, limit int32) ([]AuditEntry, error)`, and
  `Operators.Audit(ctx, actor) ([]AuditEntry, error)`

- [ ] **Step 1: the query**

```sql
-- name: ListAudit :many
SELECT * FROM audit_log ORDER BY at DESC LIMIT @row_limit;
```

- [ ] **Step 2: widen the port**

`AuditLog` today has only `Record`. Add `List`. Every fake implementing it —
`auditLog` in `internal/core/usecase/fakes_test.go` and `memAudit` in
`internal/adapter/api/web/portal_test.go` — needs the method.

- [ ] **Step 3: the test**

In `audit_test.go`, assert that a recorded entry comes back from `List` with its
`Note` and `ActorEmail` intact.

- [ ] **Step 4: run it**

---

### Task 7: The admin surface

**Files:**
- Create: `internal/adapter/api/web/admin.go`, `admin_review.go`, `admin_people.go`, `admin_audit.go`, `admin_const.go`
- Create: `public/templates/admin/{layout,queue,sources,source,log,people,person,audit}.html`
- Create: `public/static/admin/admin.css`
- Modify: `internal/adapter/api/web/portal_const.go` (`surface` -> `surfacePortal`)
- Modify: `internal/adapter/api/web/session.go` (`superAdmin`)
- Test: `internal/adapter/api/web/admin_test.go`

**Interfaces:**
- Consumes: `Operators` from Tasks 3-6.
- Produces: `web.AdminDeps`, `web.NewAdmin(d AdminDeps) (http.Handler, error)`

- [ ] **Step 1: rename the surface constant**

`portal_const.go` has `surface = "portal"`. Two surfaces need two names:

```go
const (
	surfacePortal = "portal"
	surfaceAdmin  = "admin"
)
```

Move both to `const.go`, which is the file shared by the package, and update the
three uses in `portal.go`.

- [ ] **Step 2: the second role check on the guard**

In `session.go`, beside `operator`:

```go
// superAdmin is the rule for the two pages that change who somebody is.
//
// Read from the live row like operator, and for the same reason: taking the
// role away has to take effect on the next request rather than the next
// sign-in.
func superAdmin(u *user.User) bool { return u.Role == user.RoleSuperAdmin }
```

- [ ] **Step 3: the failing tests — the two the architecture demands**

```go
// docs/ARCHITECTURE.md names this and calls it not optional. It is what a
// fourth binary would have given for free, and this is the price of not
// building one: a route mounted on the wrong surface fails the build instead of
// shipping.
func TestNoAdminRouteAnswersOnThePortal(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	for _, path := range []string{"/queue", "/people", "/audit"} {
		if got := get(t, p, path, cookie); got.status != http.StatusNotFound {
			t.Errorf("the portal answers %s with %d", path, got.status)
		}
	}
}

// The other half: a customer's session is refused by the admin guard. Their
// cookie is valid and it reaches this listener, because a cookie is not scoped
// by port.
func TestTheAdminSurfaceRefusesACustomer(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "me@acme.test", user.RoleCustomer)

	for _, path := range []string{"/", "/sources", "/people", "/audit"} {
		got := get(t, a, path, cookie)
		if got.status == http.StatusOK {
			t.Errorf("a customer reached %s", path)
		}
	}
}

// An admin does the day-to-day and is refused on the two pages that change who
// somebody is.
func TestAnAdminIsRefusedOnThePeoplePages(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	if got := get(t, a, "/", cookie); got.status != http.StatusOK {
		t.Errorf("an admin cannot reach the queue: %d", got.status)
	}
	if got := get(t, a, "/people", cookie); got.status == http.StatusOK {
		t.Error("an admin reached /people")
	}
}
```

`newTestAdmin(t)` mirrors `newTestPortal(t)`, building `NewAdmin` over the same
in-memory fakes. `signedInAs` is `signedIn` with the person's role set on the
`memUsers` row before the cookie is taken.

- [ ] **Step 4: run them, watch them fail**

- [ ] **Step 5: `NewAdmin`**

Mirrors `NewPortal` exactly — its own render set over `surfaceAdmin`, its own
assets, its own engine, its own sessions:

```go
func NewAdmin(d AdminDeps) (http.Handler, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	pages, err := newPageRender(surfaceAdmin,
		pageSignIn, pageCode,
		pageQueue, pageAdminSources, pageAdminSource, pageAdminLog,
		pagePeople, pagePerson, pageAudit,
	)
	if err != nil {
		return nil, err
	}
	assets, err := browserFiles(surfaceAdmin)
	if err != nil {
		return nil, err
	}

	engine := newEngine(engineConfig{Debug: d.Debug, Render: pages, Log: d.Log})
	sessions := newSessions(d.SignIn, d.SecureCookie)

	in := &signInHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	account := &accountHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	review := &reviewHandler{ops: d.Operators, log: d.Log}
	people := &peopleHandler{ops: d.Operators, log: d.Log}
	audit := &auditHandler{ops: d.Operators, log: d.Log}

	engine.GET(pathSignIn, in.show)
	engine.POST(pathSignIn, in.request)
	engine.GET(pathCode, in.showCode)
	engine.POST(pathCode, in.verify)
	engine.POST(pathSignOut, account.signOut)

	// Named guarded, NOT inside: portal.go already declares a package-level
	// `var inside = chrome{SignedIn: true}`, and a local of that name would
	// shadow it for the whole function.
	guarded := engine.Group("", sessions.guard(operator, pathSignIn))
	guarded.GET(pathQueue, review.queue)
	guarded.GET(pathAdminSources, review.list)
	guarded.GET(pathAdminSource, review.show)
	guarded.POST(pathApprove, review.approve)
	guarded.POST(pathRefuse, review.refuse)
	guarded.POST(pathSuspend, review.suspend)
	guarded.POST(pathRestore, review.restore)
	guarded.GET(pathAdminLog, review.messages)
	guarded.GET(pathAudit, audit.show)

	// Two pages behind a second rule. A group of its own rather than a check
	// inside the handlers, so which pages need it is visible here.
	top := engine.Group("", sessions.guard(superAdmin, pathQueue))
	top.GET(pathPeople, people.list)
	top.GET(pathPerson, people.show)
	top.POST(pathPersonRole, people.setRole)
	top.POST(pathPersonActive, people.setActive)

	engine.StaticFS(pathStatic, http.FS(assets))
	return engine, nil
}
```

Note the admin's sign-in pages are its own. An operator signs in through the
same four steps, on this listener, so the panel does not depend on the public
one being reachable.

- [ ] **Step 6: the templates**

`public/templates/admin/layout.html` follows the portal's: one `{{define
"layout"}}`, a `chrome` embedded by every page. Its own navigation, and the
`/people` link shown only when the viewer is a `super_admin`. That needs a
second field, so this surface gets its OWN `adminChrome{SignedIn, SuperAdmin
bool}` and its own `var insideAdmin`. Do not widen `chrome`: every portal page
would gain a field it never uses and a portal template could branch on it.

Every page view model embeds it, **including the sign-in pages**. See
`docs/changes/2026-08-29-portal-navigation.md`: a page that leaves the field out
does not render a nav-less layout, it stops mid-tag with the error going
nowhere.

- [ ] **Step 7: run the tests, and assert the pages are whole**

Use the `whole()` helper from `portal_test.go` on every admin page.

- [ ] **Step 8: the guard reads the live row, and prove it**

The spec's last test, and the reason `operator()` reads the row instead of the
session:

```go
// Taking somebody's operator role takes effect on their next request, not their
// next sign-in. This is why the guard reads the users row rather than trusting
// the cookie, and without this test that is a comment.
func TestLosingTheRoleTakesEffectAtOnce(t *testing.T) {
	a := newTestAdmin(t)
	cookie := signedInAs(t, a, "ops@srosha.ir", user.RoleAdmin)

	if got := get(t, a, "/", cookie); got.status != http.StatusOK {
		t.Fatalf("an admin could not reach the queue: %d", got.status)
	}

	// The same person, demoted, holding the same cookie.
	a.users.setRole(t, "ops@srosha.ir", user.RoleCustomer)

	if got := get(t, a, "/", cookie); got.status == http.StatusOK {
		t.Error("a demoted operator still reached the queue with their old cookie")
	}
}
```

`memUsers.setRole` is a test helper that writes the role on the stored row.

---

### Task 8: The third listener

**Files:**
- Modify: `internal/bootstrap/console.go`, `internal/bootstrap/const.go`
- Modify: `internal/config/console.go`
- Modify: `.env.console.example`, `docs/CONFIG.md`

- [ ] **Step 1: the port**

`NOTIF_CONSOLE_ADMIN_PORT`, default `8092`. Add it to `.env.console.example` and
to `docs/CONFIG.md` in the same commit, per the repository's rule that every
piece of data lives in that file.

- [ ] **Step 2: build the second handler and serve it**

`buildConsoleCore` gains `operators`, and `console.go` starts a third server the
same way it starts the other two. The admin listener binds `127.0.0.1` by
default rather than `0.0.0.0`, because the one thing the architecture says about
it is that it is never published.

- [ ] **Step 3: `PortalDeps.validate`'s twin**

`AdminDeps.validate` refuses to build with a nil `Operators`, so a missing wire
stops the binary rather than serving a panel whose buttons do nothing.

- [ ] **Step 4: bring it up and look at it**

```bash
make run-console
```

Templates are compiled into the binary by `go:embed`, so editing HTML without
rebuilding changes nothing. Check `:8092` answers and `:8090` does not serve any
admin path.

---

### Task 9: The customer is told

**Files:**
- Modify: `public/templates/portal/source.html`
- Test: `internal/adapter/api/web/portal_test.go`

- [ ] **Step 1: the failing test**

```go
// A refusal a customer cannot read is a source that silently never works --
// the failure the "waiting for approval" message exists to avoid.
func TestARefusedSourceShowsItsReason(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "me@acme.test")

	post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	id := onlySourceID(t, p)

	at := time.Now().UTC()
	if err := p.sources.rows[0].Refuse("no working address", at); err != nil {
		t.Fatalf("Refuse: %v", err)
	}

	got := get(t, p, "/sources/"+id, cookie)
	whole(t, "/sources/"+id, got)

	if !strings.Contains(got.body, "no working address") {
		t.Error("the customer is not told why")
	}
	if strings.Contains(strings.ToLower(got.body), "waiting for approval") {
		t.Error("a refused source still says it is waiting")
	}
}
```

- [ ] **Step 2: the third branch on the page**

`source.html` today has two cases. It needs three, and the order matters —
refused is checked before waiting, because a refused source is also not
approved:

```html
{{if not .Source.IsActive}}
  {{if and .Source.IsReviewed (not .Source.IsApproved)}}
    <p class="problem" role="status">
      This source was not approved. {{.Source.ReviewNote}}
    </p>
  {{else if .Source.IsApproved}}
    <p class="problem" role="status">
      An operator switched this source off. Nothing it sends will go out.
    </p>
  {{else}}
    <p class="problem" role="status">
      Waiting for approval. Set it up now — it starts working the moment
      somebody here approves it.
    </p>
  {{end}}
{{end}}
```

- [ ] **Step 3: run it**

---

### Task 10: The report and the checks

- [ ] `make prepush`
- [ ] `go test -tags=integration ./internal/adapter/db/postgres/`
- [ ] One change report per commit, under `docs/changes/`, per the repository's
      rule. This branch carries several commits under one theme, so each brings
      its own report; the branch slug is `admin-panel`.
- [ ] Stop. Do not commit without a direct order, and do not push unless the
      user's message is exactly `push kon`.
