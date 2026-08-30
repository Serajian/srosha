# Source Settings and the Sender Switch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** A customer can change a source's name, description and default
addresses, and can switch one of its senders off and on again.

**Architecture:** Both gaps are chains with one or more links missing, and they
break in different places. The source edit is missing everything from the domain
port upward — the SQL and the adapter method have existed since 22 August and no
interface ever exposed them. The sender switch is missing only the page:
`usecase.Credentials.Deactivate` and `Activate` exist and have no caller at all.

**Tech Stack:** Go 1.26, sqlc, goose, gin, html/template.

**Spec:** `docs/superpowers/specs/2026-08-28-customer-portal-design.md`

## Global Constraints

- The portal may write **only** `name`, `description`, `default_addresses`. Not
  `id`, `owner_user_id`, `is_active`, `approved_at`, `created_at`,
  `max_priority`, `allow_custom_address`.
- That is enforced by the **statement**, not by the use case above it. The
  portal gets its own query which cannot name the other columns.
- Keys, senders and callbacks are not part of a source update.
- Every change goes through `usecase.Gate` and leaves an audit row.
- `entity.go` declares exactly one type. Limits live in `const.go`.
- No `git commit` without a direct order.

---

### Task 1: `description` on the source

**Files:**
- Modify: `migrations/00003_create_sources.sql`
- Modify: `internal/core/domain/source/entity.go`, `const.go`
- Modify: `internal/adapter/db/postgres/queries/source.sql`, `source.go`

- [ ] **Step 1: the column, in the table that creates it**

The service has never been deployed, so there is nobody to migrate. A new
migration would preserve a step in a history that has no observers, and leave
the schema readable only by reading two files. The column goes into
`00003_create_sources.sql` where the rest of the table is:

```sql
description          TEXT        NOT NULL DEFAULT '',
```

A dev database already carrying the column from an earlier attempt needs its
goose marker moved back, and nothing else.

- [ ] **Step 2: the field and its bound**

`entity.go` gains `Description string` under `Name`. `const.go` gains:

```go
// maxDescriptionLen bounds what a customer writes about a source. Long enough
// for a sentence saying what it is for, short enough that it stays a label
// rather than becoming documentation nobody reads.
const maxDescriptionLen = 280
```

- [ ] **Step 3: run `make sqlc` and `make migrate-up`, then `go build ./...`**

- [ ] **Step 4: commit** — no. Leave it in the tree.

---

### Task 2: the narrow update, bottom to top

**Files:**
- Modify: `internal/adapter/db/postgres/queries/source.sql`, `source.go`
- Modify: `internal/core/domain/source/port.go`, `service.go`
- Test: `internal/core/domain/source/service_test.go`

**Interfaces:**
- Produces: `source.Repository.UpdateSettings(ctx, *Source) error`,
  `(*source.Service).Rename(ctx, id, name, description string, addresses map[shared.Channel]string) (*Source, error)`

- [ ] **Step 1: the query that cannot raise a ceiling**

```sql
-- UpdateSourceSettings writes the three columns a customer owns, and cannot
-- name the others. UpdateSource above it writes max_priority and
-- allow_custom_address too, which is right for an operator and wrong here: a
-- rename must not be able to carry a ceiling, and the cheapest way to promise
-- that is a statement in which those columns do not appear.
--
-- name: UpdateSourceSettings :execrows
UPDATE sources
SET name              = @name,
    description       = @description,
    default_addresses = @default_addresses,
    updated_at        = @updated_at::timestamptz
WHERE id = @id;
```

- [ ] **Step 2: the port grows one method**

```go
// UpdateSettings writes what the customer owns and nothing else. Deliberately
// not Update: the adapter has one of those already, it writes the ceiling
// columns too, and the two must not be confused at a call site.
UpdateSettings(ctx context.Context, s *Source) error
```

- [ ] **Step 3: the failing test**

