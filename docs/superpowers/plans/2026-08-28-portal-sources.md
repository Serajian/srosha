# Customer portal — sources, keys and identities

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A customer registers a source, configures it, issues a key — and an
operator approves it before it can send anything.

**Architecture:** The database already has almost all of it: `CreateSource`,
`UpdateSource`, `CreateAPIKey`, `ListAPIKeysBySource`, `RevokeAPIKey` and
`auth.Scheme.Mint` are written and nothing calls them. The work is in the core —
ports, service methods and use cases — and in the pages. Every mutating action
goes through `usecase.Gate`, which gets its first caller here.

**Tech Stack:** Go 1.26, pgx + sqlc, gin, `html/template`, goose migrations.

**Spec:** `docs/superpowers/specs/2026-08-28-customer-portal-design.md`

## Global Constraints

- **`docs/CONVENTIONS.md` and `docs/ARCHITECTURE.md` are binding.** Read both
  before the first edit.
- **A source is created inactive and sends nothing until an operator approves
  it.** `sources.is_active` is already the gate: `EnsureActive` is called inside
  `source/auth.go` `Authenticate` and inside `source/service.go` `Load`.
- **There are no plans, tiers or payment.** srosha behaves the same way with
  everybody. No rule here may lean on a future in which it does not.
- **A source with no credential of its own uses srosha's default identities.**
  `internal/adapter/sender/registry.go` is correct as written and this plan does
  not touch it.
- **Constants live in `const.go`**, one per package. Never inline a limit.
- **Comments are few and short.** Most declarations need none.
- **One adapter never imports another.** `make arch-check` enforces it.
- **Every error is built through `pkg/errs`.** Only `message` crosses the wire.
- **Every id is a ULID** — Crockford base32, which excludes **I, L, O and U**.
  The `ulid` domain refuses anything else. Check every literal id you write.
- **Nothing is deployed**, so a column folds into the migration that created the
  table rather than arriving as an ALTER.
- **`make prepush` must be green before every commit.**
- **Never `git commit` or `git push`** — the owner gives those orders.

### Scope

This plan ends when a customer can register a source, configure it, issue a key,
and see plainly that it is waiting for approval — and when an operator's `UPDATE`
makes it send. **The approval page is phase 2**, in the admin panel.

---

### Task 1: A source has an owner, and waits to be approved

Three columns and two fields. Folded into the migration that created the table,
because nothing is deployed.

**Files:**
- Modify: `migrations/00002_create_sources.sql`
- Modify: `internal/core/domain/source/entity.go`
- Modify: `internal/adapter/db/postgres/queries/source.sql`
- Modify: `internal/adapter/db/postgres/source.go`
- Test: `internal/core/domain/source/entity_test.go`

**Interfaces:**
- Consumes: `user.User` from the foundation plan
- Produces: `source.Source` gains `OwnerUserID shared.ID` and
  `ApprovedAt *time.Time`; `(*Source).IsApproved() bool`

- [ ] **Step 1: Add the columns**

In `migrations/00002_create_sources.sql`, inside `CREATE TABLE sources`, replace
the `is_active` line and add two columns:

```sql
    -- Who registered this. A customer sees their own sources and nobody
    -- else's, and this is the whole of how that is decided.
    owner_user_id        ulid        NOT NULL REFERENCES users (id),

    -- Whether this source may send. FALSE on creation: anybody may register a
    -- source, and nothing it registers reaches anybody until an operator says
    -- so. It is also the switch an operator uses later, which is why there is
    -- no second column for approval.
    is_active            BOOLEAN     NOT NULL DEFAULT FALSE,

    -- When it was first approved. A record, never a gate -- nothing reads this
    -- to decide anything. It exists so a review queue can ask for what has
    -- never been approved without also listing what somebody switched off last
    -- month.
    approved_at          TIMESTAMPTZ,
```

Then add the index the queue will read:

```sql
-- What a customer's own page reads, and what an operator's queue reads.
CREATE INDEX sources_owner_idx ON sources (owner_user_id, created_at DESC);
CREATE INDEX sources_unapproved_idx ON sources (created_at) WHERE approved_at IS NULL;
```

**The order has to change first.** `sources` is `00002` and `users` is `00008`,
so the foreign key above points at a table that does not exist yet. Nothing is
deployed, so renumber rather than adding an ALTER later:

```
00001_create_ulid_domain.sql      unchanged
00002_create_users.sql            was 00008
00003_create_sources.sql          was 00002   <- the columns above go here
00004_create_api_keys.sql         was 00003
00005_create_credentials.sql      was 00004
00006_create_webhooks.sql         was 00005
00007_create_notifications.sql    was 00006
00008_create_deliveries.sql       was 00007
00009_create_login_codes.sql      unchanged
00010_create_sessions.sql         unchanged
00011_create_audit_log.sql        unchanged
```

Rename the files with `git mv`; goose reads the number from the filename and
nothing inside them changes. Then `make migrate-reset && make migrate-up` and
check the order goose reports matches the list above.

- [ ] **Step 2: Write the failing test**

Append to `internal/core/domain/source/entity_test.go`:

```go
func TestANewSourceIsNotApprovedYet(t *testing.T) {
	s := &source.Source{ID: "01K0SRC0000000000000000000", IsActive: false}

	if s.IsApproved() {
		t.Error("a source nobody has approved says it is approved")
	}
	if err := s.EnsureActive(); err == nil {
		t.Error("a source nobody has approved may send")
	}
}

// approved_at is a record and not a gate: a source an operator switched off is
// still a source that was once approved, and the two questions have different
// answers.
func TestApprovalIsRememberedSeparatelyFromBeingOn(t *testing.T) {
	at := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	s := &source.Source{ID: "01K0SRC0000000000000000000", IsActive: false, ApprovedAt: &at}

	if !s.IsApproved() {
		t.Error("a source that was approved and later switched off forgot it was approved")
	}
	if err := s.EnsureActive(); err == nil {
		t.Error("a switched-off source may send")
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/domain/source/ -run "TestANewSource|TestApprovalIs"`
Expected: FAIL — `ApprovedAt` and `IsApproved` undefined.

- [ ] **Step 4: Add the fields**

In `internal/core/domain/source/entity.go`, inside `type Source struct`:

```go
	// OwnerUserID is who registered this. A customer sees their own sources and
	// nobody else's.
	OwnerUserID shared.ID

	// ApprovedAt is when an operator first let this source out. A record, never
	// a gate: IsActive is what decides, and this only tells a queue what it has
	// never looked at.
	ApprovedAt *time.Time
```

And below `EnsureActive`:

```go
// IsApproved reports whether an operator has ever let this source out. It is
// not the same question as EnsureActive: a source approved in March and
// switched off in August answers yes here and refuses there.
func (s *Source) IsApproved() bool { return s.ApprovedAt != nil }
```

- [ ] **Step 5: Carry them through the repository**

In `internal/adapter/db/postgres/queries/source.sql`, replace the comment above
`CreateSource` and the statement:

```sql
-- is_active is not given: a source is created switched OFF, and the column
-- default says so. Anybody may register one; nothing it registers reaches
-- anybody until an operator approves it.
--
-- exec, not one: the ports return only an error and nothing here is computed by
-- the database, so a returned row would be read by nobody.
--
-- name: CreateSource :exec
INSERT INTO sources (
    id, owner_user_id, name, max_priority, allow_custom_address,
    default_addresses, created_at, updated_at
) VALUES (
    @id, @owner_user_id, @name, @max_priority, @allow_custom_address,
    @default_addresses, @created_at, @created_at
);

-- ListSourcesByOwner is a customer's own page. Newest first, because the one
-- they just registered is the one they are looking for.
-- name: ListSourcesByOwner :many
SELECT * FROM sources WHERE owner_user_id = @owner_user_id ORDER BY created_at DESC;
```

