# Go SDK — design

The first client library for srosha, and the first of several: Go now, other
languages later. This document is what was agreed before any of it was written.

srosha speaks gRPC and nothing else. There is no REST surface and none is
planned, so every SDK in every language will be a gRPC client and
`api/proto/notification/v1` is the one contract behind all of them.

---

## What it covers

All three services — `NotificationService`, `CredentialService`,
`WebhookService`.

**Not the inbound half.** srosha signs its status callbacks with HMAC-SHA256,
and verifying that signature runs in the customer's process, not ours. A helper
for it was considered and deliberately left out of this version. The
consequence is stated plainly here because it is a security consequence: every
customer writes that verification by hand, and getting it wrong means accepting
a forged callback. It is the first candidate for the next version.

## What it adds over the generated client

`protoc` already produces a working client. This one exists to hide three
things the generated code cannot:

- **protobuf itself.** `time.Time` rather than `*timestamppb.Timestamp`, a
  `Channel` string rather than an enum constant, typed errors rather than
  `codes.ResourceExhausted`. A customer who has to import `timestamppb` is
  coupled to our transport, and we could never change it.
- **Connecting.** Dialing, TLS, and the bearer header are four lines each of
  the kind that are wrong until they are not.
- **Rules the customer would otherwise have to learn.** Which errors are worth
  retrying, that Submit is only safe to retry with an idempotency key, and how
  a page token is walked.

Per-channel send helpers — `SendEmail`, `SendTelegram` — were rejected. Seven
methods with identical bodies and a different enum is ceremony, and every new
channel would add one.

---

## Structure

```
api/proto/notification/v1/*.proto     unchanged, the one source
                  │
                  │ buf
                  ▼
sdk/
├── README.md                  the rule every language SDK follows
└── go/                        module github.com/Serajian/srosha/sdk/go
    │
    ├── srosha/                the only package a customer imports
    │   ├── client.go            New, Close, the options
    │   ├── send.go              Submit
    │   ├── query.go             Get, List, the iterator
    │   ├── credential.go        the sending identities
    │   ├── webhook.go           where status is pushed
    │   ├── channel.go           Channel, and the Route constructors
    │   ├── types.go             Message, Notification, Delivery, Page
    │   ├── errors.go            what a caller can act on
    │   └── const.go             limits and bounds
    │
    ├── notification/v1/       the contract. generated, and the server reads it
    │                            too. package notificationv1
    │
    └── internal/
        ├── transport/           dial, TLS, the bearer header
        └── retry/               which codes, and for how long
```

Three directories with three different lifetimes, which is what makes the
layout say something: `srosha/` is public and stable, the generated package is
never edited by hand, and `internal/` can change tomorrow without breaking
anybody.

### Why a separate module in this repository

`gen/` moves to `sdk/go/notification/v1/` and is deleted from the main module.
`buf.gen.yaml` changes in two places — `out: sdk/go` and a `go_package_prefix`
of `github.com/Serajian/srosha/sdk/go` — and `paths=source_relative` puts the
files where the proto says they go. The package clause stays `notificationv1`.
`internal/adapter/api/grpcsrv` — the only package that imports it, ten files in
one directory — imports it from its new home. The main `go.mod` gains a
`require` and a `replace ./sdk/go`, so local builds always use local source and
customers never see the replace.

Three alternatives were weighed:

| | Why not |
| --- | --- |
| One module, `sdk/` inside the main one | The SDK's version number becomes the server's. A dispatcher bugfix release bumps the SDK with nothing changed for a customer. |
| Separate module, `gen/` generated twice | Zero churn in the server, but two generated copies of one proto in one repository. Every reviewer trips on it once. |
| Separate repository, `srosha-go` | What Stripe and Twilio do, but the proto must be synchronised across repositories — and it will drift, worse with every language added. |

The blast radius was measured before choosing, not after: one package.

### Naming

The directory is `sdk/go` so that `sdk/python` and `sdk/node` have somewhere
obvious to go. The public package is `srosha/`, so the import path stutters
once and the call site reads correctly every day:

```go
import "github.com/Serajian/srosha/sdk/go/srosha"

srosha.New(...)   srosha.Email("a@b")   srosha.ErrRateLimited
```

