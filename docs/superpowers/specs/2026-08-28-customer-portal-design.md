# Customer portal — design

Where a customer signs up, registers what they send as, and gets the key their
code uses. Today none of that exists: a source is two hand-written SQL inserts,
and the key reaches them however somebody remembers to send it.

This is phase one of two, and the phases are separate because they serve
different people and carry different risk.

| | | |
| --- | --- | --- |
| **1** | **Customer portal** | public. A customer signs up and registers their own sources |
| 2 | Admin panel | private. Operators approve, watch and intervene |

Only phase 1 is specified here.

**There was a third phase and there is not any more.** Subscription, plans and
prices are cancelled: srosha behaves the same way with everybody. Nothing is
sold, nothing is metered, and no feature is switched on by what somebody paid.
That decision is not a deferral -- it removes a reason that several rules below
used to lean on, and each of them had to stand on its own or go.

---

## What changed on the way here

The work started as "an admin panel where an operator registers a source". That
is not what is being built, and the difference is worth recording because it
inverts the original problem.

A customer registers **themselves**. So the question that started all of this —
how does the API key reach them — no longer has an answer, because it no longer
has a question: they create the key, and nobody hands anything over.

Signup was going to require an operator's approval, and does not. What approval
was really protecting against is dealt with by a rule instead; see **The rule
that replaced approval**.

---

## A user is not a source

They were one thing for most of the design conversation, and separating them
made everything after it simpler.

```
user     a person. an email, a role, signs in with a one-time code
source   a thing that sends. owned by a user
```

One user may own several sources — one company, two products — and each source
has its own keys, its own sending identities and its own callback.

### One table for everybody

```sql
users
  id           ulid
  email        text unique
  role         text    -- customer | admin | super_admin
  is_active    boolean
  created_at   timestamptz
```

Operators and customers are the same kind of row with a different `role`. That
was not the plan — two account tables were — and collapsing them removes a
second sign-in flow, a second session table and a second set of bugs.

`super_admin` exists for one reason that is not negotiable: **only it may make
somebody an operator, or take that away.** Without it, any admin can promote
themselves more admins and the two roles are one role.

Customers are not in that sentence: they create themselves, which is the whole
point of the portal. What super_admin controls is the `role` column, not the
row.

### Sources gain an owner

```sql
sources
  owner_user_id  ulid  -- new; sources have no owner today
```

Nothing is deployed, so this folds into the original migration rather than
arriving as an alter.

---

## Signing in

**There are no passwords.** A user is sent a one-time code at their email.

That was not a convenience decision. The first user has to be created by hand,
in SQL, and an argon2 hash cannot be written by hand — but an email can:

```sql
INSERT INTO users (id, email, role, is_active, created_at)
VALUES ('01K…', 'ops@acme.test', 'super_admin', true, now());
```

The same flow serves customers and operators.

```
1  enter your email
2  a code is sent
3  enter the code
4  a session begins
```

**Signing up and signing in are the same four steps.** There is no separate
"create an account": an email nobody has used yet becomes a `customer` on the
way through.

That is not a shortcut, it is what makes step 2 honest. Two flows would mean
signup answering "that address is taken" and sign-in answering "a code has been
sent", and anybody could tell the two apart and learn who has an account here.
One flow has nothing to leak.

A user costs nothing to create and can do nothing on its own: it has no source,
and a source with no credential of its own cannot send.

### What makes it safe

- **Step 2 answers the same way for everybody** — a new address, a known one,
  and a deactivated one all get the same sentence. Only the deactivated one is
  sent no code, and it does not learn that.
- **A code is single use.** Spent on the first attempt, valid or not, whatever
  is left of its life.
- **Guesses are capped.** Six digits is a million tries, which a script
  exhausts in seconds; past a few wrong answers the code is dead.
- **Requests are capped.** Otherwise anybody can fill a stranger's inbox, or
  learn which addresses are real by timing the reply.
- **Codes expire in minutes**, not hours.

### One thing not to pretend

Sealing the code the way this service seals every other secret is tempting and
would be **theatre**. Six digits is a million values: a lookup table inverts
any hash of it instantly. And whoever holds the database does not need a code
at all — they can write a session row.

So the protection is the three above: a short life, one use, a guess limit. Not
encryption. If the code were long and alphanumeric, sealing it would start to
mean something, but it would not make those three any stronger and it would be
harder to type.

### Where the code is sent from

**The portal sends it itself, over SMTP, and does not go through srosha.**

Sending it as a notification would be the service using itself, which is
appealing right up to the morning srosha is broken and nobody can sign in to
fix it. `internal/infra/smtp` is already there and needs no queue.

