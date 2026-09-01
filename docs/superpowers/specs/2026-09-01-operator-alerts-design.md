# Operator alerts — design

srosha tells its customers what happened to their messages and tells nobody
what happened to srosha. A source registers and waits for a review nobody knows
is due. A deploy comes up, or does not, and the only way to find out is to go
and look. A scheduled job fails and the line goes into a log file.

This gives the operator one channel — Gotify — carrying the things they either
need to act on or would want to know.

---

## The rule that shapes everything

**An alert must not travel the path it is reporting on.**

```
if it went through the pipeline:

  Notify ──► postgres ──► NATS ──► dispatcher ──► Gotify
             ▲            ▲
             └────────────┴── the alert "postgres is down"
                              needs postgres to be up
```

So the alerter holds its own http client and its own Gotify credential, and
reaches Gotify directly. It shares no code path with a customer's message
beyond the `gotify` package that formats the call — which is exactly the part
that cannot be down.

That is not a refinement. An alarm wired through the thing it watches is an
alarm that is silent precisely when it matters.

---

## What raises an alert

Two sources, one port.

### Everything the audit already sees

`usecase.Gate.Do` is the single point every notable change passes through, and
it already carries who did it, what verb, and to what. Eleven verbs:

```
source.create   source.update   source.approve  source.refuse
source.suspend  source.restore  key.issue       key.revoke
user.role       user.deactivate user.activate
```

All of them alert. The owner chose the whole list over a curated subset, with
the noise understood: a customer issuing keys will reach the operator's phone.
A narrower list is a configuration change later, not a redesign.

**After the change succeeds, not before.** The audit deliberately records
attempts — "a change nobody can account for is worse than a change refused" —
but an alert saying a source registered is wrong if it did not:

```go
if err := fn(ctx); err != nil {
    return err
}
g.alerts.Notify(ctx, entry)
return nil
```

### What the audit cannot see

Lifecycle, from `bootstrap`:

| Event | Why it earns a push |
| --- | --- |
| started | the deploy worked, and which binary |
| failed to start | the deploy did not, with the reason |
| a dependency went down | postgres, nats, or **schema** — the check added when three services reported healthy on a database with no tables |
| a dependency recovered | so the operator knows the incident ended without going to look |
| a scheduled job failed | today it is a log line nobody reads |

Anything added later calls the same method. That is the whole extension story:
one port, one call.

---

## Where the code lives

```
internal/adapter/alert/     the queue, the worker, the wording
        ▲
        │ a one-method interface this package declares for itself
        │
bootstrap ──► gotify.New(client, token, cfg)
```

One adapter may not import another, and `make arch-check` enforces it. The
error it prints is the instruction: *declare the interface you need in the
package that calls it; bootstrap passes the implementation in.* So `alert`
declares what it needs — something that takes a `shared.Message` — and
bootstrap hands it a `gotify.Sender`.

The core gains a port with one method, defaulting to a no-op so that every
existing test of `Gate` keeps compiling and passing without knowing this exists.

---

## It must never damage what it reports

Three rules that are the same rule:

| When | What happens |
| --- | --- |
| The queue is full | the alert is **dropped** and a warning is logged |
| Gotify refuses or times out | logged, never retried |
| Nothing is configured | a no-op: no queue, no goroutine, no cost |

An alert that holds a transaction open is worse than no alert. A source
registering must not become slower, or fail, because a push server is
unreachable — and it must not be able to.

Not retried, deliberately. A retried alert arrives after the operator has
already found out some other way, and a queue that drains slowly turns into a
queue that reports yesterday.

---

## The message

The audit entry has everything: actor, actor's email, verb, target type, target
id, time.

**The email is included.** The owner decided this with the consequence stated:
for `source.create` the actor is the customer, so whoever holds the Gotify token
sees customer email addresses on a lock screen and in that server's history.
That is the same audience `/audit` has — it is a `super_admin` view — and the
token now carries that visibility.

```
srosha · source.create
mohsen@acme.test registered source 01K0SRC…
```

---

## Configuration

| Key | |
| --- | --- |
| `NOTIF_ALERT_GOTIFY_SERVER_URL` | the operator's own Gotify |
| `NOTIF_ALERT_GOTIFY_TOKEN` | an application token, sealed like any secret |
| `NOTIF_ALERT_GOTIFY_APP_ID` | the address the message is delivered to |
| `NOTIF_ALERT_QUEUE` | how many alerts may wait before dropping. Default 64 |

All three Gotify values empty means alerts are off, which is the state on every
laptop. Read by all three binaries: each announces its own start, and each
carries its own gate.

**`APP_ID` rests on an unverified assumption.** Whether Gotify reads an
application id from an `appid` query parameter — or needs one at all, since the
token already selects the application — has never been tested against a real
server. It is documented in the comment above `(*Sender).endpoint`. This
feature will be the first thing srosha has ever actually sent through any
channel, so that assumption gets tested here or nowhere.

---

## Testing

| Test | Holds |
| --- | --- |
| a full queue drops and does **not** block | the one that matters: a slow Gotify must not slow a source registration |
| a pusher that always fails returns nothing to the caller | |
| `Gate.Do` notifies after success and stays silent on failure | the alert must not describe something that did not happen |
| unconfigured builds a no-op with no goroutine | |
| the message carries verb, target and email | it is what the operator reads |

The first is the one to write first and to break on purpose: an alerter that
blocks under load is indistinguishable from a working one until the day it
matters.

---

## Out of scope

| | |
| --- | --- |
| Alerting through srosha's own pipeline | argued against above |
| Channels other than Gotify | the operator has one. A second is a config change against the same port |
| Retries, batching, digests | an alert is only useful fresh |
| Alert rules, thresholds, silences | this is a push channel, not a monitoring system |
| Per-customer notification of their own events | a different feature for a different audience |
