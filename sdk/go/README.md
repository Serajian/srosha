# srosha — Go SDK

**English** · [فارسی](README.fa.md)

The client for [srosha](../../README.md), an asynchronous notification service.
You submit a message once; srosha delivers it out of band across email,
Telegram, Bale, WhatsApp, Matrix, FCM and APNs, retrying per channel.

---

## 1. Before you can send anything

You need three things, in this order:

| | | |
| --- | --- | --- |
| 1 | **An address** | where srosha is listening — a hostname, or `host:port` |
| 2 | **An API key** | it identifies you; every call carries it |
| 3 | **A sending identity** | *whose* bot or mail account the message goes out as |

A bare hostname is enough when srosha is on 443, which is where it sits behind
a TLS terminator: `api.srosha.dev` and `api.srosha.dev:443` are the same target.
Give a port when it is anything else — `gateway:50051` for the plaintext
listener inside srosha's own network.

The third is optional at first: srosha can send as itself if the operator
configured it to, and you can add your own later. The first two are not.

## 2. Getting an API key

**You cannot mint one yourself, and there is no sign-up.** srosha has no admin
API and no console: whoever runs it creates your source and hands you a key out
of band. Ask them.

<details>
<summary>If you are that operator, this is what you run today</summary>

> **This is a stopgap.** An admin panel is the next thing being built: an admin
> takes a source's details, creates the record, and the key is issued and sent
> to them. What is collected and how the key travels are decided when the panel
> is. Until then, it is two SQL statements.

A key is `srosha_` followed by 43 characters of base64url, and only its SHA-256
is stored, so **the key is shown once and never again.**

```bash
# 1. mint a key. Give it to the customer now -- it cannot be recovered.
KEY="srosha_$(head -c 32 /dev/urandom | base64 | tr '+/' '-_' | tr -d '=')"
HASH=$(printf '%s' "$KEY" | shasum -a 256 | cut -d' ' -f1)
echo "$KEY"

# 2. two ids. Every id in srosha is a ULID: 26 characters of Crockford
#    base32, which EXCLUDES I, L, O and U. Typing one by hand and reaching
#    for a letter that looks right is how you meet the ulid_check constraint.
mkid() { python3 -c "
import time, secrets
A = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
n = (int(time.time() * 1000) << 80) | secrets.randbits(80)
print(''.join(A[(n >> (5 * (25 - i))) & 31] for i in range(26)))"; }
SOURCE_ID=$(mkid); KEY_ID=$(mkid)

# 3. create the source and the key
psql "$NOTIF_DB_DSN" <<SQL
INSERT INTO sources (id, name, max_priority, is_active, allow_custom_address,
                     created_at, updated_at)
VALUES ('$SOURCE_ID', 'acme', 'HIGH', true, true, now(), now());

INSERT INTO api_keys (id, source_id, key_hash, label, created_at)
VALUES ('$KEY_ID', '$SOURCE_ID', '$HASH', 'acme prod', now());
SQL
```

Two fields decide what the source may do:

- `max_priority` is their ceiling. Asking above it is **not** an error — the
  message is accepted at the ceiling and the answer says it was lowered.
- `allow_custom_address` false means they may only send to addresses configured
  for them, which bounds what a leaked key can reach.

A source may hold **two keys at once**, which is what makes rotation possible
without an outage: issue the second, let them move, revoke the first.

</details>

Keep the key out of your source tree. It is a bearer credential: anybody
holding it can send as you.

## 3. Install

```bash
go get github.com/Serajian/srosha/sdk/go
```

Go 1.23 or newer — deliberately below the service's own, so an SDK never makes
you upgrade your toolchain.

## 4. Send your first message

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/Serajian/srosha/sdk/go/srosha"
)