### Two consequences of this layout

**The generated package cannot be `internal/`.** Go's internal rule is
path-based: a package under `sdk/go/internal/` is invisible to the server,
which is not under `sdk/go/`. So the generated package is public whether we
like it or not, and it is accepted as an escape hatch rather than worked around — a
customer who needs something the ergonomic layer does not expose drops one
level instead of being stuck. No `Raw()` method is added for it: a method is a
promise of stability, and this is not stable.

**The SDK's `go.mod` says `go 1.23`, not the server's `1.26`.** An SDK must not
force a customer to upgrade their toolchain. 1.23 is the lowest version that
has range-over-func, which the iterator needs.

---

## The public API

```go
c, err := srosha.New(ctx, "srosha.acme.test:443", apiKey)
defer c.Close()

r, err := c.Submit(ctx, srosha.Message{
    IdempotencyKey: "order-42",
    Title:          "Your order shipped",
    Body:           "Tracking: 123",
    Routes: []srosha.Route{
        srosha.Email("a@b.test"),
        srosha.Telegram("123456789"),
        srosha.APNs(token).From("alerts"),
    },
})
```

`Route` is a channel, an address, and optionally which identity it goes out as:

```go
type Route struct {
    Channel Channel
    Address string
    Sender  string
}

func Email(address string) Route     // one per channel, each a struct literal
func Telegram(address string) Route
// …

func (r Route) From(sender string) Route   // returns a copy
```

These constructors are the one place a per-channel function earns its place.
The body is a literal, `srosha.` autocompletes into the list of channels, and a
new channel costs one line rather than a method with logic in it.

### Options

| | |
| --- | --- |
| `WithInsecure()` | plaintext, for a caller inside `srosha-net` |
| `WithTimeout(d)` | a deadline applied when the caller's context carries none |
| `WithRetry(n)` | `0` means one attempt |
| `WithTLSConfig(*tls.Config)` | a private CA in staging |

**TLS is the default and plaintext is explicit.** srosha's gRPC server runs
without TLS today and is reachable only from inside its own network, but a
default that is insecure is what ships to production by accident. The mistake
falls in the safe direction.

### Methods

```go
c.Submit(ctx, msg)         c.Credentials.Register(...)
c.Get(ctx, id)             c.Credentials.Rotate(...)
c.List(ctx, window)        c.Webhooks.Register(...)
```

Asymmetric on purpose. Nineteen calls in twenty are `Submit`, and
`c.Notifications.Submit` is ceremony for a service whose whole purpose is
submitting. Setup is rare and reads better grouped. There is no collision:
`Get` exists on notifications and webhooks, and `Credential` has no `Get` at
all.

---

## Types

No protobuf crosses the SDK's surface.

| wire | SDK |
| --- | --- |
| `*timestamppb.Timestamp` | `time.Time`, zero meaning unset |
| `pb.Channel` (enum) | `srosha.Channel` (string) |
| `pb.Priority` | `srosha.Priority` |
| `pb.Window` | `srosha.Window` |

`Channel` is a string rather than an integer, and that buys forward
compatibility: **a newer server does not break an older SDK.** A channel this
build has never heard of arrives as its own name rather than as a zero value or
a panic.

### The time window

`List` takes a closed vocabulary, not two timestamps:

```go
for n, err := range c.List(ctx, srosha.LastWeek) { … }
for n, err := range c.List(ctx, srosha.Everything) { … }
```

This mirrors the `Window` enum added to the proto in `feat/list-window`, and
the reasoning is recorded there: past the retention age a message is deleted,
and a range reaching beyond it comes back short with nothing saying so.
`srosha.Everything` is the zero value and means "as far back as this deployment
keeps" — the only answer that is right whatever the retention age is set to.

A window longer than the deployment keeps is refused by the server, and the
message names the real limit.

### Pagination

An iterator, not a page token the caller walks:

```go
for n, err := range c.List(ctx, srosha.LastWeek) {
    if err != nil {
        return err
    }
    fmt.Println(n.ID, n.Title)
}
```

`iter.Seq2[Notification, error]`, which is why the module needs Go 1.23.

---

## Errors

