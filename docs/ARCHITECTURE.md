# ARCHITECTURE

Binding. Read this before writing, editing, or reviewing any Go code in this
repository. Code that contradicts it is wrong, even if it compiles, even if its
tests pass, and even if the surrounding code already does it that way.

This file is written incrementally, from what is actually decided and actually
built. Every entry is proposed to the owner in full and approved before it is
added. Nothing lands here as a side effect of another task, and nothing is
inferred from `docs/reference/`, which is background material and not a decision.

---

## Recovering a delivery nobody was told about

### The gap

Accepting a message is two separate writes. The rows go into Postgres, then an
event goes into NATS so a worker picks it up. Nothing can make those two atomic.

If the event never reaches NATS — the broker is down, the process dies in
between — the rows are there and no worker will ever look at them. The client
got its 201. The message is never sent, and nobody notices.

### There is no outbox table

The `deliveries` table is the outbox. A row in `PENDING` says exactly what an
outbox row would: this must be sent and has not been. A second table would
duplicate that and could disagree with it.

So the recovery is: find deliveries that have been pending too long, and deal
with them.

### Recovery is not a second use case

Recovery is not a different operation. It is the same one — deliver this
message — found a different way. So there is no `Reconciler`: the dispatcher
has two ways in and one path through.

```
Handle(id, attempt)   from the broker
Recover()             from the scheduler
        ↓
deliver(delivery, lastChance)
```

`lastChance` is the only thing the two disagree about, and each decides it from
what it knows: the broker's delivery count, or how long the row has been
waiting. Everything after that — the expiry check, the sender, what SENT and
FAILED mean — happens once, so an outcome means the same however it was reached.

It also means recovery **sends** rather than putting the event back on NATS.
`Handle` needs nothing from an event but the delivery id — the source, the
channel and the sender are all read from the rows — so there is nothing to
rebuild.

This means the scheduler must run **in the dispatcher**, not the gateway: it now
sends, so it needs the senders and their credentials. The gateway has neither
and must not get them.

### Age is the retry counter

Nothing is written when a send fails transiently. The delivery stays `PENDING`
and `updated_at` does not move, so every failed attempt leaves the row a little
older. The age is therefore a count of how long this has been stuck, and no
counter column is needed.

Two thresholds use it:

| | |
| --- | --- |
| `RECONCILE_AFTER` | older than this, `reconcile` picks the row up |
| `RECONCILE_GIVE_UP` | older than this, the attempt is the last one |

Below the second threshold a failure changes nothing and the next run tries
again. Above it, a failure is recorded as `FAILED` and the row stops moving.

The gap between the two is what decides how many attempts a row gets: with
`reconcile` running every five minutes and giving up at thirty, a row gets
roughly six.

Both live in config, because they are exactly the numbers someone wants to turn
when the service is under load.

### Why not retry for ever

A row that keeps failing would otherwise be picked up on every run until the end
of time, and never reported to the source. Giving up is what turns a silent loop
into an answer: `FAILED`, with a reason, on a row the source can query.

---

## Shutdown order is declared, not inherited

Dependencies close by **tier**, from the outside in: listeners, then outbound
clients, then the broker, then the store. The tiers are named in
`internal/registry/const.go`, each carrying the reason it sits where it does.

### The alternative, and why not

Closing in the reverse of the order things opened also works, and it has one
property tiers do not: it maintains itself. Open something after what it depends
on and its shutdown is automatically right.

What it does not do is say anything. The order becomes a consequence of how
`bootstrap` happens to be written, so moving two lines silently changes what
happens on shutdown and nothing fails. Anyone reading `Close` sees a reversed
loop, not a decision.

A tier says what a dependency **is**, not where it was opened. Within one tier
there is nothing to sort by, so those still unwind in the reverse of the order
they were built -- the dispatcher holds two http clients at the same tier, so
that case is real.

The cost is accepted deliberately: a tier is a second source of truth, and a
wrong one is a silent bug the reverse order could not have had.

### Readiness has no tiers

Closing has an order because it pulls something out from under someone. Asking a
question does not, so `Ready` checks everything and no order is defined.

It reports each dependency **separately** rather than as one joined error,
because whoever asked has to know *which* one is down -- and the only other way
to tell would be to read the error's message, which this repository forbids.
The reason itself never leaves the process: it names our dependencies and the
addresses they live at, so it goes to the log while the endpoint answers with
names and a status.

---

## A source's keys are rows, not a column

Authentication is a lookup, not a comparison. There is no username to find the row by: the
key is both the identifier and the secret, so the stored hash of it is what the row is found
by. `api_keys.key_hash` is therefore indexed, and hashing is a plain SHA-256 — `bcrypt` and
`argon2` are for low-entropy human passwords, and their per-row salt would make the lookup a
full scan.

The keys live in their own table rather than in a column on `sources`, so a source can hold
**two at once**. Rotation is then: issue the second, let them move, revoke the first — with
no window in which their messages are refused. One column would have made every rotation an
outage for that customer.

A revoked key is marked, never deleted. After an incident the first question is when the key
was revoked and when it was last used, and a deleted row answers neither.

---

## An encrypted value says which key encrypted it

A bot token has to be handed to Telegram, so it must come back out: it is encrypted, not
hashed. The key for that lives in the environment and never in the database — a dump that
carries both the ciphertext and the key protects nothing.

The hard part is not encrypting. It is the day the key changes, which is a *when*, not an
*if*. If the column holds only ciphertext, nothing in the row says which key produced it, so
changing the key means stopping the service, decrypting everything with the old key and
re-encrypting with the new — an outage that can fail halfway and leave two kinds of row that
cannot be told apart.

So the stored value describes itself:

```
v1.2.<nonce>.<ciphertext>
   │
   └─ which key encrypted this
```

and configuration holds a **set** of keys plus which one new writes use. Rotation is then:
add the second key, point the current-key id at it, let old rows re-encrypt as they are
touched, run a job for the ones nobody touches, and drop the first key when no row names it.
No step stops the service.

The version prefix is for the day the algorithm changes rather than the key.

**AES-256-GCM**, so that a tampered ciphertext fails to open instead of opening to rubbish.
The credential's own identity is bound in as authenticated data: without that, a writer who
can reach the database can copy source A's encrypted token into source B's row and B sends as
A — no key broken, just a value moved. With it, that copy fails to open.

This is for anything we must be able to read back — bot tokens, the SMTP password. It is the
opposite case from an API key, which is only ever compared and so is hashed. Two mechanisms
for one thing is where one of them gets forgotten, so the rule is the question: *do we need
the original back?*

Both binaries hold the keys, and the cipher is symmetric — so the gateway holding the key to
seal is the gateway holding the key to open. That is a real widening and it is accepted rather
than overlooked: what this guards against is a database dump, and the gateway already reads
those rows with the same connection string. An asymmetric scheme, where the gateway can only
seal, is the only thing that would make the restriction real; that is a decision for the day
these two stop trusting each other.

A value is resealed when it is read, and only if it names a key that is no longer the current
one. The key id is inside the value, so asking costs no column and no second query. Without
that guard every read would be followed by a write, because sealing is randomized and no two
seals of one value are ever equal. The reseal is best effort: the secret is already open and
the message is going out, so a failed rewrite is logged and the next read tries again.