Run `make sqlc`, then in `internal/adapter/db/postgres/source.go` add
`OwnerUserID` to the `CreateSource` params and to whatever maps a row to a
`source.Source`, and add `ApprovedAt` to the mapping.

- [ ] **Step 6: Run every source test**

Run: `go test ./internal/core/domain/source/ && go test -tags=integration ./internal/adapter/db/postgres/ -run TestASource`
Expected: PASS. Fix any existing test that assumed a new source is active — the
default changed, and that is the point.

- [ ] **Step 7: Apply and check the guard holds**

```bash
make migrate-reset && make migrate-up
docker exec srosha-postgres-dev psql -U srosha -d srosha -c \
  "INSERT INTO sources (id,owner_user_id,name,max_priority,created_at,updated_at)
   VALUES ('01K0SRC0000000000000000000','01K0ACCT0000000000000000ZZ','x','NORMAL',now(),now());"
```

Expected: refused by the **foreign key** — a source with no owner cannot exist.

**Read the error, do not just check that it failed.** Both ids above are valid
ULIDs, which is the point: Crockford base32 has no I, L, O or U, and an id with
one in it is refused by the `ulid` domain *before* the foreign key is ever
reached. A test written with such an id passes for the wrong reason and proves
nothing about the constraint it claims to check.

- [ ] **Step 8: `make prepush`, then stop**

Expected: green. **Do not commit** — the owner gives that order.

---

### Task 2: Configuring a source that is not approved yet

The order a customer works in, and the reason this task exists at all.

`source/service.go` `Load` calls `EnsureActive`, and both `Registrar.Register`
and `Credentials.Register` go through it. With a source created inactive, a
customer could register one and then be unable to give it a bot or a callback
until somebody approved it — and the operator would be approving an empty shell.

So the sending path and the managing path stop sharing a method.

**Files:**
- Modify: `internal/core/domain/source/service.go`
- Modify: `internal/core/usecase/register.go`
- Modify: `internal/core/usecase/credential.go`
- Test: `internal/core/domain/source/service_test.go`

**Interfaces:**
- Consumes: `source.Source` from Task 1
- Produces: `(*source.Service).Manage(ctx context.Context, id string) (*Source, error)`
  — reads the row and does **not** require it to be active. `Load` keeps
  `EnsureActive` and stays the sending path's.

- [ ] **Step 1: Write the failing test**

Append to `internal/core/domain/source/service_test.go`:

```go
// A customer configures a source before it is approved, not after. Otherwise
// registering one ends in waiting, and the operator approves a shell with
// nothing set up in it.
func TestAnUnapprovedSourceCanStillBeConfigured(t *testing.T) {
	repo := &fakeSourceRepo{src: &source.Source{
		ID: "01K0SRC0000000000000000000", Name: "acme", IsActive: false,
	}}
	svc := source.NewService(repo, allowAll{})

	if _, err := svc.Manage(context.Background(), "01K0SRC0000000000000000000"); err != nil {
		t.Fatalf("Manage: %v", err)
	}
}

// And it still may not send.
func TestAnUnapprovedSourceMayNotSend(t *testing.T) {
	repo := &fakeSourceRepo{src: &source.Source{
		ID: "01K0SRC0000000000000000000", Name: "acme", IsActive: false,
	}}
	svc := source.NewService(repo, allowAll{})

	if _, err := svc.Admit(context.Background(), "01K0SRC0000000000000000000"); err == nil {
		t.Fatal("Admit: an unapproved source was allowed to send")
	}
}
```

Reuse whatever fake and limiter the existing tests in that file already declare;
if there are none, write `fakeSourceRepo` with a `ReadByID` returning `src`, and
`allowAll` with an `Allow` returning `true, nil`.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/core/domain/source/ -run TestAnUnapproved`
Expected: FAIL — `Manage` undefined.

- [ ] **Step 3: Split the two paths**

In `internal/core/domain/source/service.go`, below `Load`:

```go
// Manage answers "who is this source" for a caller that is changing its
// configuration rather than sending.
//
// It deliberately does NOT require the source to be active. A source is created
// waiting for approval, and a customer sets it up in that window -- a bot, a
// callback, a key -- so that what an operator approves is a source that is
// ready rather than an empty one. A source an operator later switched off can
// still be reconfigured for the same reason: fixing whatever caused that is the
// only way back.
func (s *Service) Manage(ctx context.Context, id string) (*Source, error) {
	return s.repo.ReadByID(ctx, id)
}
```

- [ ] **Step 4: Move the management callers onto it**

In `internal/core/usecase/register.go`, in `Register`, `RotateSecret`, `Get`,
`Deactivate` and `Activate`, replace every `r.sources.Load(ctx, sourceID)` with
`r.sources.Manage(ctx, sourceID)`.

In `internal/core/usecase/credential.go`, do the same for every
`c.sources.Load(ctx, sourceID)`.

Do **not** touch `Admit`, and do not touch anything in `dispatch.go`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/...`
Expected: PASS. A test that asserted a webhook could not be registered on an
inactive source is now wrong and should be updated to assert the opposite, with
the reason above in its comment.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 3: A source can be created and listed

The port grows for the first time since it was written.

**Files:**
- Modify: `internal/core/domain/source/port.go`
- Modify: `internal/adapter/db/postgres/source.go`
- Test: `internal/adapter/db/postgres/source_test.go` *(build tag `integration`)*

**Interfaces:**
- Consumes: `source.Source` from Task 1
- Produces: `source.Repository` gains
  `Create(ctx context.Context, s *Source) error` and
  `ListByOwner(ctx context.Context, ownerID shared.ID) ([]Source, error)`

- [ ] **Step 1: Grow the port**

In `internal/core/domain/source/port.go`:

```go
// Repository is where sources are kept.
//
// ReadByID does not filter on is_active: a source waiting for approval must
// come back as a row so EnsureActive can say what is wrong with it, rather than
// as "no such source", which sends a customer looking for a typo in an id that
// is perfectly correct.
type Repository interface {
	Create(ctx context.Context, s *Source) error
	ReadByID(ctx context.Context, id string) (*Source, error)
	ListByOwner(ctx context.Context, ownerID shared.ID) ([]Source, error)
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/adapter/db/postgres/source_test.go`:

```go
func TestASourceIsCreatedWaitingForApproval(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	owner := aStoredUser(t, pool, "OWN", "ops@acme.test")
	repo := postgres.NewSourceRepository(pool)

	src := &source.Source{
		ID:               ulid("SR1"),
		OwnerUserID:      owner.ID,
		Name:             "acme-billing",
		MaxPriority:      shared.PriorityNormal,
		DefaultAddresses: map[shared.Channel]string{},
		CreatedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := repo.Create(ctx, src); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.ReadByID(ctx, src.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if got.IsActive {
		t.Error("a source was created able to send, with nobody having approved it")
	}
	if got.IsApproved() {
		t.Error("a source was created already approved")
	}
	if got.OwnerUserID != owner.ID {
		t.Errorf("owner = %q, want %q", got.OwnerUserID, owner.ID)
	}
}

// A customer sees their own and nobody else's. This is the whole of the
// ownership rule, and it is one WHERE clause.
func TestOnlyTheOwnersSourcesComeBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	mine := aStoredUser(t, pool, "MNE", "me@acme.test")
	theirs := aStoredUser(t, pool, "THR", "them@acme.test")
	repo := postgres.NewSourceRepository(pool)

	for _, c := range []struct {
		id    string
		owner shared.ID
	}{
		{ulid("SR2"), mine.ID}, {ulid("SR3"), mine.ID}, {ulid("SR4"), theirs.ID},
	} {
		err := repo.Create(ctx, &source.Source{
			ID: c.id, OwnerUserID: c.owner, Name: "s", MaxPriority: shared.PriorityNormal,
			DefaultAddresses: map[shared.Channel]string{},
			CreatedAt:        time.Now().UTC().Truncate(time.Microsecond),
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.ListByOwner(ctx, mine.ID)
	if err != nil {
		t.Fatalf("ListByOwner: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sources, want the owner's two", len(got))
	}
	for _, s := range got {
		if s.OwnerUserID != mine.ID {
			t.Errorf("somebody else's source came back: %q", s.ID)
		}
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run "TestASourceIsCreated|TestOnlyTheOwners"`
Expected: FAIL — `Create` and `ListByOwner` undefined on the repository.

