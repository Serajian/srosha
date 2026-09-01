# srosha — Go SDK

**English** · [فارسی](README.fa.md)

The client for [srosha](../../README.md), an asynchronous notification service.
You submit a message once; srosha delivers it out of band across email,
Telegram, Bale, WhatsApp, Matrix, Gotify, FCM and APNs, retrying per channel.

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

**You issue it yourself, from the portal.** Ask whoever runs srosha for its
address; the rest is four steps in a browser.

1. **Sign in.** Give your email address and a six-digit code arrives. There is
   no password and no sign-up form — the first time you ask for a code, the
   account is created for you.

2. **Create a source.** A source is the thing that sends, and the key belongs
   to it rather than to you. Give it a default address on at least one channel:
   a source with no default address anywhere and no permission to use custom
   ones has nowhere to send, and cannot be turned on at all.

3. **Wait for it to be approved.** A new source starts switched off, and an
   operator reviews it. While it waits, a key you have already issued
   authenticates correctly and every call still comes back
   `srosha.ErrForbidden` — deliberately not `srosha.ErrUnauthorized`. "Your
   source is off" has to be tellable from "your key is wrong", or you spend the
   wait rotating a key that was never the problem.

4. **Issue the key** on the source's Keys page. It is `srosha_` followed by 43
   characters of base64url, and only its SHA-256 is stored — so **it is shown
   once, on the page that issues it, and never again.** Copy it before you
   navigate away.

A source can hold more than one key at a time, which is what makes rotation
possible without an outage: issue the second, move over, revoke the first. A
revoked key is marked rather than deleted, so after an incident there is still
an answer to when it was revoked and when it was last used.

<details>
<summary>Two limits the panel shows but does not set</summary>

What a source may do is bounded by two fields that today are only changed in
the database. Every source starts at the cautious end of both.

- `max_priority` is the source's ceiling, and it starts at `NORMAL`. Asking
  above it is **not** an error — the message is accepted at the ceiling and the
  answer says it was lowered.
- `allow_custom_address` starts false, which means the source may only send to
  the addresses configured for it. That is what bounds where a leaked key can
  reach.

```sql
UPDATE sources
SET max_priority = 'HIGH', allow_custom_address = true, updated_at = now()
WHERE id = '<the source id, from its page in the panel>';
```

</details>

Keep the key out of your source tree. It is a bearer credential: anybody
holding it can send as you.

## 3. Install

```bash
go get github.com/Serajian/srosha/sdk/go
```

Go 1.25 or newer — deliberately below the service's own, so an SDK never makes
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
		Routes: []srosha.Route{srosha.EmailTo("customer@example.com")},
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
		srosha.EmailTo("a@b.test"),
		srosha.TelegramTo("123456789").From("marketing"),
	},
})
```

Only `Body` and `Routes` are required.

### Title and body, per channel

There is one pair of fields and eight channels, and they do not all have two
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
	srosha.EmailTo("a@b.test"),
	srosha.TelegramTo("123456789"),
	srosha.APNsTo(deviceToken),
}
```

**Several people, one way:**

```go
Routes: []srosha.Route{
	srosha.EmailTo("a@acme.test"),
	srosha.EmailTo("b@acme.test"),
}
```

**As a particular identity of yours**, when a channel has more than one:

```go
srosha.TelegramTo("123456789").From("marketing")
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

Each channel has two constructors. The bare form — `srosha.Email()`,
`srosha.Telegram()` — routes to this source's configured default address,
which is the common case: set it once in the portal and most messages never
name one. The `To` form — `srosha.EmailTo(address)`,
`srosha.TelegramTo(address)` — routes to an address the message itself names.

```go
Routes: []srosha.Route{
	srosha.EmailTo("someone@acme.test"),      // an address the message names
	srosha.Telegram(),                        // the source's default
	srosha.GotifyTo("42").From("ops"),        // see the note under the table
}
```

An address in the wrong shape is refused at `Submit`, before anything is
stored — so a mistake costs you an error, not a failed delivery hours later.

| Channel | Address | Example |
| --- | --- | --- |
| `Email` | a mail address | `a@b.test` |
| `Telegram`, `Bale` | a numeric chat id, or `@name` **for a public channel only** — never a person, whatever their username | `123456789`, `-100123`, `@acmenews` |
| `WhatsApp` | E.164, `+` and 8–15 digits | `+989121234567` |
| `Matrix` | a **room**, never a user. Matrix has no "send to this person" | `!abc:matrix.org` |
| `Gotify` | a positive integer — but see below | `1`, `42` |
| `FCM` | an Android device token, 32–4096 characters | `cXy…` |
| `APNs` | an Apple device token, hexadecimal, 32–200 characters | `a1b2c3…` |

**Gotify does not route on this address, but srosha does.** Its token is minted
per application and the token alone decides which application's subscribers see
the message -- so the value chooses nothing at Gotify's end. It is still not
decoration: srosha tells two deliveries apart by channel and address, so two
Gotify routes in one message need different ones or the second is folded into
the first as a duplicate. Give the application id and both facts line up.

That is not a reading of the documentation. A stock Gotify 2.6.3 was asked
three times: with the right id, with none, and with `999`, which did not exist.
All three arrived in the same place.

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
	Routes: []srosha.Route{srosha.TelegramTo("123456789")},  // goes out as "alerts"
})
```

Only when a channel has more than one identity do you say which:

```go
srosha.TelegramTo("123456789").From("marketing")
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
| `GotifyCredential` | `ServerURL` (https, no path) and `Token` — the application token, which alone decides which application a stock Gotify server delivers to |
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
hook, secret, err := c.Webhooks.Register(ctx, "https://acme.test/srosha")
```

srosha then POSTs each delivery's final outcome. The url must be https and must
not point inside srosha's own network.

**That call is the only time you are given the signing secret.** srosha keeps it
encrypted and no other call hands it back — not `Get`, not a listing, nothing.
Store it wherever your verifier will read it from.

Registering again to change the address returns an **empty** secret: the one you
have still stands. Rotating it silently would break every receiver that was
already verifying.

Lost it, or it leaked:

```go
secret, err := c.Webhooks.RotateSecret(ctx)
```

Every receiver still checking with the old one starts failing the moment that
returns. That is what a rotation is — change what verifies first, or accept the
gap.

Every callback is signed with **HMAC-SHA256 over `<timestamp>.<body>`**.

**Verify it before you trust it.** An unverified callback is anything anybody
posted to that url.

```go
// Once, at startup.
v, err := srosha.NewVerifier(os.Getenv("SROSHA_WEBHOOK_SECRET"))

func handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	cb, err := v.Verify(r.Header, body)
	if err != nil {
		// Do not say which check failed. Whoever is guessing does not need
		// to be told how close they got.
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	for _, d := range cb.Deliveries {
		fmt.Println(d.Channel, d.Address, d.Status, d.Reason)
	}
	w.WriteHeader(http.StatusOK)
}
```

Three things it checks, and each is a different problem:

| | |
| --- | --- |
| `ErrSignatureMissing` | no signature at all. Somebody who is not srosha |
| `ErrSignatureInvalid` | the signature does not match. Somebody pretending to be, or the body changed in flight |
| `ErrCallbackTooOld` | genuine, but signed too long ago. Almost always a replay — the other cause is your clock |

**Read the body raw and pass it unchanged.** The signature covers the exact
bytes srosha sent, so a body that has been re-encoded, pretty printed or run
through a json decoder will not verify. That is not a bug; it is the signature
working.

The stale window is five minutes by default, and it is exactly how long a
captured callback stays replayable. `srosha.WithTolerance(d)` widens it — do
that only for clocks you cannot fix.

`Verify` is a function and not an `http.Handler` on purpose: wiring it into a
route is three lines and belongs to whatever framework you use.

The callback is best effort: it is not retried past a limit, and enough
failures switch it off. `Get` and `List` are the reliable path — the webhook
saves you polling, it does not replace it. Answer `2xx` once you have the
callback in hand, not after you have finished acting on it.

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
