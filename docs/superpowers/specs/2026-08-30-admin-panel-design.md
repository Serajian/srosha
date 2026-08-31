# The admin panel

The second surface on the `console` binary, and the one the customer portal is
waiting on. A source registers switched off and nothing can turn it on today
except an `UPDATE` run by hand.

This document is phase 2 of `2026-08-28-customer-portal-design.md`, which
deferred the panel and the approval page to here.

---

## What is already decided, and not reopened

`docs/ARCHITECTURE.md`, under *Two surfaces in one binary*, settles the shape:

```
:8090   portal   public, reached through Traefik
:8091   healthz  private
:8092   admin    private, never published
```

One process, three listeners. `adminweb` builds its own handler and is passed to
its own listener, so no router knows both. The guard reads the role from the
live `users` row on every request, never from the session or the cookie —
because cookies are not scoped by port, and a customer holding a valid portal
session arrives at the admin listener already carrying it.

That section is binding. Nothing below may weaken it.

## What an operator is for

A source that has just been registered is a stranger's claim about themselves.
Approval is the one place srosha decides whether to carry somebody's traffic,
and it exists because every rule that tried to replace it — see the previous
spec — decided the same thing on worse evidence.

So the panel's job is: decide, undo the decision, and answer "what happened to
this customer's messages" without reading them.

## Two audiences inside one surface

`role` already has three values and `IsOperator()` flattens the last two. The
panel is the first thing that needs them apart.

| | `admin` | `super_admin` |
| --- | --- | --- |
| approve, refuse, suspend, restore | yes | yes |
| read sources, the queue, a source's messages, the audit log | yes | yes |
| see who has an account at all | no | yes |
| change a role | no | yes |
| deactivate an account | no | yes |

An `admin` who could change roles could promote anybody, including themselves
out of whatever bound they are under. Least privilege is the whole reason
`super_admin` is a distinct value; without this table it is a string nobody
reads.

The roster is read-restricted too, not only write-restricted. An `admin` has no
page that shows it and no reason to have one -- their work is sources -- and a
use case that allowed a read no page exposes would leave the route guard as the
only thing between an `admin` and every customer's address. One line is not a
boundary. Both `People` and `Person` therefore take the `super_admin` check,
the same one `SetRole` takes.

## Refusal is a decision, not an absence

Today a source carries two facts: `is_active`, which decides whether it may
send, and `approved_at`, a record of when an operator first let it out.

A refused source would be `is_active = false, approved_at = null` — **identical
to one registered a minute ago.** It would return to the queue for ever and an
operator would take the same decision again, with no way to tell a stranger from
somebody already turned away.

So a third fact, and the reason beside it:

```sql
reviewed_at   TIMESTAMPTZ
review_note   TEXT NOT NULL DEFAULT ''
```

`approved_at` keeps exactly the meaning it has. Four states fall out of the
three columns:

| | `is_active` | `approved_at` | `reviewed_at` |
| --- | --- | --- | --- |
| waiting | `f` | null | null |
| approved | `t` | set | set |
| refused | `f` | null | set |
| approved, later switched off | `f` | set | set |

The review queue is `reviewed_at IS NULL`. `sources_unapproved_idx`, which was
created for a queue that was never built and has no reader today, becomes that
index.

**A refusal requires a reason; an approval does not.** A refused source whose
note is empty is exactly the silent failure the column was added to prevent, so
it is refused at the domain rather than left to the form. Approving needs no
justification: the source works, which is the whole message.

Approving a source that was refused before clears the note. The state is the
current decision, not a history — the history is the audit log.

**The customer sees `review_note`.** A refusal a person cannot read is a source
that silently never works, which is the failure the portal's "waiting for
approval" message exists to avoid. The portal's source page grows a branch for
it.

### Nothing is deleted

Refusing does not remove the source, its keys or its senders, for the reason
already written into `notifications`: deleting a source that has ever sent
anything is refused, and switching it off is what to do instead. A refused
source that is later approved keeps its id, so a customer who fixed whatever was
wrong does not re-register.

## What an operator may see of a customer's messages

`notifications` holds `title` and `body`. `deliveries` holds the recipient's
`address`. Diagnosing a failed delivery needs neither: `status`,
`failure_reason`, `last_error`, `attempts` and the timestamps are the whole of
it.