- [ ] **Step 4: Write them**

In `internal/adapter/db/postgres/source.go`, following the shape of the methods
already there:

```go
func (r *SourceRepository) Create(ctx context.Context, s *source.Source) error {
	addresses, err := json.Marshal(s.DefaultAddresses)
	if err != nil {
		return errs.InternalErr("default addresses could not be stored").WithErr(err)
	}

	err = r.q(ctx).CreateSource(ctx, gen.CreateSourceParams{
		ID:                 s.ID,
		OwnerUserID:        s.OwnerUserID.String(),
		Name:               s.Name,
		MaxPriority:        s.MaxPriority.String(),
		AllowCustomAddress: s.AllowCustomAddress,
		DefaultAddresses:   addresses,
		CreatedAt:          s.CreatedAt,
	})
	if err != nil {
		return failed("create source", err)
	}
	return nil
}

func (r *SourceRepository) ListByOwner(
	ctx context.Context, ownerID shared.ID,
) ([]source.Source, error) {
	rows, err := r.q(ctx).ListSourcesByOwner(ctx, ownerID.String())
	if err != nil {
		return nil, failed("list sources by owner", err)
	}

	out := make([]source.Source, 0, len(rows))
	for _, row := range rows {
		s, err := toSource(row)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, nil
}
```

Use whatever the file already calls its row-to-entity mapper instead of
`toSource` if the name differs, and match how it already unmarshals
`default_addresses`.

- [ ] **Step 5: Run the tests**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run "TestASource|TestOnlyTheOwners" -v`
Expected: PASS.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 4: Registering a source, through the gate

The gate's first caller.

**Files:**
- Create: `internal/core/usecase/source.go`
- Create: `internal/core/usecase/source_test.go`
- Modify: `internal/core/usecase/const.go`

**Interfaces:**
- Consumes: `source.Repository` from Task 3, `usecase.Gate` and `usecase.Act`
- Produces: `usecase.Sources` with
  `Register(ctx context.Context, actor *user.User, reg SourceRegistration) (*source.Source, error)`,
  `Mine(ctx context.Context, actor *user.User) ([]source.Source, error)`,
  `One(ctx context.Context, actor *user.User, id string) (*source.Source, error)`;
  `usecase.SourceRegistration{Name string, DefaultAddresses map[shared.Channel]string}`

- [ ] **Step 1: Add the limits**

Append to `internal/core/usecase/const.go`:

```go
// MaxSourcesPerUser is a backstop, not a plan: srosha treats everybody the
// same, and this exists so one account cannot fill the table before anybody
// notices. Reaching it is worth a conversation, not an upgrade.
const MaxSourcesPerUser = 20

// Verbs, spelled once. They end up in audit_log and are read a year later.
const (
	ActSourceCreate = "source.create"
	ActKeyIssue     = "key.issue"
	ActKeyRevoke    = "key.revoke"
)
```

- [ ] **Step 2: Write the failing test**

`internal/core/usecase/source_test.go`:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

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
	if rig.log.entries[0].Verb != usecase.ActSourceCreate {
		t.Errorf("verb = %q", rig.log.entries[0].Verb)
	}
}

// A customer sees their own and nobody else's, and asking for somebody else's
// by id answers the same as asking for one that does not exist.
func TestSomebodyElsesSourceIsNotFound(t *testing.T) {
	rig := newSourceRig(t)

	theirs, err := user.New(shared.ID("01K0ACCT0000000000000000AC"), "them@acme.test",
		user.RoleCustomer, rig.at)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}

	mine, err := rig.sources.Register(context.Background(), rig.actor,
		usecase.SourceRegistration{Name: "mine"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := rig.sources.One(context.Background(), theirs, mine.ID); err == nil {
		t.Fatal("somebody read a source they do not own")
	} else if !errors.Is(err, source.ErrNotFound) {
		t.Errorf("One = %v, want ErrNotFound -- not a different answer that says it exists", err)
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
```

Write `newSourceRig` beside it with an in-memory `source.Repository` (a slice,
`Create` appending, `ReadByID` scanning, `ListByOwner` filtering), the `auditLog`
fake already in `gate_test.go`, `seqIDs()` and `fixedNow()` from
`fakes_test.go`, and an `actor` built with `user.New`.

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/usecase/ -run TestARegisteredSource`
Expected: FAIL — `usecase.Sources` undefined.

- [ ] **Step 4: Write the use case**

`internal/core/usecase/source.go`:

```go
package usecase

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// SourceRegistration is what a customer may set. What they may not is not in
// here: max_priority, allow_custom_address and is_active are ours, and a form
// that offered them would let a customer raise their own ceiling.
//
// The spec asks for a test that a registration cannot set those. There is none,
// because there is nothing to test: the fields are absent, so a handler that
// tried would not compile. That is a stronger guarantee than a test and it is
// the reason this type exists rather than taking a *source.Source.
type SourceRegistration struct {
	Name             string
	DefaultAddresses map[shared.Channel]string
}

// Sources is what a customer does with the things they own.
type Sources struct {
	repo  source.Repository
	gate  *Gate
	newID shared.IDFunc
	now   shared.NowFunc
}

func NewSources(
	repo source.Repository, gate *Gate, newID shared.IDFunc, now shared.NowFunc,
) *Sources {
	return &Sources{repo: repo, gate: gate, newID: newID, now: now}
}

// Register creates a source, switched off.
//
// Anybody may register one. Nothing it is given reaches anybody until an
// operator approves it, which is what replaced trying to tell a spammer from a
// customer by what they had configured.
func (s *Sources) Register(
	ctx context.Context, actor *user.User, reg SourceRegistration,
) (*source.Source, error) {
	mine, err := s.repo.ListByOwner(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	if len(mine) >= MaxSourcesPerUser {
		return nil, errs.TooManyErr("you have as many sources as one account may have").
			WithStr(fmt.Sprintf("user %q has %d", actor.ID, len(mine)))
	}

	built, err := source.New(s.newID().String(), actor.ID, reg.Name, reg.DefaultAddresses, s.now())
	if err != nil {
		return nil, err
	}

	act := Act{Verb: ActSourceCreate, TargetType: "source", TargetID: built.ID}
	err = s.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return s.repo.Create(ctx, built)
	})
	if err != nil {
		return nil, err
	}
	return built, nil
}

// Mine is everything this person registered.
func (s *Sources) Mine(ctx context.Context, actor *user.User) ([]source.Source, error) {
	return s.repo.ListByOwner(ctx, actor.ID)
}

