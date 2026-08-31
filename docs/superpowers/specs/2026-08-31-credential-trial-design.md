# Testing a sending identity — design

A source registers a Telegram bot token, or an SMTP password, or a Gotify
application token, and then has no way to find out whether it works. The first
proof arrives when a real notification either lands or does not, hours later, in
a delivery row nobody is watching.

This adds one button: **send a test message with this identity**, on the portal
page where the identities already live.

---

## What a test is

**A real message, to the source's own default address, through the same
resolution a real send uses.**

Not a credential-validity check. `getMe` for Telegram and `AUTH` for SMTP would
prove a token parses and a password is accepted, and prove nothing about whether
a message arrives. They would also need a new method on `delivery.Sender` and
eight implementations, and for APNs and WhatsApp there is no clean endpoint for
it at all.

The trial sends a short fixed message. If it works the customer sees it on their
phone, which is a kind of proof no status code offers.

---

## The problem this had to solve first

The console cannot send. That is not an oversight — it is the shape of the
service:

```
gateway     ──► NATS ──► dispatcher ──► Telegram / SMTP / Gotify / …
                                        ▲
                                        │  sender.NewRegistry, eight adapters,
                                        │  an http client, token factories
console     ──► postgres
            ──► SMTP        (srosha's own mailbox, for sign-in codes only)
```

`usecase.Credentials`, which the portal already calls, touches only the
database: register, list, switch on and off, rotate. It has never sent
anything. The eight sender adapters are built in the dispatcher, and the
`NOTIF_SENDER_*` keys belong to the dispatcher alone. The console is not
connected to NATS either, so it cannot hand the work to the dispatcher.

So "test connection" is not a new handler. It is a new dependency for a binary.

### Why the console can take it safely

Two facts in the existing code make this smaller than it looks.

**The token factories hold no secrets.**

```go
type GoogleTokens interface { Open(serviceAccount []byte) (*googleauth.Source, error) }
type AppleTokens  interface { Open(p8 []byte, id appleauth.Identity) (*appleauth.Source, error) }
```

Both take the material as an argument. Constructing them requires nothing of
srosha's own.

**A source's own credential and srosha's fallback identity are different code
paths.** `Registry.For` calls `build` for a credential the source registered,
and `ours` only when the source registered nothing. Every branch of `ours`
begins by asking `configured()`.

Therefore the console builds a full registry with an **empty** `Fallback`:

```go
sender.NewRegistry(creds, secrets, httpClient, mailDialer,
                   googleTokens, appleTokens, sender.Fallback{})
```

It can send as the customer. It **cannot** send as srosha — every fallback
branch answers `noSender`. That boundary is held by the code that is already
there, not by a rule somebody has to remember. A test asserts it.

The console already has the keyring, `secret.New`, and an SMTP dialer. What it
gains is an http client, which means `NOTIF_HTTP_CLIENT_*` — until now the
dispatcher's alone — is read by the console too.

---

## Where it lives in the core

**Not on `usecase.Credentials`.** Two binaries build that type: the console, and
the gateway, which serves the same operations to the SDK over gRPC. Adding a
required `SenderRegistry` to its constructor would force the gateway to supply
one, and the gateway has no sender adapters at all.

A type of its own, built only by the console:

```go
// usecase.Trials
func (t *Trials) Run(ctx context.Context, sourceID string, credentialID shared.ID) error
```

This is the same split already made between `Operators` and `Sources`: a
different job, a different owner, a different type — rather than one type that
grows a dependency most of its callers cannot satisfy.

### It resolves the way a real send resolves

`Trials.Run` reads the credential to learn its channel and its name, then asks
the registry for that **name**, exactly as the dispatcher does:

```go
sender, err := registry.For(ctx, sourceID, cred.Channel, cred.Name)
```

Not a lookup by id and not a private path. A trial that resolved differently
from a real send would prove something other than what the customer asked. In
particular, a switched-off identity is refused here for the same reason it is
refused in production, and the customer is told so rather than being handed a
misleading success.

