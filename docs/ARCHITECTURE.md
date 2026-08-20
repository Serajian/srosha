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