Only two things cross the wire — a code and the message the core wrote for a
client. The `reason`, which names columns, hosts and rejected values, never
does. The SDK's error model has to live inside that.

```go
var (
    ErrInvalidRequest  // InvalidArgument
    ErrUnauthorized    // Unauthenticated
    ErrForbidden       // PermissionDenied
    ErrNotFound        // NotFound
    ErrDuplicate       // AlreadyExists
    ErrRateLimited     // ResourceExhausted
    ErrUnavailable     // Unavailable
    ErrTimeout         // DeadlineExceeded
    ErrInternal        // everything else
)

// Error is what every failed call returns. It wraps one of the sentinels
// above, so errors.Is finds it, and carries the sentence the service wrote.
type Error struct {
    kind    error  // one of the sentinels above
    Message string // what the service said, for a person to read
}

func (e *Error) Error() string { … }
func (e *Error) Unwrap() error { return e.kind }
```

```go
if errors.Is(err, srosha.ErrRateLimited) { … }
```

`Unwrap` rather than a hand-written `Is`: one method, and `errors.As` keeps
working for a caller who wants the message out.

**One sentinel per gRPC code and no finer.** `InvalidArgument` covers an
invalid address, a missing body, a window past retention and a channel with no
sender, and a caller could act differently on some of them. The only way to
tell them apart today is matching the message text, which `docs/CONVENTIONS.md`
forbids and which breaks the day somebody rewords a sentence.

So the SDK does not do it. If a caller ever needs to branch inside
`InvalidArgument`, the answer is a machine-readable reason on the wire — a
server change — not cleverness here.

---

## Retry, and what makes it safe

| code | retried |
| --- | --- |
| `Unavailable` | yes |
| `DeadlineExceeded` | yes |
| `ResourceExhausted` | yes, with a longer wait |
| everything else | no |

`Submit` without an idempotency key is **not** safe to retry: a timeout
followed by a second attempt is a second message.

**So the SDK generates one when the caller did not.** Sixteen bytes from
`crypto/rand`, hex — no new dependency.

```
one Submit, three attempts    one key    one message
two Submit calls              two keys   two messages   (which is correct)
```

Backoff is exponential with jitter, and the constants live in
`internal/retry/const.go`. The server sends no timing hint — `TooManyErr`
carries a sentence and nothing else — so the wait is entirely the SDK's choice.

### The alternative not taken

gRPC has retry built in, configured through `WithDefaultServiceConfig` and a
JSON string. It is better tested than anything written here. It was rejected
because `WithRetry(n)` would mean generating that JSON at runtime, and an
opaque config string reads worse in this repository than eighty lines that can
be read and tested.

---

## Credentials

A customer registers each sending identity once, and then never mentions it
again:

```go
// once
c.Credentials.Register(ctx, srosha.Registration{
    Name:    "alerts",
    Default: true,
    Credential: srosha.APNsCredential{
        KeyID: "ABC1234567", TeamID: "TEAM123456",
        Topic: "com.acme.app", Key: p8, Environment: srosha.Sandbox,
    },
})

// every day — no credential in sight
c.Submit(ctx, srosha.Message{Routes: []srosha.Route{srosha.APNs(token)}})
```

The name is only needed when a channel has more than one identity:
`srosha.Email("a@b").From("marketing")`.

### Why typed rather than a json string

Today `Register` takes `config` as a json string whose shape differs per
channel — `{"key_id":…,"team_id":…,"topic":…,"environment":…}` for APNs,
`{"phone_number_id":…}` for WhatsApp, `{}` for three of them — and that shape
is documented nowhere a customer can read. A misspelt key compiles and fails
days later on the first send.

This is not the multiplication that killed `SendEmail`/`SendTelegram`. Those
were seven methods with identical bodies. These are seven genuinely different
shapes, and the type carries information rather than repeating it.

The set is closed by an unexported method on the interface, so nothing outside
the package can pose as a credential.

### The escape hatch

```go
srosha.RawCredential{Channel: "sms", Config: `{"line":"…"}`, Secret: token}
```

A newer server with a channel this SDK has never heard of does not leave a
customer stuck — the same instinct that made `Channel` a string.

### Secrets never reach a log

