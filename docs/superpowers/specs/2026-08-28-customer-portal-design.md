# Customer portal — design

Where a customer signs up, registers what they send as, and gets the key their
code uses. Today none of that exists: a source is two hand-written SQL inserts,
and the key reaches them however somebody remembers to send it.

This is phase one of three, and the phases are separate because they serve
different people and carry different risk.

| | | |
| --- | --- | --- |
| **1** | **Customer portal** | public. A customer signs up and runs their own sources |
| 2 | Admin panel | private. Operators watch, investigate and intervene |
| 3 | Subscription and payment | rides on 1 |

Only phase 1 is specified here.

Phase 3 is deliberately last: until there is a real customer, every decision
about plans and prices is a guess.

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
| `default_addresses` | `allow_custom_address` — how far a leaked key reaches |
| | `may_use_shared_sender` — see below |

The right-hand column takes defaults at registration and is changed only from
the admin panel.

---

## The rule that replaced approval

Signup is open. Nobody waits for an operator.

That is only safe because of one rule, and the rule matters more than the
approval it replaces.

**A source may not send as srosha.** Today, a source that has registered no
credential of its own falls back to the service's own identities:

```go
// internal/adapter/sender/registry.go
the source registered nothing at all  ->  ours
```

Which means an account created a minute ago could send anything to anybody
**signed as us**: our Telegram bot banned, our sending domain blacklisted, and
our real customers paying for it.

So `sources.may_use_shared_sender` defaults to false, and a source that has
registered nothing can send nothing.

This is a better filter than approval, because it moves the cost of abuse onto
the abuser:

```
a spammer       must bring their own bot -- the spam is signed as them
a real customer has one already, registers it, and works
```

Who they choose to message is between them and their provider.

Later this flag becomes what a paid plan switches on: **using srosha's own
identities stops being a liability and becomes something to sell.**

### One place this touches existing code

`Registry.For` takes a source id, not a source, so it cannot see the flag. The
check either loads the source there or is made a level up. Deciding which is
implementation, but it is a real edge with code that already works and not just
a new column.

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
| a customer has not paid *(phase 3)* | **yes** — or how would they pay | no |

A cascade makes the third row impossible, and the third row is what phase 3 is
built on.

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
shared sender a source with may_use_shared_sender false and no credential
              of its own cannot send
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

- **Approval.** Removed, and replaced by the shared-sender rule.
- **Payment, plans, quotas.** Phase 3. The service has a rate limit and no
  monthly allowance, and adding one is its own piece of work.
- **The admin panel.** Phase 2, and a separate private surface.
- **SMS codes.** There is no SMS channel yet.
- **Transferring a source between users**, and what happens to sources when an
  owner is deleted. Nothing is deleted today.