Email only. SMS would be the obvious second channel and there is no SMS channel
in this service yet.

### Sessions

```
cookie      HttpOnly, Secure, SameSite=Lax
stored      server side, in a table
lifetime    an absolute deadline and an idle one
```

Server side because deactivating a user has to log them out **now**, not when
a token they already hold happens to expire.

---

## What a customer does

```
sign up            an email, a code, in. Nothing else is asked
register a source  a name, and the addresses it sends to by default
issue keys         their own, as many as they need, revoked by them
register identities their own bot, mail account, signing key
register a callback and receive the signing secret, once
change a source    its name, what it is for, where it sends by default
```

Everything after signing up already exists as a use case; the portal is a second
face on it, next to gRPC. That is the point of the architecture and this is the
first time it is being used that way.

### Which fields are the customer's, and which are ours

`sources` carries both, and a form that asked for all of them would let a
customer raise their own ceiling.

| the customer's | ours |
| --- | --- |
| `name` | `max_priority` — their ceiling |
| `description` | `allow_custom_address` — how far a leaked key reaches |
| `default_addresses` | `is_active` — approval, and switching it off later |
| | `approved_at` — when an operator first let it out |
| | `owner_user_id` — who it belongs to |

The right-hand column takes defaults at registration and is changed only from
the admin panel.

### Changing a source afterwards

The left column is editable for as long as the source exists. A default address
outliving its usefulness is ordinary -- a team moves channel, a mailbox is
retired -- and registering a second source to change one address would leave the
first one behind with its keys still valid.

The right column is not editable, and not by being hidden from a form. The
portal's update writes **only** the three columns above:

```sql
UPDATE sources SET name = ..., description = ..., default_addresses = ...
```

`UpdateSource`, which the admin panel will use, writes `max_priority` and
`allow_custom_address` as well. If the portal shared it, every rename would
carry the customer's ceiling in the same statement -- correct only for as long
as the use case above it kept re-reading and re-sending the current values, and
wrong the first time somebody edited that use case. A second statement that
cannot name those columns is the cheaper guarantee.

`owner_user_id` is absent from both. Ownership is not a setting: transferring a
source is its own operation with its own rules, and it is not in this phase.

Three resources hang off a source and none of them are part of this. Keys,
senders and callbacks each have their own lifecycle, their own pages and their
own audit verbs; folding them into one save would mean a rename could revoke a
key.

A change is audited like every other administrative act -- `source.update`,
through the same gate.

---

## Approval, and the rule that was tried instead

**A source cannot send until an operator approves it.** Anybody may sign up and
register one; nothing it registers reaches anybody until somebody here says so.

That was not the first answer. What was written here before was a rule meant to
replace approval, and it is worth keeping the record of why it was reversed
rather than quietly deleting it.

### What was tried

A source that has registered no credential of its own falls back to srosha's
identities:

```go
// internal/adapter/sender/registry.go
the source registered nothing at all  ->  ours
```

The idea was to turn that off by default -- `sources.may_use_shared_sender`
false -- so that a spammer had to bring their own bot and the spam was signed as
them rather than as us. Open signup, no operator in the loop, and the cost of
abuse moved onto the abuser.

### Why it was reversed

Because it makes the product worse for the people it is for, and it was leaning
on a phase that no longer exists.

**Sending as srosha is the point, not a liability to price.** A source that has
to register its own bot before it can send anything has to do the hardest part
of the setup first. The default identities are what make the first message
possible, and taking them away to filter spammers takes them away from everybody.

**It was justified by a paid plan.** The old text said the flag "becomes what a
paid plan switches on". There are no plans. Remove that sentence and the rule is
a permanent tax on every legitimate customer, paid to avoid one conversation
with an operator.

**Approval costs a person a few minutes, once.** It is a real filter, made by
somebody who can look, and it does not change what the product does for anybody
who passes it.

### What approval actually is

Nothing new: `sources.is_active` already exists, and it is already the gate.

```
source/auth.go     EnsureActive()  inside Authenticate  -- the key is refused
source/service.go  EnsureActive()  inside Admit         -- the message is refused
```

So a source is created **inactive**, and an operator activates it. There is no
second check to add and no path around the one that is there.

`approved_at` is added beside it as a **record, not a gate**: it says when a
source was first approved and, with the audit log, by whom. It exists so a
review queue can ask for what has never been approved
(`approved_at IS NULL`) without also listing everything an operator switched off
last month. Nothing reads it to decide anything.

### What this costs, and it is not nothing