func main() {
	ctx := context.Background()

	c, err := srosha.New(ctx, "api.srosha.acme.test", os.Getenv("SROSHA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	r, err := c.Submit(ctx, srosha.Message{
		Title:  "Your order shipped",
		Body:   "Tracking: 123",
		Routes: []srosha.Route{srosha.Email("customer@example.com")},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("accepted as", r.ID)
}
```

**Connecting from inside srosha's own network?** Its gRPC listener has no TLS
there, so say so out loud:

```go
srosha.New(ctx, "gateway:50051", key, srosha.WithInsecure())
```

Both halves of that are unlike the outside case, and for the same reason:

- **`gateway`** is srosha's compose service name, resolved by Docker's own DNS —
  the same way srosha reaches `postgres` and `nats`. It is not a domain and
  there is no Traefik in front of it.
- **`:50051`** is required because the gRPC port is never published. It is
  `expose:` on the private network, so it exists for neighbours and for nobody
  else.
- **`WithInsecure()`** because TLS is terminated at Traefik, and the listener
  behind it speaks plaintext. Without it the client would begin a handshake that
  nothing answers.

TLS is the default on purpose, and the SDK deliberately does **not** guess from
the port. Forgetting `WithInsecure()` in here fails immediately and loudly;
guessing would one day send in the clear to something that was not what you
thought it was.

You *can* reach srosha by its public domain from inside the network — but that
leaves the private network, goes out to Traefik and comes back, for TLS you do
not need. It also breaks when the domain does, while your neighbour is fine.

### Know your limits before you hit them

```go
me, err := c.Whoami(ctx)
```

Two of the things below are otherwise only learnable by getting them wrong: a
priority ceiling shows up as a message that was quietly lowered, and a retention
window as a listing that was refused.

```go
me.ID                  // quote this when asking for help
me.Name
me.MaxPriority         // asking above it is lowered, not refused
me.AllowCustomAddress  // false means DefaultAddresses is all you can reach
me.DefaultAddresses    // per channel, what a route with no address resolves to
me.Retention           // a listing may not reach further back
me.MaxWindow()         // that, as the longest Window it will accept
me.RateLimitPerMinute  // counted per source
```

It is also the cheapest way to find out the address is right and the key works.
A client connects lazily, so without it the first news of either being wrong
arrives on the first message that mattered:

```go
c, err := srosha.New(ctx, addr, key)
if err != nil {
	return err
}
if me, err := c.Whoami(ctx); err != nil {
	log.Warn("srosha unreachable at startup", "err", err)
} else {
	log.Info("srosha", "as", me.Name, "ceiling", me.MaxPriority)
}
```

**Call it when a process starts, not on a timer.** It is not a health check: it
counts against your rate limit like every other call, and a successful answer
says nothing about the next one — keys are revoked and networks part in
between.

And **do not refuse to start when it fails.** srosha is asynchronous; an
application that will not boot while it is briefly down is worse than one that
logs the warning and carries on.

### The whole message

Everything `Message` carries, and what each field is for:

```go
c.Submit(ctx, srosha.Message{
	// Optional. Empty means one is generated for this call, which is what
	// makes retrying safe. Set your own when you want that guarantee across
	// calls -- see §9.
	IdempotencyKey: "order-42",

	Title: "Your order shipped",
	Body:  "Tracking: 123",

	// Optional. Above your ceiling is accepted and lowered, never refused;
	// the receipt says it happened.
	Priority: srosha.PriorityHigh,

	// Optional. Zero means it never expires. Past this the message is not
	// worth sending and a delivery still waiting fails as FailureExpired.
	ExpireAt: time.Now().Add(2 * time.Hour),

	// Optional. Carried through and returned untouched -- an order number,
	// a trace id. Two channels also read it; see below.
	Metadata: map[string]string{"order_id": "42"},

	// At least one. Each is a separate delivery with its own outcome.
	Routes: []srosha.Route{
		srosha.Email("a@b.test"),
		srosha.Telegram("123456789").From("marketing"),
	},
})
```

Only `Body` and `Routes` are required.

### Title and body, per channel

There is one pair of fields and seven channels, and they do not all have two
places to put them.

| | |
| --- | --- |
| `Email` | `Title` is the subject line |
| `FCM`, `APNs` | separate fields, rendered differently by the platform |
| everything else | one text field, so they are joined by a blank line |

So a title that reads well as a subject also reads well as a bold first line.
Leave it empty and only the body is sent.

### The shapes a message takes

**One person, several ways** — each route is its own delivery with its own
outcome, and one failing does not stop the others:

```go
Routes: []srosha.Route{
	srosha.Email("a@b.test"),
	srosha.Telegram("123456789"),
	srosha.APNs(deviceToken),
}
```

**Several people, one way:**

```go
Routes: []srosha.Route{
	srosha.Email("a@acme.test"),
	srosha.Email("b@acme.test"),
}
```

**As a particular identity of yours**, when a channel has more than one:

```go
srosha.Telegram("123456789").From("marketing")
```

**On a channel this build has no constructor for** — a newer service:

```go
srosha.To("sms", "+989121234567")
```

### Metadata two channels actually read

`Metadata` is yours and srosha never interprets it. Two provider adapters look
in it for what their API needs, and no other channel is affected.

**WhatsApp** refuses free text outside a window the recipient opened, and takes
an approved template instead. You decide which, because only you know whether
they wrote to you recently:

| key | |
| --- | --- |
| `template` | the approved template's name. Present means template, absent means text |
| `template_language` | defaults to `en_US` |
| `template_params` | a **JSON array** of strings, in order |

```go
Metadata: map[string]string{
	"template":        "order_shipped",
	"template_params": `["Ali","123"]`,
}
```

**FCM** carries the whole map into the push as its `data` payload, which is what
your app reads when it opens the notification. Keys FCM reserves — `from`,
`message_type`, `notification`, and anything starting with `google` or `gcm` —
are refused before the call rather than dropped silently.

**APNs** puts the map beside Apple's own `aps` key. The key `aps` itself is
refused for the same reason.

Everywhere else it is stored, returned by `Get`, and otherwise ignored.

### What "accepted" means

`Submit` answers as soon as the message is **stored**, not when it is
delivered. That is the whole design: it returns in the time it takes to write a
row, not the time a provider takes to answer.

So `r.ID` means "we have it and will keep trying", nothing more.

Three fields on the receipt are worth reading:

```go
r.ID          // the message's id -- what you ask about later
r.Priority    // what it will actually be sent at
r.Downgraded  // you asked above your ceiling; it was lowered, not refused
r.Duplicate   // this idempotency key had been used; nothing new was created
```

## 5. Find out what happened

```go
got, err := c.Get(ctx, r.ID)
for _, d := range got.Deliveries {
	log.Println(d.Channel, d.Address, d.Status, d.Reason)
}
```

One `Delivery` per recipient, each with its own outcome.

| `Status` | |
| --- | --- |
| `StatusPending` | still being tried |
| `StatusSent` | a provider accepted it |
| `StatusFailed` | it will not be tried again; `Reason` says why |

`StatusSent` means the **provider** took it. Whether a phone lit up is
something only the provider knows; `ProviderMessageID` is the handle you need
to ask them.

| `Reason` | what to do |
| --- | --- |
| `FailureNotReachable` | **the one you can act on.** The provider refused the *recipient*: a dead device token, an unregistered number, a room you were not invited to. Stop sending there. |
| `FailureNoSender` | no identity is configured for that channel — see §7 |
| `FailurePermanent` | the provider refused the message and would again |
| `FailureMaxAttempts` | tried as often as srosha will try |
| `FailureExpired` | your own `ExpireAt` passed first |

To see what you sent recently:

```go
for n, err := range c.List(ctx, srosha.LastDay) {
	if err != nil {
		return err
	}
	log.Println(n.ID, n.Title, n.CreatedAt)
}
```

Pages are fetched as the loop asks for them, so `break` stops the requests.

The window is a closed set — `LastHour`, `LastDay`, `LastWeek`, `LastMonth`,
`Everything` — and not two timestamps, because srosha is not an archive: past
its retention age a message is deleted, and an open-ended range would come back
short with nothing saying so. `Everything` means as far back as this deployment
keeps, which is the only answer that is right whatever that is set to. Ask for
longer than it keeps and you get `ErrInvalidRequest` naming the real limit.

## 6. Addresses, per channel

An address in the wrong shape is refused at `Submit`, before anything is
stored — so a mistake costs you an error, not a failed delivery hours later.

| Channel | Address | Example |
| --- | --- | --- |
| `Email` | a mail address | `a@b.test` |
| `Telegram`, `Bale` | a numeric chat id, or `@name` **for a public channel only** — never a person, whatever their username | `123456789`, `-100123`, `@acmenews` |
| `WhatsApp` | E.164, `+` and 8–15 digits | `+989121234567` |
| `Matrix` | a **room**, never a user. Matrix has no "send to this person" | `!abc:matrix.org` |
| `FCM` | an Android device token, 32–4096 characters | `cXy…` |
| `APNs` | an Apple device token, hexadecimal, 32–200 characters | `a1b2c3…` |

`srosha.To(channel, address)` covers a channel this build has no constructor
for yet.

## 7. Sending as yourself

By default a message goes out as srosha's own identity, if the operator
configured one for that channel. To send as your own bot or mail account,
register it — **once**:

```go
_, err := c.Credentials.Register(ctx, srosha.Registration{
	Name:       "alerts",
	Default:    true,
	Credential: srosha.TelegramCredential{Token: botToken},
})
```

After that you never mention it again. `Submit` names a **channel**, not an
identity, and the default is used:

```go
c.Submit(ctx, srosha.Message{
	Body:   "…",
	Routes: []srosha.Route{srosha.Telegram("123456789")},  // goes out as "alerts"
})
```

Only when a channel has more than one identity do you say which:

```go
srosha.Telegram("123456789").From("marketing")
```

Names are lowercase letters, digits and hyphens, because they travel in a url
and a config key. **They are unique per channel, not per source** — `alerts` on
Telegram and `alerts` on email are two different identities.

### "Once" means once, and here is why

It is tempting to put `Register` in your application's startup so the
configuration is applied on every boot. Don't. Registering the same name twice
on the same channel returns `ErrDuplicate`, and the second call changes
**nothing**: not the stored secret, not the default flag, not even the row's
`updated_at`. The whole request is refused rather than half applied, so this is
never destructive — but it has three consequences worth knowing.

- If your code treats the error as fatal, **your app stops booting** after the
  first time.
- The secret has to stay in your application's environment forever. The point
  of registering once is that srosha holds it sealed afterwards and your app
  never needs it again — one fewer place it can leak from.
- The quiet one: rotate the token later, and every restart re-registers the
  **old** one, gets `ErrDuplicate`, ignores it, and leaves you believing your
  code owns the token when it does not. Nothing tells you the two have parted.

Do it once, from somewhere that is not your application — a small admin command,
or by hand. If it genuinely has to happen at startup, tolerate the duplicate:

```go
if err := register(); err != nil && !errors.Is(err, srosha.ErrDuplicate) {
	return err
}
```

Listing first and registering only when absent looks tidier and is not better:
two instances booting together both see it absent, and one still gets
`ErrDuplicate`. The unique index is the arbiter, not your check.

**Neither this package nor srosha will make `Register` idempotent for you.** It
could only do so by ignoring a secret that differs from the stored one — and it
cannot even tell, because srosha never returns a secret. Silence there is
exactly the third consequence above, moved somewhere harder to see. When the
secret changes, say so: that is what `Rotate` is.

### What each channel needs

| | |
| --- | --- |
| `TelegramCredential`, `BaleCredential` | `Token` — from BotFather |
| `FCMCredential` | `ServiceAccount` — the whole service account json file, not base64 of it |
| `SMTPCredential` | `Host`, `Port`, `Username`, `From`, `Password`. Port 465 is TLS from the first byte; anything else is STARTTLS. Zero means 587 |
| `MatrixCredential` | `Homeserver` (https, no path) and `Token` |
| `WhatsAppCredential` | `PhoneNumberID` — Meta's id for the number, not the number — and `Token` |
| `APNsCredential` | `Key` (the `.p8` file's contents), `KeyID`, `TeamID`, `Topic` (your bundle id), and `Environment`. Leave `Environment` unset for production, which is what a shipped app uses — a token from a development build is unknown to production and comes back as `FailureNotReachable` |
| `RawCredential` | a channel this build has no type for yet: `Channel`, `Config` json, `Secret` |

None of them print their secret, through `%v` or `json.Marshal`.

### Changing one later

```go
c.Credentials.Rotate(ctx, id, newSecret)   // new secret, same name
c.Credentials.Update(ctx, id, cred)        // new settings, same secret
```

**Rotate is what a leaked token needs.** Registering a second identity instead
would make every message still naming the old one fail — turning a leak into a
code change.

`Update` sends **only the settings half**; a secret set on the credential you
hand it is ignored. It replaces the whole settings document rather than
patching it, so send every field, not just the changed one.

`Deactivate` stops an identity being used without forgetting it, so turning it
back on is not a re-registration. Nothing here deletes. If it held the default,
the channel is left with none until `SetDefault` names one — srosha will not
guess which should take over.

## 8. Being told instead of asking

```go
c.Webhooks.Register(ctx, "https://acme.test/srosha")
```

srosha then POSTs each delivery's final outcome. The url must be https and must
not point inside srosha's own network.

Every callback is signed with **HMAC-SHA256 over `<timestamp>.<body>`**, using
a secret handed to you out of band.

> **This package does not verify that signature for you.** Until it does,
> verify it yourself before trusting a callback. An unverified one is anything
> anybody posted to that url.

The callback is best effort: it is not retried past a limit, and enough
failures switch it off. `Get` and `List` are the reliable path — the webhook
saves you polling, it does not replace it.

## 9. Errors

```go
switch {
case errors.Is(err, srosha.ErrRateLimited):
	// too many requests. The client already backs off unless you turned
	// retries off; if you see this, you exhausted them
case errors.Is(err, srosha.ErrInvalidRequest):
	// the request is wrong and will be wrong again. err.Error() carries
	// srosha's own words -- show them, do not match on them
case errors.Is(err, srosha.ErrUnauthorized):
	// no key, or one srosha does not know
}
```

There is one sentinel per code srosha answers with, and no finer:
`ErrInvalidRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`,
`ErrDuplicate`, `ErrRateLimited`, `ErrUnavailable`, `ErrTimeout`,
`ErrInternal`.

`ErrInvalidRequest` covers an address in the wrong shape, a missing body, and a
window past retention. Telling those apart would mean matching on the message
text, which breaks the day somebody rewords a sentence — so **read the message,
branch on the sentinel.**

### Retrying

The client retries `ErrUnavailable`, `ErrTimeout` and `ErrRateLimited` and
nothing else. Three attempts by default, exponentially spaced with jitter.

**Retrying `Submit` is safe even though it creates something.** Leave
`IdempotencyKey` empty and one is generated for the call, so all three attempts
are one message. Two separate `Submit` calls get two keys and therefore two
messages — which is correct, because the same alert sent twice on purpose is a
real thing.

Set your own key when *you* want that guarantee across calls:

```go
srosha.Message{IdempotencyKey: "order-42", …}
```

Send it twice and the second answers with the first's id and
`Duplicate: true`. Keys are unique to you, so another customer's `order-42` is
not yours.

## 10. Connecting: the options

| | |
| --- | --- |
| `WithInsecure()` | plaintext, for a caller inside srosha's own network |
| `WithTLSConfig(*tls.Config)` | a private CA in staging |
| `WithTimeout(d)` | a deadline when your context carries none. Default 30s |
| `WithRetry(n)` | total attempts. `1` turns retrying off. Default 3 |

A `Client` holds a connection and is safe for concurrent use. **Keep it** —
building one per request throws away everything gRPC does about reconnection.

---

## Reference

Nothing on this package's surface is protobuf or gRPC: times are `time.Time`,
channels and priorities are strings, failures are errors `errors.Is`
understands. `sdk/go/notification/v1` is the generated contract underneath, and
you can drop to it if this package has not covered something — it is not
covered by the stability the rest of this package promises.

The design, and what was decided against, is in
[the spec](../../docs/superpowers/specs/2026-08-27-go-sdk-design.md).

`v0.x` while the API settles.