The view is the same two levels the customer's own query already has: messages
newest first, and one message opening to the deliveries it produced. What
changes is that neither level carries content.

So the operator's view carries metadata only, and this is enforced twice:

- The statement does not select `title` or `body`. A column that is never read
  cannot be rendered.
- The type has no field for a raw address. `OperatorDelivery` carries
  `MaskedAddress` and nothing else, so a handler that tried to show the full one
  would not compile.

Masking happens in the use case, not in SQL. *An adapter returns facts, the core
decides* — formatting is not the database's business.

This is the same lock used for `SourceSettings` in the portal, for the same
reason: a guarantee the compiler holds does not depend on anybody remembering.

## Where the code lives

A new use case, `usecase.Operators`, beside `Sources` and `Keys`.

`Sources` says what it is in its own first line: *what a customer does with the
things they own*. An operator acting on somebody else's source is not that
caller — it checks a role where the other checks ownership. Widening `Sources`
would mean every method knowing about two audiences, and that `if` is where the
mistake eventually gets written.

Every method goes through `Gate`, like every other change.

```
internal/adapter/api/web/admin.go          AdminDeps, NewAdmin, the route table
internal/adapter/api/web/admin_review.go   the queue and one source's decisions
internal/adapter/api/web/admin_people.go   roles and accounts, super_admin only
internal/adapter/api/web/admin_audit.go    who did what
public/templates/admin/                    its own directory, its own layout
public/static/admin/                       its own stylesheet
```

`adminweb` in the architecture text means a second struct in `web`, not a
subpackage: `make arch-check` allows parent to import subpackage and not the
reverse, which is why the portal is one struct in this package rather than
`web/portal`. The rule that matters — *three handlers, no shared mux* — is
satisfied by `NewAdmin` building its own engine, exactly as `NewPortal` does.

## The pages

```
/                  the queue: sources nobody has decided about, oldest first
/sources           all of them, filtered by state
/sources/:id       one source: its owner, its settings, its senders, its decisions
/sources/:id/log   its messages, newest first; one opens to its deliveries
/people            everybody                                    super_admin
/people/:id        one person's role and whether they may sign in   super_admin
/audit             what operators have done                    super_admin
```

## What is written down

`audit_log` records the verb and the target and has nowhere to say **why**.
`review_note` on the source is overwritten by the next decision, so a year later
the reason for the first refusal is gone.

```sql
note TEXT NOT NULL DEFAULT ''
```

A copy rather than a join, the same argument `notifications.source_name`
carries in its own comment: what is read a year later must describe things as
they were, not as they have since become.

New verbs:

```
source.approve   source.refuse   source.suspend   source.restore
user.role        user.deactivate                  user.activate
```

## Migrations

The service has never been deployed. Both changes go into the migrations that
create their tables — `00003_create_sources.sql` and
`00011_create_audit_log.sql` — rather than into new ones. A migration nobody has
run preserves a step in a history with no observers, and makes the shape of a
table readable only by reading two files.

## Testing

`docs/ARCHITECTURE.md` names two tests and calls them not optional. The first
exists. The second could not be written until this surface did:

```
a customer's session is refused by the admin guard        DONE
every admin route answers 404 on the portal's handler     this spec
```

The second is what a fourth binary would have given for free, and this is the
price of not building one.

Beyond those:

- An `admin` is refused on `/people`, and a `super_admin` is not.
- A refused source leaves the queue, and does not come back to it.
- A refused source shows its reason on the customer's own page.
- Approving a source lets it send: the same source refuses a message before and
  accepts one after, through the domain rather than by reading a column.
- The operator's delivery view does not contain the message body, asserted on
  the rendered page rather than on the struct.
- Every decision writes exactly one audit row, and a refused one writes none.
- Losing the operator role takes effect on the next request, not the next
  sign-in.

## Not in this phase

- **Deleting anything.** Nothing in this service deletes, and the panel is not
  where that starts.
- **Editing a customer's source settings.** An operator can switch a source off
  and say why; changing somebody's name or addresses on their behalf is a
  support action with no request behind it.
- **Metrics, charts, counts per day.** The panel answers questions about one
  source or one person. Aggregates are a different surface with different
  queries.
- **Approving anything other than a source.** Senders and callbacks are the
  customer's own and were never gated.