// One is a source this person owns.
//
// Somebody else's answers ErrNotFound rather than a refusal, deliberately: a
// refusal would confirm that the id exists, and an id is guessable in a way a
// source's contents are not.
func (s *Sources) One(
	ctx context.Context, actor *user.User, id string,
) (*source.Source, error) {
	src, err := s.repo.ReadByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if src.OwnerUserID != actor.ID {
		return nil, errs.NotFoundErr("no such source").WithErr(source.ErrNotFound).
			WithStr(fmt.Sprintf("user %q asked for source %q, owned by %q",
				actor.ID, id, src.OwnerUserID))
	}
	return src, nil
}
```

- [ ] **Step 5: Add the constructor the use case needs**

`source.New` does not exist. In `internal/core/domain/source/entity.go`:

```go
// New builds a source, switched off. An operator decides when it may send, and
// nothing here can decide that for them.
func New(
	id string, owner shared.ID, name string,
	addresses map[shared.Channel]string, now time.Time,
) (*Source, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errs.InvalidInputErr("a source needs a name").WithErr(ErrEmptyName)
	}
	if len(trimmed) > maxNameLen {
		return nil, errs.InvalidInputErr("that name is too long").WithErr(ErrEmptyName).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), maxNameLen))
	}

	for channel, address := range addresses {
		if err := channel.ValidateAddress(address); err != nil {
			return nil, err
		}
	}

	return &Source{
		ID:          id,
		OwnerUserID: owner,
		Name:        trimmed,
		MaxPriority: shared.PriorityNormal,
		IsActive:    false,

		AllowCustomAddress: false,
		DefaultAddresses:   addresses,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}
```

`source` has `ErrNotFound` and nothing else of these. Add to
`internal/core/domain/source/const.go`:

```go
// maxNameLen is a bound of our own. A name only the customer sees, and anything
// near this is a paste that went wrong rather than a name.
const maxNameLen = 64
```

and to `internal/core/domain/source/errors.go`:

```go
	ErrEmptyName   = errors.New("a source needs a name")
	ErrKeyNotFound = errors.New("key not found")
```

`ErrKeyNotFound` is used by Task 5. `credential` has its own `ErrEmptyName` and
`maxNameLen`; these are a second pair in a second package, deliberately, because
the two names are different concepts that happen to have the same bound.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/core/usecase/ ./internal/core/domain/source/ -v`
Expected: PASS.

- [ ] **Step 7: `make prepush`, then stop**

---

### Task 5: Issuing and revoking keys

The port `KeyRepository`'s own comment promised this: "Issuing, listing and
revoking keys are the administrator's, and get their own port when there is an
administrator to call it." There is one now.

**Files:**
- Modify: `internal/core/domain/source/port.go`
- Create: `internal/core/usecase/key.go`
- Create: `internal/core/usecase/key_test.go`

**Interfaces:**
- Consumes: `usecase.Sources` from Task 4, `auth.Scheme.Mint`
- Produces: `source.KeyIssuer` with
  `Create(ctx context.Context, k *Key, keyHash string) error`,
  `ListBySourceID(ctx context.Context, sourceID string) ([]Key, error)`,
  `Revoke(ctx context.Context, keyID shared.ID, now time.Time) error`;
  `usecase.KeyMinter` with `Mint() (key, hash string, err error)`;
  `usecase.Keys` with
  `Issue(ctx, actor, sourceID, label string) (key string, k *source.Key, err error)`,
  `List(ctx, actor, sourceID string) ([]source.Key, error)`,
  `Revoke(ctx, actor, sourceID string, keyID shared.ID) error`

- [ ] **Step 1: Declare the port**

In `internal/core/domain/source/port.go`, below `KeyRepository`:

```go
// KeyIssuer is the other half of a key's life: making one, listing them and
// revoking one. It is separate from KeyRepository because that one runs on
// every request and this one runs when a person clicks something -- and a port
// that grew both would be faked in tests that care about neither.
type KeyIssuer interface {
	Create(ctx context.Context, k *Key, keyHash string) error
	ListBySourceID(ctx context.Context, sourceID string) ([]Key, error)
	Revoke(ctx context.Context, keyID shared.ID, now time.Time) error
}
```

`postgres.APIKeyRepository` already has all three with these signatures. Nothing
in the adapter changes.

- [ ] **Step 2: Write the failing test**

`internal/core/usecase/key_test.go`:

```go
package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/usecase"
)

// The key is returned once, from the call that made it. srosha keeps a hash,
// so there is no second chance and the page has to say so.
func TestAKeyIsHandedBackOnceAndOnlyStoredHashed(t *testing.T) {
	rig := newKeyRig(t)

	key, k, err := rig.keys.Issue(context.Background(), rig.actor, rig.sourceID, "laptop")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if key == "" {
		t.Fatal("no key came back")
	}
	if k.Label != "laptop" {
		t.Errorf("label = %q", k.Label)
	}

	for _, stored := range rig.issuer.hashes {
		if strings.Contains(stored, key) {
			t.Error("the key itself was stored, not a hash of it")
		}
	}
}

// Two at once is the whole reason keys are rows: rotation is issue the second,
// move, revoke the first, with no window where messages are refused.
func TestASourceMayHoldTwoKeys(t *testing.T) {
	rig := newKeyRig(t)
	ctx := context.Background()

	for _, label := range []string{"old", "new"} {
		if _, _, err := rig.keys.Issue(ctx, rig.actor, rig.sourceID, label); err != nil {
			t.Fatalf("Issue(%s): %v", label, err)
		}
	}

	got, err := rig.keys.List(ctx, rig.actor, rig.sourceID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2", len(got))
	}
}

// Ownership is checked on the source, not on the key: a key id says nothing
// about who may touch it.
func TestNobodyIssuesAKeyForSomebodyElsesSource(t *testing.T) {
	rig := newKeyRig(t)

	if _, _, err := rig.keys.Issue(
		context.Background(), rig.stranger, rig.sourceID, "theirs",
	); err == nil {
		t.Fatal("a key was issued for a source the actor does not own")
	}
	if len(rig.issuer.hashes) != 0 {
		t.Error("a key was written despite the refusal")
	}
}

func TestRevokingAKeyLeavesAnAuditRow(t *testing.T) {
	rig := newKeyRig(t)
	ctx := context.Background()

	_, k, err := rig.keys.Issue(ctx, rig.actor, rig.sourceID, "laptop")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	before := len(rig.log.entries)

	if err := rig.keys.Revoke(ctx, rig.actor, rig.sourceID, k.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if len(rig.log.entries) != before+1 {
		t.Fatalf("revocation wrote %d rows", len(rig.log.entries)-before)
	}
	if rig.log.entries[before].Verb != usecase.ActKeyRevoke {
		t.Errorf("verb = %q", rig.log.entries[before].Verb)
	}
}
```

Write `newKeyRig` with an in-memory `source.KeyIssuer` recording `hashes []string`,
a `mintN` stand-in for `usecase.KeyMinter` returning
`("srosha_sk_" + n, "hash_" + n, nil)`, the `Sources` use case from Task 4 over
an in-memory repository holding one source owned by `actor`, and a `stranger`
built with `user.New` and a different id.

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/usecase/ -run TestAKeyIsHandedBack`
Expected: FAIL — `usecase.Keys` undefined.

- [ ] **Step 4: Write the use case**

`internal/core/usecase/key.go`:

```go
package usecase