```go
// The three fields the customer owns change. Nothing else does -- and the test
// asserts on the ceiling rather than trusting the statement, because the
// statement is what a later edit would break.
func TestChangingSettingsLeavesTheCeilingAlone(t *testing.T) {
	repo := &fakeSources{ /* a source with MaxPriority CRITICAL, AllowCustomAddress true */ }
	svc := source.NewService(repo, nil, fixedNow(at))

	got, err := svc.Rename(ctx, id, "renamed", "what it is for", nil)
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.MaxPriority != shared.PriorityCritical {
		t.Errorf("the ceiling moved: %v", got.MaxPriority)
	}
	if !got.AllowCustomAddress {
		t.Error("allow_custom_address was cleared by a rename")
	}
}
```

- [ ] **Step 4: run it, watch it fail with "undefined: Rename"**

- [ ] **Step 5: the service method**

It reads through `Manage` (not `Load` — a source waiting for approval must be
configurable), re-validates the name and description against their bounds and
every address against its channel, stamps `UpdatedAt`, and writes.

- [ ] **Step 6: run the tests, watch them pass**

---

### Task 3: the use case, and the audit row

**Files:**
- Modify: `internal/core/usecase/source.go`, `const.go`
- Test: `internal/core/usecase/source_test.go`

- [ ] **Step 1: the verb**

```go
ActSourceUpdate = "source.update"
```

- [ ] **Step 2: the failing tests**

```go
func TestOnlyTheOwnerChangesASource(t *testing.T)
func TestAChangeLeavesAnAuditRow(t *testing.T)
```

The first asserts a stranger gets `ErrNotFound` — not a refusal, which would
confirm the id exists.

- [ ] **Step 3: `Sources.Update`, through the gate**, ownership checked by
      reading the source with `One` first, exactly as `Keys.Revoke` does.

- [ ] **Step 4: run them**

---

### Task 4: the page

**Files:**
- Create: `public/templates/portal/source_edit.html`
- Modify: `public/templates/portal/source.html`, `internal/adapter/api/web/portal_source.go`, `portal_const.go`, `portal.go`
- Test: `internal/adapter/api/web/portal_test.go`

- [ ] **Step 1: the routes**

```go
authed.GET(pathSourceEdit, sources.showEdit)
authed.POST(pathSourceEdit, sources.update)
```

- [ ] **Step 2: the form**, carrying name, description and one row per channel,
      pre-filled from the source. The same `defaultAddresses(c)` reader the
      create form uses.

- [ ] **Step 3: the tests**

```go
// Somebody else's source is not editable, and says the same thing a missing
// one says.
func TestAStrangerCannotOpenTheEditPage(t *testing.T)

// The form has no field for anything on the right-hand column, and posting one
// anyway changes nothing.
func TestPostingTheCeilingInTheFormChangesNothing(t *testing.T)
```

The second is the one that matters: it posts `max_priority=CRITICAL` and
`is_active=true` and asserts the row is untouched.

- [ ] **Step 4: run them**

---

### Task 5: the sender switch

**Files:**
- Modify: `internal/adapter/api/web/portal_identity.go`, `portal_const.go`, `portal.go`
- Modify: `public/templates/portal/senders.html`
- Test: `internal/adapter/api/web/portal_test.go`

Nothing below the page is missing. `SenderPages` grows two methods,
`usecase.Credentials` already satisfies them.

- [ ] **Step 1: widen the interface**

```go
Deactivate(ctx context.Context, sourceID string, id shared.ID) error
Activate(ctx context.Context, sourceID string, id shared.ID) error
```

- [ ] **Step 2: the routes and the buttons.** A switched-off sender shows as
      off and offers "Turn on"; a live one offers "Turn off".

- [ ] **Step 3: the test**

```go
// Switching an identity off is not deleting it: it stays on the page, and it
// can come back. A source that lost a bot token needs the row to still be
// there when the new token arrives.
func TestASenderSwitchedOffIsStillThere(t *testing.T)
```

- [ ] **Step 4: run it**

---

### Task 6: the report and the checks

- [ ] `make prepush`
- [ ] `docs/changes/2026-08-30-source-settings.md`
- [ ] Stop. Do not commit without a direct order.