---

## One click

```
"Send a test" on the senders page
   │
   ▼ Trials.Run(sourceID, credentialID)
   ├── is the source active?                no → "this source has not been approved yet"
   ├── is the credential this source's?      no → 404, like every other page
   ├── DefaultAddresses[channel] set?        no → "set a default address for this channel first"
   ├── within the trial rate limit?          no → "too many tests, try again shortly"
   ▼
registry.For(source, channel, name)  →  Send(a short fixed message)
   │
   ├── ok    → "sent — the provider called it 12345"
   └── error → the provider's own words, as they arrived
```

**The last line is the feature.** "Telegram: 401 Unauthorized" is something a
customer can act on. "Test failed" is not. srosha's error wrapping already
carries the provider's message; the page shows it rather than replacing it with
a category.

### Refusals, and why each one

| Condition | Answer |
| --- | --- |
| Source not active | A source waiting for review has not been let out. A trial would be srosha sending on behalf of something it has not approved. |
| Credential not this source's | 404 rather than 403, matching every other portal page: whether a credential id exists is not a customer's business. |
| No default address for the channel | The trial has nowhere to go. Custom addresses are not accepted here — see below. |
| Over the rate limit | The button really sends. Without a cap it is a way to make srosha's server send whatever a customer wants, as fast as they can click. |

**The address is the channel's default and nothing else.** Not a field the
customer types. The credential is theirs and the spam risk is theirs, but the
server doing the sending is ours, and a free-text recipient box is a different
feature with a different argument behind it. `DefaultAddresses` already exists
and is already the thing a source must have before it can be approved.

---

## The cap

A button that sends a real message is an operational knob, so it lives in
configuration rather than in a constant:

| | |
| --- | --- |
| Key | `NOTIF_CONSOLE_TRIAL_PER_MINUTE` |
| Default | `3` |
| Scope | per source, in the console process |

Three per minute with a burst of three — `ratelimit.NewMemory` takes a
per-minute quota and uses it as the burst — is enough to try a fix and try
again, and not enough to be a sending channel.

**This is not the source's sending quota and cannot be.** The gateway's limiter
is a separate in-memory bucket in a separate process; there is no shared counter
to spend from. Saying that plainly here is better than implying a trial costs a
message when it does not.

---

## What is recorded

One audit row per trial, verb `credential.test`, actor the customer — the same
shape as `key.issue`, which is also a customer's own action on their own source.

It is **not** added to `sourceDecisionVerbs`. That list is the filter behind the
per-source history an operator reads, and it is deliberately narrow because the
actor of a customer's own action is the customer: widening it leaks the
customer's identity into a page a plain `admin` can open. The same reasoning
that moved `/audit` to `super_admin` applies unchanged.

No notification and no delivery row. A trial is a diagnostic, not traffic, and
putting it in the message log would make the log lie about what the source sent.

---

## Testing

| Test | Holds |
| --- | --- |
| the console's registry cannot send as srosha | build one with an empty `Fallback` and assert every channel's fallback path answers `noSender` — this is the security boundary of the whole change |
| a trial resolves by name, not by id | a switched-off credential is refused with the same error a real send gets |
| a source with no default address for the channel is refused | and the message names the channel |
| a stranger's credential id answers 404 | not 403 |
| the provider's error survives to the page | a fake sender returning "401 Unauthorized" produces a page containing those words |
| the rate limit refuses the fourth press | with the default of three |

The first is the one that matters most. If the console's registry ever gains a
fallback, a customer's failed trial would silently succeed by sending as srosha
— a wrong answer that looks like a right one.

---

## Out of scope

| | |
| --- | --- |
| A credential-validity check that sends nothing | rejected above; it would need a new port method and eight implementations |
| Testing from the SDK or the gRPC API | this is a portal affordance. The gateway has no sender adapters and this does not give it any |
| A free-text recipient | a different feature with a different argument |
| Trials for srosha's own fallback identity | the console structurally cannot, and a customer testing srosha's shared bot is not a thing they need |