import (
	"context"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// KeyMinter makes a key and the hash it is found by.
//
// Declared here rather than imported: what a key looks like and how it is
// hashed is the adapter's business, and this layer only knows that one call
// produces both and that only the hash is kept.
type KeyMinter interface {
	Mint() (key, hash string, err error)
}

// Keys is issuing, listing and revoking a source's keys.
type Keys struct {
	issuer  source.KeyIssuer
	sources *Sources
	minter  KeyMinter
	gate    *Gate
	newID   shared.IDFunc
	now     shared.NowFunc
}

func NewKeys(
	issuer source.KeyIssuer, sources *Sources, minter KeyMinter,
	gate *Gate, newID shared.IDFunc, now shared.NowFunc,
) *Keys {
	return &Keys{
		issuer:  issuer,
		sources: sources,
		minter:  minter,
		gate:    gate,
		newID:   newID,
		now:     now,
	}
}

// Issue makes a key and hands it back once.
//
// The key is returned and never stored: what goes in the row is the hash it
// will be looked up by. There is no second chance to read it, which is why the
// page that calls this says so before the customer navigates away.
func (k *Keys) Issue(
	ctx context.Context, actor *user.User, sourceID, label string,
) (string, *source.Key, error) {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return "", nil, err
	}

	key, hash, err := k.minter.Mint()
	if err != nil {
		return "", nil, err
	}

	built := &source.Key{
		ID:        k.newID(),
		SourceID:  sourceID,
		Label:     label,
		CreatedAt: k.now(),
	}

	act := Act{Verb: ActKeyIssue, TargetType: "key", TargetID: built.ID.String()}
	err = k.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return k.issuer.Create(ctx, built, hash)
	})
	if err != nil {
		return "", nil, err
	}
	return key, built, nil
}

// List is a source's keys. The keys themselves are not in it and cannot be:
// only their hashes were kept.
func (k *Keys) List(
	ctx context.Context, actor *user.User, sourceID string,
) ([]source.Key, error) {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return nil, err
	}
	return k.issuer.ListBySourceID(ctx, sourceID)
}

// Revoke marks a key, and never deletes it. After an incident the questions are
// when it was revoked and when it was last used, and a deleted row answers
// neither.
func (k *Keys) Revoke(
	ctx context.Context, actor *user.User, sourceID string, keyID shared.ID,
) error {
	if _, err := k.sources.One(ctx, actor, sourceID); err != nil {
		return err
	}

	keys, err := k.issuer.ListBySourceID(ctx, sourceID)
	if err != nil {
		return err
	}
	if !holds(keys, keyID) {
		return errs.NotFoundErr("no such key").WithErr(source.ErrKeyNotFound)
	}

	act := Act{Verb: ActKeyRevoke, TargetType: "key", TargetID: keyID.String()}
	return k.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return k.issuer.Revoke(ctx, keyID, k.now())
	})
}

// holds is why Revoke lists first: a key id belongs to a source, and taking the
// caller's word for which one would let somebody revoke a key on a source they
// do not own by naming it.
func holds(keys []source.Key, id shared.ID) bool {
	for i := range keys {
		if keys[i].ID == id {
			return true
		}
	}
	return false
}
```

Add `ErrKeyNotFound` to `internal/core/domain/source/errors.go` if it is not
already there.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/usecase/ -v`
Expected: PASS.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 6: The source pages

Three pages: the list, the registration form, and one source.

**Files:**
- Create: `internal/adapter/api/web/portal_source.go`
- Create: `public/templates/portal/sources.html`
- Create: `public/templates/portal/source_new.html`
- Create: `public/templates/portal/source.html`
- Modify: `internal/adapter/api/web/portal.go`
- Modify: `internal/adapter/api/web/portal_const.go`
- Test: `internal/adapter/api/web/portal_test.go`

**Interfaces:**
- Consumes: `usecase.Sources` from Task 4
- Produces: `web.SourcePages` interface —
  `Register(ctx, *user.User, usecase.SourceRegistration) (*source.Source, error)`,
  `Mine(ctx, *user.User) ([]source.Source, error)`,
  `One(ctx, *user.User, string) (*source.Source, error)`;
  routes `GET /sources`, `POST /sources`, `GET /sources/new`, `GET /sources/{id}`

- [ ] **Step 1: Add the paths and page names**

Append to `internal/adapter/api/web/portal_const.go`:

```go
const (
	pathSources    = "/sources"
	pathSourceNew  = "/sources/new"
	pathSource     = "/sources/:id"
	pathSourceKeys = "/sources/:id/keys"
)

const (
	pageSources   = "sources"
	pageSourceNew = "source_new"
	pageSource    = "source"
)

const (
	fieldName    = "name"
	fieldChannel = "channel"
	fieldAddress = "address"
	fieldLabel   = "label"
)
```

`:id` is gin's parameter syntax, read with `c.Param("id")`.

- [ ] **Step 2: Write the failing test**

Append to `internal/adapter/api/web/portal_test.go`:

```go
// The one sentence this page exists to say. A customer who registers a source
// and is not told this discovers it from a send that failed, days later.
func TestANewSourceSaysItIsWaitingForApproval(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")

	made := post(t, p, "/sources", url.Values{"name": {"acme-billing"}}, cookie)
	if made.status != http.StatusSeeOther {
		t.Fatalf("POST /sources = %d", made.status)
	}

	list := get(t, p, "/sources", cookie)
	body := strings.ToLower(list.body)
	if !strings.Contains(body, "approval") && !strings.Contains(body, "approved") {
		t.Errorf("the source list does not say the source is waiting:\n%s", list.body)
	}
}

// Ownership, from the outside. Somebody else's source is not found, and the
// answer does not differ from an id that never existed.
func TestACustomerCannotOpenSomebodyElsesSource(t *testing.T) {
	p := newTestPortal(t)

	mine := signedIn(t, p, "me@acme.test")
	post(t, p, "/sources", url.Values{"name": {"mine"}}, mine)
	id := onlySourceID(t, p)

	theirs := signedIn(t, p, "them@acme.test")
	got := get(t, p, "/sources/"+id, theirs)
	missing := get(t, p, "/sources/01K0SRC0000000000000000000", theirs)

	if got.status != missing.status {
		t.Errorf("somebody else's source answered %d and a missing one %d -- "+
			"the difference says the id exists", got.status, missing.status)
	}
}

func TestTheSourceListNeedsASession(t *testing.T) {
	p := newTestPortal(t)

	if got := get(t, p, "/sources"); got.status != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to sign in", got.status)
	}
}
```

Write `signedIn(t, p, email)` as a helper that runs the existing sign-in flow
and returns the cookie, and `onlySourceID(t, p)` reading the id out of the
in-memory source repository the rig holds.

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/adapter/api/web/ -run TestANewSourceSays`
Expected: FAIL — 404, the routes do not exist.

- [ ] **Step 4: Write the handler**

`internal/adapter/api/web/portal_source.go`:

```go
package web

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/gin-gonic/gin"
)

// SourcePages is what this adapter needs from the core. usecase.Sources
// satisfies it.
type SourcePages interface {
	Register(
		ctx context.Context, actor *user.User, reg usecase.SourceRegistration,
	) (*source.Source, error)
	Mine(ctx context.Context, actor *user.User) ([]source.Source, error)
	One(ctx context.Context, actor *user.User, id string) (*source.Source, error)
}

type sourceHandler struct {
	sources SourcePages
	log     *slog.Logger
}

type sourceListPage struct {
	Sources []source.Source
	Problem string
}

type sourceNewPage struct {
	Name    string
	Problem string
}

type sourcePage struct {
	Source *source.Source
}

func (h *sourceHandler) list(c *gin.Context) {
	mine, err := h.sources.Mine(c.Request.Context(), signedInUser(c))
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not list sources", "error", err)
		c.HTML(http.StatusOK, pageSources, sourceListPage{Problem: message(err)})
		return
	}
	c.HTML(http.StatusOK, pageSources, sourceListPage{Sources: mine})
}

func (h *sourceHandler) showNew(c *gin.Context) {
	c.HTML(http.StatusOK, pageSourceNew, sourceNewPage{})
}

// create registers a source. It is switched off, and the page it redirects to
// says so.
func (h *sourceHandler) create(c *gin.Context) {
	name := formValue(h.log, c, fieldName)

	reg := usecase.SourceRegistration{
		Name:             name,
		DefaultAddresses: defaultAddresses(c),
	}

	if _, err := h.sources.Register(c.Request.Context(), signedInUser(c), reg); err != nil {
		h.log.WarnContext(c.Request.Context(), "source registration refused", "error", err)
		c.HTML(http.StatusOK, pageSourceNew, sourceNewPage{Name: name, Problem: message(err)})
		return
	}
	c.Redirect(http.StatusSeeOther, pathSources)
}