**Until the admin panel exists, approval is an UPDATE somebody runs by hand.**
The same as the first operator, and the same kind of stopgap: honest, written
down, and not allowed to outlive phase 2.

**A customer's first visit now ends in waiting.** They sign up, register a
source, issue a key -- and it sends nothing until somebody approves. The portal
has to say that plainly on the source's own page rather than letting them
discover it from a failed send.

---

## Two levers, and no cascade

"Deactivate a user" was one idea doing two jobs, and separating them dissolved
a problem rather than solving it.

```
users.is_active     may this person sign in
sources.is_active   may this source send
```

Nothing cascades between them, because the two are wanted in opposite
combinations:

| | sign in | send |
| --- | --- | --- |
| an operator leaves | no | — they have no sources |
| a customer abuses the service | maybe | **no**, immediately |
| a source has not been approved yet | **yes** | no — it has not been let out |

The third row used to be "a customer has not paid", and that was the strongest
argument for keeping the two apart. It is gone with the plans. The row above
replaces it and makes the same point: somebody signs in perfectly normally while
the thing they registered is not allowed out yet, and no cascade could express
that.

Closing a customer down entirely is therefore an **action** in the admin panel —
switch off each of their sources — and not a state. It is explicit, it is
visible, and reactivating the user later does not quietly undo it.

---

## One place every change goes through

Every mutating action passes through a single point that knows three things:

```
who      the session's user
what     "source.create", "key.revoke", "credential.rotate"
which    the target's id
```

Today it calls the use case and writes one audit row. It exists so that what
comes later is one file rather than fifty call sites:

- **roles**, when a support operator may look but not act
- **two-person approval**, if a dangerous action ever needs it
- **rate limits per user**, distinct from the per-source limit that exists

### The audit log

```sql
audit_log
  at · actor_user_id · action · target_type · target_id · detail
```

Append only. Never edited, never deleted — a record somebody can tidy only
shows what nobody wanted to hide.

It is in phase 1 rather than later because per-user accounts were chosen so
that "who created this source" and "who revoked that key" have answers, and
accounts without a record answer neither. No UI yet; `SELECT` is enough.

---

## Where it lives

A third binary beside gateway and dispatcher, talking to postgres directly
through the same core — as both of them do. Reaching the gateway over gRPC
instead would put the portal's traffic on the customer API and give up the
separation for nothing.

**It is public**, and that is a change from what was decided when this was an
admin tool. Then it was to be private, because creating sources and issuing
keys is the most powerful thing the service does. Customers have to reach the
portal, so the portal is exposed — and the admin panel, in phase 2, is a
**separate private surface** rather than a role inside this one. Routing that
puts the two on one port is one bug away from handing source creation to the
internet.

Server-rendered HTML: `html/template`, plain forms, no build pipeline and no
second repository. The audience is people with browsers, and a single-page app
would triple the work and add a deployment for no gain a customer would notice.

`CONFIG.md`'s "there is no REST surface and none is planned" is about the
**customer API**, which is still gRPC and still the only way to send. A portal
serving HTML is not a second API.

---

## Testing

The use cases beneath it are already covered. What is new and worth its own
tests:

```
sign-in       an unknown email answers exactly as a known one
              a code is spent by its first attempt, right or wrong
              guesses run out
              requests run out
              an expired code fails
sessions      deactivating a user ends their session on the next request
ownership     a user cannot see or touch a source they do not own
fields        a registration form cannot set max_priority or the flags
audit         every mutating action leaves exactly one row
approval    a source is created inactive, and sends nothing until an
            operator activates it -- refused at the key and at the message
```

The last one is the security property this whole design rests on, and it is the
one to write first.

---

## Order of work

1. **Users, sign-in, sessions.** No portal pages yet — done when a user can sign
   in and out and a deactivated one cannot.
2. **The chokepoint and the audit log**, before anything mutating exists, so
   nothing is ever written that does not go through it.
3. **Sources**: registration, ownership, and the shared-sender rule with its
   test.
4. **The rest of a source's setup**: keys, credentials, callback — each already
   a use case.

---

## Not in phase 1

- **Payment, plans, quotas.** Cancelled, not deferred. srosha behaves the same
  way with everybody, and no rule in this document may lean on a future in
  which it does not.
- **The approval page.** Phase 2, in the admin panel. Until then approval is an
  UPDATE run by hand, like the first operator.
- **The admin panel.** Phase 2, and a separate private surface.
- **SMS codes.** There is no SMS channel yet.
- **Transferring a source between users**, and what happens to sources when an
  owner is deleted. Nothing is deleted today.