Every credential type has a `String()` that redacts, exactly as `sender.SMTP`
and `sender.APNs` already do in the server. A `%v` on a struct is where a p8
key escapes.

### One place the API can mislead

```
Rotate(ctx, id, secret)   a new secret, the same settings
Update(ctx, id, cred)     new settings, the same secret
```

`Update` takes a whole credential and sends **only its settings half**. A
secret set on it is silently ignored. Splitting every type in two would make
that impossible, at the cost of doubling seven types for a method that is
rarely called. The chosen answer is to say so in the doc comment rather than
add structure. Revisit if it bites.

---

## Testing

`bufconn` — gRPC's in-memory listener. A fake server, a client over a memory
pipe, no ports and no Docker. Possible from inside the SDK module because
the generated package carries the server stubs as well as the client ones.

```
client        the bearer header actually goes out
              TLS is the default; WithInsecure is explicit
errors        every code maps to its sentinel, and the message survives
retry         Unavailable is retried, InvalidArgument is not
              the same idempotency key on every attempt
              WithRetry(0) means one attempt
idempotency   a key is generated when the caller gave none
              two Submit calls produce two keys
types         time.Time both ways; zero means unset
              a channel this build does not know does not panic
iterator      walks pages, stops, and surfaces an error mid-walk
```

### The test only this layout allows

`APNsCredential` produces json. Nothing proves the server's `apns.ParseConfig`
accepts it — the SDK cannot import the server, because the dependency runs the
other way.

But the server can import the SDK:

```
sdk/go        APNsCredential{…}.Config()   →  json
                        ↓
main module   apns.ParseConfig(json)       →  must not error
```

Seven tests, one per channel, living in the main module. This is the seam where
an SDK and its service drift apart silently, and it is the concrete payoff of
keeping them in one repository. A separate repository could not have this test.

### A trap in CI

`go test ./...` in the main module **does not descend into a nested module**.
`make prepush` would skip the SDK entirely and say nothing. The targets must
run the SDK module explicitly:

```
make prepush  →  main module  +  (cd sdk/go && test, vet, lint)
```

---

## Publishing

| | |
| --- | --- |
| Tag | `sdk/go/v0.1.0` — the prefix is required for a module in a subdirectory |
| Install | `go get github.com/Serajian/srosha/sdk/go@v0.1.0` |
| Local | `replace` in the main `go.mod`; customers never see it |

`v0.x` until the API has settled. `v1` is a promise, and no customer has used
this yet.

## Documentation

```
sdk/go/README.md   connect, send one, ask what happened
example_test.go    examples that compile, so they cannot rot
doc comments       on everything exported. this is a public API
```

`docs/CONVENTIONS.md` needs a new clause: `sdk/` is code a **customer** imports,
it never sees `internal/`, and its dependencies are deliberately few. The file
defines `internal/`, `pkg/` and `cmd/` today and would otherwise have nothing
to say about a new top-level directory.

---

## Order of work

Four steps, each one leaving the repository green:

1. **Move the contract.** `buf.gen.yaml` points at `sdk/go`, `gen/` is deleted,
   `grpcsrv` imports the new path, the main `go.mod` gains its `require` and
   `replace`, and `make prepush` learns to descend into the nested module. No
   SDK code yet — this step is done when the server builds and passes exactly
   as it did before.
2. **The client and the sending path.** `New`, the options, transport, errors,
   retry, `Submit`, `Get`, `List` and the iterator, with the types they need.
   This is the half a customer uses every day.
3. **Setup.** Credentials and webhooks, including the typed credential set and
   the seven cross-module tests that prove each one parses on the server side.
4. **Ship.** README, compiled examples, doc comments, the `CONVENTIONS.md`
   clause for `sdk/`, and the first tag.

Steps 2 and 3 are independent of each other once 1 is done.

---

## Out of scope, and why

- **Webhook signature verification.** Decided against for this version. The
  first thing to reconsider.
- **An HTTP handler for receiving callbacks.** Would tie the SDK to a web
  framework.
- **Other languages.** The layout makes room for them; nothing else is done.
- **A `Limits` rpc.** A caller discovers the retention window only by being
  refused. A dedicated rpc would answer it up front, but that is new API
  surface and nobody has asked.