func (h *sourceHandler) show(c *gin.Context) {
	src, err := h.sources.One(c.Request.Context(), signedInUser(c), c.Param("id"))
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageSource, sourcePage{Source: src})
}

// defaultAddresses reads the repeated channel/address pairs the form posts. A
// pair with either half missing is dropped rather than half-stored, and a
// channel the service does not have is refused by the domain, not here.
func defaultAddresses(c *gin.Context) map[shared.Channel]string {
	channels := c.Request.PostForm[fieldChannel]
	addresses := c.Request.PostForm[fieldAddress]

	out := map[shared.Channel]string{}
	for i := range channels {
		if i >= len(addresses) {
			break
		}
		channel := strings.TrimSpace(channels[i])
		address := strings.TrimSpace(addresses[i])
		if channel == "" || address == "" {
			continue
		}
		out[shared.Channel(channel)] = address
	}
	return out
}

// notFound is the one answer for a source that is not there and for a source
// somebody else owns. Two answers would let anybody test ids.
func notFound(c *gin.Context) {
	c.String(http.StatusNotFound, "no such source")
}
```

- [ ] **Step 5: Mount the routes**

In `internal/adapter/api/web/portal.go`, add to `PortalDeps`:

```go
	Sources SourcePages
```

validate it as `SignIn` is validated, build the handler beside the others:

```go
	sources := &sourceHandler{sources: d.Sources, log: d.Log}
```

and mount, **inside the guarded group**:

```go
	authed.GET(pathSources, sources.list)
	authed.GET(pathSourceNew, sources.showNew)
	authed.POST(pathSources, sources.create)
	authed.GET(pathSource, sources.show)
```

Add `pageSources`, `pageSourceNew` and `pageSource` to the `newPageRender` call.

- [ ] **Step 6: Write the templates**

`public/templates/portal/sources.html`:

```html
{{define "content"}}
<div class="head-row">
  <h2 class="head">Your sources</h2>
  <a class="button" href="/sources/new">Register a source</a>
</div>

{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}

{{if not .Sources}}
  <div class="empty">
    <h3>Nothing sends yet</h3>
    <p>
      A source is one thing that sends — an app, a service, a single product of
      yours. Register one and it gets its own keys and its own senders.
    </p>
    <a class="button" href="/sources/new">Register your first source</a>
  </div>
{{else}}
  <ul class="cards">
    {{range .Sources}}
    <li class="card">
      <a class="nm" href="/sources/{{.ID}}">{{.Name}}</a>
      <span class="id">{{.ID}}</span>
      {{if .IsActive}}
        <span class="pill ok">Sending</span>
      {{else if .IsApproved}}
        <span class="pill off">Switched off</span>
        <p class="why">An operator switched this source off. Nothing it sends will go out.</p>
      {{else}}
        <span class="pill hold">Waiting for approval</span>
        <p class="why">
          Somebody here checks new sources before they can send. Set it up in the
          meantime — a sender and a callback — and it will start working the
          moment it is approved.
        </p>
      {{end}}
    </li>
    {{end}}
  </ul>
{{end}}
{{end}}
```

`public/templates/portal/source_new.html`:

```html
{{define "content"}}
<h2 class="head">Register a source</h2>
<p class="sub">
  One thing that sends. It will not send anything until somebody here approves
  it, which usually takes a little while.
</p>

{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}

<form method="post" action="/sources">
  <label for="name">Name</label>
  <input id="name" name="name" type="text" value="{{.Name}}"
         placeholder="acme-billing" autofocus required>
  <p class="hint">Lowercase letters, digits and dashes. Only you ever see it.</p>

  <label for="address">Where it sends by default <span class="optional">— optional</span></label>
  <div class="pair">
    <select id="channel" name="channel" aria-label="Channel">
      <option value="">Channel</option>
      <option value="email">email</option>
      <option value="telegram">telegram</option>
      <option value="bale">bale</option>
      <option value="whatsapp">whatsapp</option>
      <option value="matrix">matrix</option>
      <option value="fcm">fcm</option>
      <option value="apns">apns</option>
    </select>
    <input id="address" name="address" type="text" placeholder="billing@acme.test">
  </div>
  <p class="hint">Where this source sends when a message doesn't name anyone.</p>

  <button type="submit">Register</button>
</form>
{{end}}
```

`public/templates/portal/source.html`:

```html
{{define "content"}}
<h2 class="head">{{.Source.Name}}</h2>
<p class="sub"><span class="mono">{{.Source.ID}}</span></p>

{{if not .Source.IsActive}}
  <p class="problem" role="status">
    {{if .Source.IsApproved}}
      An operator switched this source off. Nothing it sends will go out.
    {{else}}
      Waiting for approval. Set it up now — it starts working the moment
      somebody here approves it.
    {{end}}
  </p>
{{end}}

<dl class="facts">
  <dt>Registered</dt><dd class="mono">{{.Source.CreatedAt.Format "2 Jan 2006"}}</dd>
  <dt>Highest priority</dt><dd class="mono">{{.Source.MaxPriority}}</dd>
</dl>

<p class="fine"><a href="/sources/{{.Source.ID}}/keys">Keys</a></p>
{{end}}
```

- [ ] **Step 7: Add the styles the new pages use**

In `public/static/portal/portal.css`, add `.head-row`, `.button`, `.cards`,
`.card`, `.pill`, `.why`, `.hint`, `.optional`, `.pair`, `select` and `.empty`,
using the tokens already at the top of that file and no new colours. `.pill.hold`
uses `--gold`; `.pill.ok` uses `--action`; `.pill.off` uses `--muted`.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/adapter/api/web/ -v`
Expected: PASS.

- [ ] **Step 9: `make prepush`, then stop**

---

### Task 7: The keys page, and the one time a key is shown

**Files:**
- Create: `internal/adapter/api/web/portal_key.go`
- Create: `public/templates/portal/keys.html`
- Create: `public/templates/portal/key_issued.html`
- Modify: `internal/adapter/api/web/portal.go`
- Test: `internal/adapter/api/web/portal_test.go`

**Interfaces:**
- Consumes: `usecase.Keys` from Task 5
- Produces: `web.KeyPages` interface —
  `Issue(ctx, *user.User, sourceID, label string) (string, *source.Key, error)`,
  `List(ctx, *user.User, sourceID string) ([]source.Key, error)`,
  `Revoke(ctx, *user.User, sourceID string, keyID shared.ID) error`;
  routes `GET /sources/:id/keys`, `POST /sources/:id/keys`,
  `POST /sources/:id/keys/:keyID/revoke`

- [ ] **Step 1: Write the failing test**

Append to `internal/adapter/api/web/portal_test.go`:

```go
// The key is shown once, on the page that made it, and never again. A page that
// showed it twice would mean srosha had kept it.
func TestAKeyIsShownOnceAndNeverAgain(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")
	post(t, p, "/sources", url.Values{"name": {"acme"}}, cookie)
	id := onlySourceID(t, p)

	issued := post(t, p, "/sources/"+id+"/keys", url.Values{"label": {"laptop"}}, cookie)
	if issued.status != http.StatusOK {
		t.Fatalf("POST keys = %d", issued.status)
	}

	key := theKeyOn(t, issued.body)
	if key == "" {
		t.Fatal("the page that issues a key does not show it")
	}

	again := get(t, p, "/sources/"+id+"/keys", cookie)
	if strings.Contains(again.body, key) {
		t.Error("the key is on the list page, so it was kept somewhere")
	}
}

func TestRevokingAKeyTakesItOutOfUse(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")
	post(t, p, "/sources", url.Values{"name": {"acme"}}, cookie)
	id := onlySourceID(t, p)

	post(t, p, "/sources/"+id+"/keys", url.Values{"label": {"laptop"}}, cookie)
	keyID := onlyKeyID(t, p)

	res := post(t, p, "/sources/"+id+"/keys/"+keyID+"/revoke", url.Values{}, cookie)
	if res.status != http.StatusSeeOther {
		t.Fatalf("revoke = %d", res.status)
	}

	list := get(t, p, "/sources/"+id+"/keys", cookie)
	if !strings.Contains(strings.ToLower(list.body), "revoked") {
		t.Error("the list does not show the key as revoked")
	}
}
```

`theKeyOn` finds the key in the body by its prefix; `onlyKeyID` reads it from the
rig's in-memory issuer.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/adapter/api/web/ -run TestAKeyIsShownOnce`
Expected: FAIL — 404.

- [ ] **Step 3: Write the handler**

`internal/adapter/api/web/portal_key.go`:

```go
package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/gin-gonic/gin"
)

// KeyPages is what this adapter needs from the core. usecase.Keys satisfies it.
type KeyPages interface {
	Issue(
		ctx context.Context, actor *user.User, sourceID, label string,
	) (string, *source.Key, error)
	List(ctx context.Context, actor *user.User, sourceID string) ([]source.Key, error)
	Revoke(
		ctx context.Context, actor *user.User, sourceID string, keyID shared.ID,
	) error
}

type keyHandler struct {
	keys KeyPages
	log  *slog.Logger
}

type keysPage struct {
	SourceID string
	Keys     []source.Key
	Problem  string
}

// keyIssuedPage carries the key itself, exactly once. Nothing stores it and
// nothing can render it again.
type keyIssuedPage struct {
	SourceID string
	Key      string
	Label    string
}

func (h *keyHandler) list(c *gin.Context) {
	id := c.Param("id")

	keys, err := h.keys.List(c.Request.Context(), signedInUser(c), id)
	if err != nil {
		notFound(c)
		return
	}
	c.HTML(http.StatusOK, pageKeys, keysPage{SourceID: id, Keys: keys})
}

// issue renders the key straight into the response and keeps it nowhere. There
// is no redirect afterwards on purpose: a redirect would need somewhere to put
// the key in the meantime, and every such place outlives the page.
func (h *keyHandler) issue(c *gin.Context) {
	id := c.Param("id")
	label := formValue(h.log, c, fieldLabel)

	key, made, err := h.keys.Issue(c.Request.Context(), signedInUser(c), id, label)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "key not issued", "error", err)
		keys, listErr := h.keys.List(c.Request.Context(), signedInUser(c), id)
		if listErr != nil {
			notFound(c)
			return
		}
		c.HTML(http.StatusOK, pageKeys, keysPage{SourceID: id, Keys: keys, Problem: message(err)})
		return
	}

	c.HTML(http.StatusOK, pageKeyIssued, keyIssuedPage{
		SourceID: id, Key: key, Label: made.Label,
	})
}

func (h *keyHandler) revoke(c *gin.Context) {
	id := c.Param("id")

	err := h.keys.Revoke(
		c.Request.Context(), signedInUser(c), id, shared.ID(c.Param("keyID")),
	)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "key not revoked", "error", err)
	}
	c.Redirect(http.StatusSeeOther, pathSources+"/"+id+"/keys")
}
```

Add to `portal_const.go`:

```go
const (
	pathKeyRevoke = "/sources/:id/keys/:keyID/revoke"
)

const (
	pageKeys      = "keys"
	pageKeyIssued = "key_issued"
)
```

- [ ] **Step 4: Mount and template**

In `portal.go` add `Keys KeyPages` to `PortalDeps`, validate it, build
`keys := &keyHandler{keys: d.Keys, log: d.Log}`, add `pageKeys` and
`pageKeyIssued` to `newPageRender`, and mount inside `authed`:

```go
	authed.GET(pathSourceKeys, keys.list)
	authed.POST(pathSourceKeys, keys.issue)
	authed.POST(pathKeyRevoke, keys.revoke)
```

`public/templates/portal/keys.html`:

```html
{{define "content"}}
<h2 class="head">Keys</h2>
<p class="sub">Your code sends one of these with every message.</p>

{{if .Problem}}<p class="problem" role="alert">{{.Problem}}</p>{{end}}

<form method="post" action="/sources/{{.SourceID}}/keys">
  <label for="label">What is this key for?</label>
  <input id="label" name="label" type="text" placeholder="production" required>
  <p class="hint">A name only you see. It is how you tell them apart when you rotate.</p>
  <button type="submit">Issue a key</button>
</form>

{{if .Keys}}
<ul class="cards">
  {{range .Keys}}
  <li class="card">
    <span class="nm">{{.Label}}</span>
    <span class="id">{{.ID}}</span>
    {{if .RevokedAt}}
      <span class="pill off">Revoked</span>
    {{else}}
      <span class="pill ok">In use</span>
      <form method="post" action="/sources/{{$.SourceID}}/keys/{{.ID}}/revoke">
        <button type="submit" class="quiet">Revoke</button>
      </form>
    {{end}}
  </li>
  {{end}}
</ul>
{{end}}
{{end}}
```

`public/templates/portal/key_issued.html`:

```html
{{define "content"}}
<h2 class="head">Your key</h2>
<p class="sub">Copy it now. This is the only time it is shown.</p>

<div class="reveal">
  <code class="k">{{.Key}}</code>
  <span class="cap">{{.Label}}</span>
</div>

<p class="hint">
  Put it where your code reads secrets. srosha stores only a hash of it — if you
  lose it, revoke it and issue another.
</p>

<p class="fine"><a href="/sources/{{.SourceID}}/keys">I've saved it</a></p>
{{end}}
```

Add `.reveal`, `.reveal .k` and `.cap` to `portal.css`. **`.reveal .k` is the
only place `--gold` appears on this page**, and it must be the only gold on it.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/adapter/api/web/ -v`
Expected: PASS.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 8: Senders and the callback

Two pages over two use cases that already exist and are already tested.

**Files:**
- Create: `internal/adapter/api/web/portal_identity.go`
- Create: `public/templates/portal/senders.html`
- Create: `public/templates/portal/callback.html`
- Create: `public/templates/portal/callback_secret.html`
- Modify: `internal/adapter/api/web/portal.go`
- Test: `internal/adapter/api/web/portal_test.go`

**Interfaces:**
- Consumes: `usecase.Credentials` and `usecase.Registrar`, both unchanged
- Produces: `web.SenderPages` and `web.CallbackPages`; routes
  `GET|POST /sources/:id/senders`, `GET|POST /sources/:id/callback`

- [ ] **Step 1: Write the failing test**

Append to `internal/adapter/api/web/portal_test.go`:

```go
// A source is configured while it waits, not after. This is the whole reason
// source.Service.Manage exists.
func TestASourceCanBeConfiguredBeforeItIsApproved(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")
	post(t, p, "/sources", url.Values{"name": {"acme"}}, cookie)
	id := onlySourceID(t, p)

	res := post(t, p, "/sources/"+id+"/callback",
		url.Values{"url": {"https://acme.test/hooks/srosha"}}, cookie)

	if res.status != http.StatusOK && res.status != http.StatusSeeOther {
		t.Fatalf("callback on an unapproved source = %d", res.status)
	}
	if strings.Contains(strings.ToLower(res.body), "not active") {
		t.Error("configuring an unapproved source was refused for being unapproved")
	}
}

// The signing secret is handed over once, like a key.
func TestTheCallbackSecretIsShownOnce(t *testing.T) {
	p := newTestPortal(t)
	cookie := signedIn(t, p, "a@acme.test")
	post(t, p, "/sources", url.Values{"name": {"acme"}}, cookie)
	id := onlySourceID(t, p)

	made := post(t, p, "/sources/"+id+"/callback",
		url.Values{"url": {"https://acme.test/hooks/srosha"}}, cookie)
	secret := theSecretOn(t, made.body)
	if secret == "" {
		t.Fatal("registering a callback did not show its signing secret")
	}

	again := get(t, p, "/sources/"+id+"/callback", cookie)
	if strings.Contains(again.body, secret) {
		t.Error("the secret is on the page a second time, so it was kept")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/adapter/api/web/ -run TestASourceCanBeConfigured`
Expected: FAIL — 404.

- [ ] **Step 3: Write the handler**

`internal/adapter/api/web/portal_identity.go` declares:

```go
// SenderPages is a source's own identities: its bot, its mail account, its
// signing key. usecase.Credentials satisfies it.
type SenderPages interface {
	Register(
		ctx context.Context, sourceID string, reg usecase.CredentialRegistration,
	) (*credential.Credential, error)
	List(ctx context.Context, sourceID string) ([]credential.Credential, error)
}

// CallbackPages is where a source is told what happened. usecase.Registrar
// satisfies it.
type CallbackPages interface {
	Register(
		ctx context.Context, sourceID string, reg webhook.Registration,
	) (*webhook.Webhook, string, error)
	Get(ctx context.Context, sourceID string) (*webhook.Webhook, error)
}
```

and an `identityHandler` with `showSenders`, `addSender`, `showCallback` and
`setCallback`, each of which:

1. calls `h.sources.One(ctx, signedInUser(c), c.Param("id"))` **first** — the
   ownership check, because these two use cases take a source id and check
   nothing about who is asking;
2. then calls the use case;
3. renders `pageCallbackSecret` with the secret exactly once, the way
   `keyHandler.issue` renders a key.

Give `identityHandler` a `sources SourcePages` field for step 1.

- [ ] **Step 4: Templates**

`senders.html` lists the source's credentials and offers a form per channel;
`callback.html` shows the current callback URL and a form to set it;
`callback_secret.html` is `key_issued.html`'s shape with the secret in the gold
slot and this text:

```html
<p class="sub">Copy it now. This is the only time it is shown.</p>
...
<p class="hint">
  Every callback srosha sends you is signed with this. Check the signature
  before you trust the body — that is what it is for.
</p>
```

- [ ] **Step 5: Mount, run, and check the gold rule**

Run: `go test ./internal/adapter/api/web/ -v`
Expected: PASS.

Then read the three templates that carry a secret and confirm each has **exactly
one** gold element: `key_issued.html`, `callback_secret.html` and the code cells
in `code.html`. Any other use of `--gold` is a bug.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 9: Wiring

**Files:**
- Modify: `internal/bootstrap/console.go`
- Modify: `internal/adapter/api/web/portal.go`

**Interfaces:**
- Consumes: everything above
- Produces: a console that serves the whole surface

- [ ] **Step 1: Build the new core in bootstrap**

In `buildConsoleCore`, beside the sign-in use case:

```go
	sourceRows := postgres.NewSourceRepository(pool)
	keyRows := postgres.NewAPIKeyRepository(pool)

	gate := usecase.NewGate(postgres.NewAuditRepository(pool), ids.Generate, now)

	sources := usecase.NewSources(sourceRows, gate, ids.Generate, now)
	keys := usecase.NewKeys(keyRows, sources, auth.NewScheme(), gate, ids.Generate, now)
```

`auth.NewScheme()` returns a `Scheme` whose `Mint` matches `usecase.KeyMinter`.

Credentials and the registrar need the same construction the gateway already
does in `internal/bootstrap/gateway.go` — copy it, including the keyring and the
secret keeper, and note that the console now needs `settings.Crypto`. Add
`Crypto settings.Crypto` to `config.Console` and load it with
`settings.LoadCrypto(r)`.

- [ ] **Step 2: Hand them to the portal**

```go
	pages, err := web.NewPortal(web.PortalDeps{
		SignIn:       signIn,
		Sources:      sources,
		Keys:         keys,
		Senders:      credentials,
		Callbacks:    registrar,
		SecureCookie: cfg.Console.SecureCookie,
		Debug:        !cfg.App.IsProduction(),
		Log:          log,
	})
```

- [ ] **Step 3: Run it against a real database**

```bash
make dev-up && make migrate-up
make run-console
```

Sign in, register a source, and read the page. Expected: it says the source is
waiting for approval.

Issue a key, and confirm the key is on that page and on no other.

Then approve it by hand, which is what phase 2 replaces:

```bash
docker exec srosha-postgres-dev psql -U srosha -d srosha -c \
  "UPDATE sources SET is_active = true, approved_at = now()
   WHERE name = 'acme-billing';"
```

Reload the source page. Expected: it now says the source is sending.

- [ ] **Step 4: Check the audit log has what it promised**

```bash
docker exec srosha-postgres-dev psql -U srosha -d srosha -c \
  "SELECT at, actor_email, verb, target_type FROM audit_log ORDER BY at;"
```

Expected: one `source.create` and one `key.issue`, both naming the address you
signed in with. This is the first time that table has had rows in it.

- [ ] **Step 5: `make prepush`, then stop**

---

### Task 10: The one thing that must not have broken

A source that has registered no credential of its own still sends as srosha.
That is the behaviour the spec now depends on, and nothing in this plan touched
the code that does it — which is exactly why it is worth a test.

**Files:**
- Test: `internal/adapter/sender/registry_test.go`

**Interfaces:**
- Consumes: `internal/adapter/sender.Registry`, unchanged

- [ ] **Step 1: Write the test**

```go
// A source with no credential of its own falls back to srosha's identities.
// This is not a leftover: it is what makes a first message possible, and the
// approval step is what makes it safe.
func TestASourceWithNothingConfiguredSendsAsSrosha(t *testing.T) {
	reg := registryWithOwnEmail(t)

	sender, err := reg.For(context.Background(), "01K0SRC0000000000000000000", shared.ChannelEmail, "")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if sender == nil {
		t.Fatal("a source with no credential got no sender")
	}
}

// And naming an identity that does not exist is still an error: the fallback is
// for a source that configured nothing, not for one that asked for something
// wrong.
func TestNamingAnIdentityThatIsNotThereIsStillAnError(t *testing.T) {
	reg := registryWithOwnEmail(t)

	if _, err := reg.For(
		context.Background(), "01K0SRC0000000000000000000", shared.ChannelEmail, "no-such-bot",
	); err == nil {
		t.Fatal("a named identity that does not exist was accepted")
	}
}
```

Build `registryWithOwnEmail` from whatever the existing tests in that file use
for a credential resolver that answers `credential.ErrNoCredentials`, with
srosha's own SMTP settings filled in.

- [ ] **Step 2: Run it**

Run: `go test ./internal/adapter/sender/ -run "TestASourceWithNothing|TestNamingAnIdentity" -v`
Expected: PASS, with no change to `registry.go`.

- [ ] **Step 3: `make prepush`, then stop**

---

## What this plan does not build

- **The approval page.** Phase 2, in the admin panel. Until then approval is an
  `UPDATE` run by hand, and the plan says so where it matters.
- **A review queue.** `sources_unapproved_idx` and `approved_at` are there for
  it; nothing reads them yet.
- **Editing a source after registration.** `UpdateSource` exists in the adapter
  and no page calls it. Changing a name or a default address is worth its own
  small piece of work, and nothing here depends on it.
- **Deleting anything.** Sources, keys and users are never deleted today, and
  what happens to a source when its owner goes is an open question the spec
  names and does not answer.
