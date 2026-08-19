# Srosha — Asynchronous Notification Service

**Engineering specification and implementation guide.**

This document is the single source of truth for the architecture, conventions
and open questions of this project. It supersedes the earlier `DESIGN.md` where
the two disagree; every such divergence is called out explicitly under
[§14 Divergences from the original DESIGN.md](#14-divergences-from-the-original-designmd).

Module path: `github.com/Serajian/srosha`
Go version: `1.26`

---

## Table of contents

1. [Purpose and scope](#1-purpose-and-scope)
2. [System architecture](#2-system-architecture)
3. [The dependency rule](#3-the-dependency-rule)
4. [Repository layout](#4-repository-layout)
5. [Naming conventions](#5-naming-conventions)
6. [Error handling](#6-error-handling)
7. [The domain layer](#7-the-domain-layer)
8. [Ports](#8-ports)
9. [Application services](#9-application-services)
10. [Configuration](#10-configuration)
11. [Infrastructure and adapters](#11-infrastructure-and-adapters)
12. [Messaging design](#12-messaging-design)
13. [Persistence and migrations](#13-persistence-and-migrations)
14. [Divergences from the original DESIGN.md](#14-divergences-from-the-original-designmd)
15. [Bootstrap and lifecycle](#15-bootstrap-and-lifecycle)
16. [Testing conventions](#16-testing-conventions)
17. [Tooling](#17-tooling)
18. [Implementation order](#18-implementation-order)
19. [Open decisions](#19-open-decisions)
20. [Rules for the implementing agent](#20-rules-for-the-implementing-agent)

---

## 1. Purpose and scope

A scalable, asynchronous notification service in Go supporting four delivery
channels: **email, Telegram, Bale, WhatsApp**.

A client submits a notification request; the service acknowledges immediately
and delivers out of band. Final status is retrieved by polling with the returned
id, or pushed to a registered webhook.

**In scope for the MVP**

- Text content only: plain text, Markdown, HTML
- Four channels behind one uniform `Sender` interface
- Per-source authentication, rate limiting and priority ceilings
- At-least-once delivery with per-channel retry
- Status callbacks (webhooks) signed with HMAC

**Explicitly out of scope**

- File attachments
- Scheduled / delayed sending
- Templating

---

## 2. System architecture

Two independently deployable binaries over a shared core.

| Binary | Responsibility |
| --- | --- |
| **gateway** | Accept requests (gRPC, plus REST via grpc-gateway), authenticate, rate-limit, apply the priority ceiling, persist, publish to the queue, return an immediate ACK |
| **dispatcher** | Consume from NATS JetStream, perform the actual send per channel, record the outcome, fire status webhooks |

Why split: independent scaling, isolation (a broken WhatsApp integration must
not stop request intake), and separate deploy/rollback.

### Request flow

```
Client
  │ gRPC / REST + auth token
  ▼
Gateway
  ├─ 1. authenticate; derive Source from the token, never from the body
  ├─ 2. rate-limit by source
  ├─ 3. idempotency check (source_id, idempotency_key)
  ├─ 4. load Source; resolve targets; clamp priority to the ceiling
  ├─ 5. generate ULID
  ├─ 6. INSERT notification (status = PENDING)
  ├─ 7. publish ONE DispatchEvent PER CHANNEL
  └─ 8. return ACK with id and effective priority

NATS JetStream (durable stream, subject per priority+channel)
  ▼
Dispatcher (durable consumer group)
  ├─ 1. consume one event (at-least-once)
  ├─ 2. load the notification; skip channels that are already settled
  ├─ 3. mark PROCESSING (increments attempts)
  ├─ 4. call the Sender for that channel
  ├─ 5. mark SENT or FAILED; persist that one delivery
  ├─ 6. on a transient failure, NAK with backoff; on a permanent one, stop
  └─ 7. when the aggregate reaches a terminal status, fire the webhook
```

---

## 3. The dependency rule

> Dependencies always point inward, from detail toward business rule.

```
cmd → bootstrap → { infra, adapter } → core/service → core/port → core/domain → core/shared
```

Hard constraints, enforced by review:

- `core/shared` imports **nothing internal**. Only stdlib and `pkg/errs`.
- `core/domain/*` imports only `core/shared` and `pkg/errs`. **No ports, no
  orchestration, no I/O, no third-party libraries.**
- `core/port` imports only `core/domain` and `core/shared`.
- `core/service` imports `core/port`, `core/domain`, `core/shared`.
- `infra/*` must **not** import `core`. If it needs to, translation logic has
  leaked into it and belongs in an adapter.
- `pkg/errs` is transport-neutral: no `net/http`, no `grpc/codes`.

Health check:

```bash
go list -deps ./internal/core/domain/... | grep -v '^github.com/Serajian/srosha\|^internal\|^errors\|^fmt\|^time\|^strings\|^net/mail\|^sort\|^unicode'
```

Anything unexpected in that output means something leaked.

---

## 4. Repository layout

```
srosha/
├── api/proto/notification/v1/
│   ├── common.proto            # enums: Priority, Channel, DeliveryStatus, Status
│   ├── notification.proto      # NotificationService
│   └── webhook.proto           # WebhookService
├── buf.yaml                    # proto module + deps + lint rules
├── buf.gen.yaml                # codegen plugins
├── gen/                        # protoc output; COMMITTED, never hand-edited
│
├── cmd/
│   ├── gateway/main.go         # signals + config + bootstrap + run. Nothing else.
│   └── dispatcher/main.go
│
├── internal/
│   ├── bootstrap/
│   │   ├── app.go              # App, Runner, Closer, reverse-order shutdown
│   │   ├── gateway.go          # wiring for the gateway binary
│   │   └── dispatcher.go       # wiring for the dispatcher binary
│   │
│   ├── config/
│   │   ├── load.go             # viper instance, bindEnvs, godotenv
│   │   ├── default.go          # default values only
│   │   ├── gateway.go          # GatewayConfig + LoadGateway + Validate
│   │   ├── dispatcher.go       # DispatcherConfig + LoadDispatcher + Validate
│   │   └── settings/
│   │       ├── app.go          # env, service name, shutdown timeout
│   │       ├── db.go
│   │       ├── mq.go
│   │       ├── grpc.go
│   │       ├── sender.go       # SMTP + per-channel provider credentials
│   │       ├── ratelimit.go
│   │       ├── webhook.go
│   │       ├── telemetry.go
│   │       └── secret.go       # Secret type, redacts in String()
│   │
│   ├── core/
│   │   ├── shared/             # value objects; zero internal imports
│   │   │   ├── id.go
│   │   │   ├── channel.go
│   │   │   ├── priority.go
│   │   │   └── errors.go
│   │   ├── domain/
│   │   │   ├── notification/   # entity.go, status.go, errors.go
│   │   │   ├── source/         # entity.go, errors.go
│   │   │   └── webhook/        # entity.go, errors.go
│   │   ├── port/               # interfaces the service layer consumes
│   │   │   ├── repository.go
│   │   │   ├── messaging.go
│   │   │   ├── sender.go
│   │   │   └── system.go
│   │   └── service/            # use cases; ONE FILE PER USE CASE, named as a verb
│   │       ├── submit.go
│   │       ├── query.go
│   │       ├── dispatch.go
│   │       ├── reconcile.go
│   │       ├── register.go     # webhook registration / deactivation / listing
│   │       ├── notify.go       # sending the status callback
│   │       └── fakes_test.go   # in-memory port fakes shared by the tests here
│   │
│   ├── adapter/
│   │   ├── api/grpc/           # see §19 open decision on renaming to grpcsrv
│   │   │   ├── register.go     # RegisterXServer calls; keeps gen/ out of bootstrap
│   │   │   ├── notification.go # NotificationService handlers
│   │   │   ├── webhook.go      # WebhookService handlers
│   │   │   ├── mapper.go       # proto <-> domain, both directions
│   │   │   ├── errors.go       # errs.Type -> codes.Code
│   │   │   └── interceptor/
│   │   │       ├── recovery.go   # outermost
│   │   │       ├── logging.go
│   │   │       ├── auth.go       # token -> Source, into context
│   │   │       └── ratelimit.go  # innermost of the four
│   │   ├── db/postgres/
│   │   │   ├── db.go           # DB interface satisfied by both *pgxpool.Pool and pgx.Tx
│   │   │   ├── notification.go
│   │   │   ├── source.go       # + cache
│   │   │   ├── webhook.go
│   │   │   ├── mapper.go       # row <-> domain
│   │   │   ├── query.go        # SQL constants
│   │   │   └── errors.go       # pg error code -> domain sentinel
│   │   ├── mq/nats/
│   │   │   ├── publisher.go
│   │   │   ├── consumer.go
│   │   │   ├── stream.go       # idempotent stream/consumer creation + JetStream iface
│   │   │   ├── subject.go      # (priority, channel) -> subject
│   │   │   └── codec.go        # DispatchEvent <-> bytes
│   │   ├── sender/
│   │   │   ├── registry.go
│   │   │   ├── email/sender.go
│   │   │   ├── telegram/{sender.go,errors.go}
│   │   │   ├── bale/{sender.go,errors.go}
│   │   │   └── whatsapp/{sender.go,errors.go}
│   │   ├── notifier/
│   │   │   ├── notifier.go     # port.WebhookNotifier
│   │   │   ├── doer.go         # Doer interface over *http.Client
│   │   │   └── signature.go    # HMAC signing
│   │   ├── auth/apikey.go
│   │   ├── ratelimit/memory.go # redis.go later, same package
│   │   └── system/
│   │       ├── idgen.go        # ULID
│   │       └── clock.go        # time.Now
│   │
│   └── infra/                  # builds and owns CONNECTIONS; returns concrete types
│       ├── database/postgres.go
│       ├── messagequeue/{nats.go,jetstream.go}
│       ├── httpclient/client.go
│       └── telemetry/{zerolog.go,prometheus.go}
│
├── pkg/errs/                   # error envelope; importable from outside the module
│   ├── type.go
│   └── error.go
│
├── migrations/                 # goose, sequential numbering
├── deployment/                 # docker-compose, Dockerfiles
├── Makefile
├── .env / .env.example
└── go.mod
```

### infra vs adapter

The distinction is precise and must be respected:

| | Responsibility | Example |
| --- | --- | --- |
| **infra** | Create and manage a **resource**: connection, pool, client, its retry and health | `pgxpool.New`, `MaxConns` tuning, NATS reconnect handlers |
| **adapter** | Implement a **port**: translate data between domain and that resource | `domain.Notification` → `INSERT`, row → entity |

`adapter/db/postgres` **receives** a `*pgxpool.Pool`; it never constructs one.

If a file under `infra/` ends up importing `core`, the boundary has been
violated — move that code to an adapter.

---

## 5. Naming conventions

**Packages are named by role; files are named by technology.**

```
infra/database/postgres.go        → database.NewPool(...)
infra/messagequeue/nats.go        → messagequeue.NewConn(...)
infra/telemetry/zerolog.go        → telemetry.NewLogger(...)
```

Callers see the role; opening the file reveals the technology. Adding Redis
later means `infra/database/redis.go`, with no import churn.

**Files in `core/service` are named after the use case, as a verb.**
`submit.go`, `dispatch.go`, `reconcile.go` — never `notification.go` or
`webhook.go`. In a package already called `service`, a noun carries no
information.

**Package names must be unique across any file's import graph.** Go permits
collisions but forces an alias at every call site, and `bootstrap/gateway.go`
imports nearly everything. Known collisions and their resolutions:

| Collision | Resolution |
| --- | --- |
| `adapter/db/postgres` vs `infra/postgres` | infra subpackage renamed to `database` ✅ |
| `adapter/mq/nats` vs `infra/nats` | infra subpackage renamed to `messagequeue` ✅ |
| `adapter/webhook` vs `core/domain/webhook` | adapter renamed to `notifier` ✅ |
| `adapter/api/grpc` vs `google.golang.org/grpc` | **unresolved** — see §19 |

`adapter/mq/nats` still collides with the `nats.go` library, but only inside
`publisher.go` and `consumer.go`. One alias in two files is acceptable.

---

## 6. Error handling

Two mechanisms, used together. Neither replaces the other.

### `pkg/errs` — the envelope

```go
type AppError struct {
    typ     Type   // classification: how to answer a caller
    message string // safe to return to a client; never leaks internals
    reason  error  // detail for logs only; may name columns, limits, providers
}
```

- `New(t Type, msg string)` — **two arguments**. No status code: that is a pure
  function of `Type`, and storing it would create a second source of truth.
- `WithErr(err)` / `WithStr(s)` — return a **copy**, and **accumulate** onto any
  existing reason. This lets package-level template errors exist safely.
- `chainReason` uses `fmt.Errorf("%w: %w", ...)`, producing a multi-error whose
  `Unwrap` returns a slice. **Never change this to `%v`** — every `errors.Is` in
  the codebase would silently start returning false, with no compile error.
- `As`, `IsType`, `TypeOf`. `TypeOf` maps anything unclassified to
  `ErrInternal`: an unclassified error at a boundary is a bug or an
  infrastructure fault, and both are safest reported as internal.

`Type` values: `ErrUnknown, ErrInvalidInput, ErrUnauthorized, ErrForbidden,
ErrNotFound, ErrDuplicateEntry, ErrTooMany, ErrInternal, ErrUnavailable,
ErrTimeout`.

### Sentinels — the identity

An `AppError` is created fresh each time and therefore has no identity;
`errors.Is` cannot distinguish "invalid target" from "empty body" since both are
`ErrInvalidInput`. Every failure therefore attaches a sentinel:

```go
return errs.InvalidInputErr("invalid id").
    WithErr(shared.ErrInvalidID).
    WithStr(fmt.Sprintf("expected %d chars, got %d", idLength, len(s)))
```

```go
errors.Is(err, shared.ErrInvalidID)     // exact cause — for branching logic
errs.IsType(err, errs.ErrInvalidInput)  // classification — for the response
ae.Reason()                             // detail — for the log
```

Sentinels live in the package that **produces** them:
`shared/errors.go`, `domain/notification/errors.go`, `domain/source/errors.go`.
Never in one central file.

### Classification rules

| Cause | Type |
| --- | --- |
| Bad client input (empty body, no channels, duplicate channel, malformed target) | `ErrInvalidInput` |
| Valid request, insufficient permission (inactive source, custom target not allowed) | `ErrForbidden` |
| **Our own** bug: missing generated id, nil source, missing timestamp, unrequested channel, illegal state transition | `ErrInternal` |
| Idempotency key reuse | `ErrDuplicateEntry` |
| Rate limit | `ErrTooMany` |

The third row matters: the service generates the id and injects the clock, so an
empty one is never the caller's fault and must not be reported as something they
could fix.

### message vs reason

`message` is client-facing and must not leak:

- the rejected value (it may be someone's phone number or address)
- our accepted formats (turns the API into a probe)
- configuration such as the source's default targets or its id

All of that goes in `reason`. There are tests asserting exactly this
(`TestInvalidTargetMessageHidesTheValue`,
`TestResolveTargetMessageHidesConfiguration`); keep them passing.

### Translation at the boundaries

Sentinels do **not** survive the wire — `errors.Is` works only in-process.

```go
// adapter/db/postgres/errors.go
pg 23505 on uk_source_idempotency → notification.ErrDuplicateIdempotencyKey

// adapter/api/grpc/errors.go
errs.ErrInvalidInput   → codes.InvalidArgument
errs.ErrUnauthorized   → codes.Unauthenticated
errs.ErrForbidden      → codes.PermissionDenied
errs.ErrNotFound       → codes.NotFound
errs.ErrDuplicateEntry → codes.AlreadyExists
errs.ErrTooMany        → codes.ResourceExhausted
errs.ErrUnavailable    → codes.Unavailable
errs.ErrTimeout        → codes.DeadlineExceeded
default                → codes.Internal
```

grpc-gateway derives HTTP status from the gRPC code, so no separate HTTP table
is needed unless a hand-written REST API is added later.

---

## 7. The domain layer

### `core/shared`

Value objects used by more than one aggregate. Complete and green.

- **`ID`** — `type ID string`, ULID-shaped (26 chars, Crockford base32). The
  package deliberately does **not** import a ULID library; generation sits
  behind `port.IDGenerator` so tests can inject a deterministic sequence.
- **`Channel`** — `email | telegram | bale | whatsapp`. `ValidateTarget` checks
  the **shape** of a destination per channel (email via `net/mail`, Telegram and
  Bale as a numeric chat id or `@username`, WhatsApp as E.164). It never checks
  **existence**: that would put a network call in the domain, and only the
  sender can discover it. The `switch` has a `default` branch so a newly added
  channel constant fails loudly instead of accepting anything.
- **`Priority`** — `int8`, ordered `Normal < High < Critical`. An integer
  because the core rule is a comparison. Lives in `shared` rather than in
  `notification` because `source.MaxPriority` needs it too — otherwise
  `source → notification → source` is an import cycle. `Clamp(max)` is the
  mechanical half of the silent downgrade; deciding to clamp rather than reject
  belongs to the notification aggregate.

`AllChannels()` returns a fresh slice each call: a package-level var would let
one caller's `sort` corrupt it for everyone.

### `core/domain/notification`

**`status.go`** — zero imports. Two distinct vocabularies:

`DeliveryStatus` (one channel), with the transition table as data:

```
PENDING    → PROCESSING, FAILED
PROCESSING → SENT, FAILED
SENT       → DELIVERED, FAILED
FAILED     → PROCESSING          (retry)
DELIVERED  → ∅                   (terminal)
```

- `PENDING → FAILED` is allowed: a channel can fail with zero attempts
  (message expired, no sender registered). Attempts stays 0, which is the truth.
- `SENT → FAILED` is allowed: SENT means the provider accepted it, not that it
  arrived. A later bounce is this edge.
- `PROCESSING → PROCESSING` is **absent**. That is the duplicate-send guard.
  A worker that dies mid-send leaves a delivery stuck in PROCESSING; recovery is
  the two-step `PROCESSING → FAILED → PROCESSING`, driven by the reconciler.

`Status` (whole notification): `PENDING, PROCESSING, COMPLETED, PARTIAL,
FAILED`. **No transition table**, because it is never assigned — always derived
by the unexported `aggregate(total, succeeded, failed, started)`. It takes four
counts rather than the deliveries so the rule stays a pure function, testable
independently of `Delivery`'s shape.

`IsSettled()` counts `FAILED` as settled, because deciding whether a retry is
allowed needs the attempt count and the configured limit — policy that belongs
to the dispatch service, not to a status value.

**`entity.go`** — the aggregate root.

Field visibility is the design statement: **exported fields have no invariant;
unexported ones do.**

```go
type Notification struct {
    ID, SourceID, IdempotencyKey, Title, Body string / shared.ID
    RequestedPriority, EffectivePriority      shared.Priority
    Metadata                                  map[string]string
    CreatedAt                                 time.Time

    status     Status      // DERIVED from deliveries
    deliveries []Delivery  // may only move along the state machine
}
```

Hiding only those two prevents `n.Status = StatusCompleted` from bypassing
recomputation, without producing nine accessors.

- `New(id, src, req, now)` — the signature says who supplies what: we provide
  the id, the source and the clock; `Request` is what the client asked for.
- Validation order is load-bearing: our own errors first (`id`, `src`, `now`),
  because `src` is dereferenced immediately after.
- `Attempts` increments on transition **to PROCESSING**, not on failure, so it
  means "tries started" — what a retry limit must compare against. Counting on
  failure would miss an attempt whose process died mid-send, letting a message
  retry forever.
- `Restore(base, status, deliveries)` skips validation deliberately: a row valid
  when written must stay loadable when a rule tightens. Only the repository
  calls it.
- `Deliveries()` returns a copy. `find()` returns a pointer into the backing
  array and stays unexported for exactly that reason.
- `UnsettledChannels()` is what makes redelivery safe: the dispatcher asks which
  channels are still open instead of resending everything.

### `core/domain/source`

Fields are **exported**, unlike the notification aggregate — and that is not an
inconsistency. `Source` has no derived state and no lifecycle; it is
configuration loaded from a row, so there is nothing for accessors to protect.

`ResolveTarget(channel, requested)` implements the **hybrid target rule**:

```
explicit target + AllowCustomTarget      → use it, after validating its shape
explicit target + !AllowCustomTarget     → refuse (Forbidden)
no explicit target                       → fall back to DefaultTargets[channel]
no explicit target, no default           → refuse (InvalidInput)
```

`AllowCustomTarget = false` is a security control, not a convenience toggle: a
leaked API key for a system source then cannot be used to message strangers.
Stored defaults are re-validated on read, because a row written before a rule
tightened never passed through the current validation.

### `core/domain/webhook`

Entity for a registered callback: id, source id, callback URL, HMAC secret,
active flag. Not yet written.

---

## 8. Ports

Interfaces are defined in the package that **consumes** them.

- Ports consumed by `core/service` live in `core/port`.
- Interfaces over **infra outputs** live in the **consuming adapter**, because
  their signatures are unavoidably technology-flavoured
  (`pgx.Row`, `pgconn.CommandTag`) and would poison core:
  - `adapter/db/postgres/db.go` — `DB`, satisfied by both `*pgxpool.Pool` and
    `pgx.Tx`, so one repository works inside and outside a transaction
  - `adapter/mq/nats/stream.go` — `JetStream`
  - `adapter/notifier/doer.go` — `Doer`
- `infra/*` returns **concrete types only**. A provider must not define its own
  abstraction.

Not everything needs an interface. The bar: **either it has two implementations,
or it must be faked in a test.** A `*slog.Logger` needs neither — it is already
an abstraction over `slog.Handler`.

### `port/repository.go`

```go
type NotificationRepository interface {
    Create(ctx, *notification.Notification) error
    FindByID(ctx, shared.ID) (*notification.Notification, error)
    FindByIdempotencyKey(ctx, sourceID, key string) (*notification.Notification, error)
    SaveDelivery(ctx, id shared.ID, d notification.Delivery) error
    ListStuck(ctx, olderThan time.Duration, limit int) ([]*notification.Notification, error)
}
```

There is deliberately **no `Save(*Notification)`**. Whole-aggregate persistence
is a read-modify-write, and with per-channel dispatch two workers update the
same notification concurrently — the second write would clobber the first.
Updates are expressed per delivery so the adapter can emit a single statement
touching only that channel's row.

The aggregate `status` column is recomputed **by the adapter, from the row's own
data**, inside the same statement — never from the caller's possibly-stale
in-memory copy.

`ListStuck` must cover both stalled cases:
- notifications in `PENDING` past a cutoff (the dual-write gap)
- deliveries in `PROCESSING` past `AckWait` (a dead worker)

Also: `SourceRepository` (caching belongs in the adapter, not in the port) and
`WebhookRepository`.

### `port/messaging.go`

```go
type DispatchEvent struct {
    NotificationID shared.ID
    SourceID       string
    Channel        shared.Channel
    Priority       shared.Priority
}

type Publisher  interface { Publish(ctx, DispatchEvent) error }
type Subscriber interface { Subscribe(ctx, EventHandler) error; Close() error }
```

`Subscriber` is a **driving** port — unlike everything else here, the
implementation calls into the core. Architecturally it sits where the gRPC
server sits, not where the repository sits.

### `port/sender.go`

```go
type Sender interface {
    Channel() shared.Channel
    Send(ctx, Message) error
}
type SenderRegistry interface { For(shared.Channel) (Sender, error) }
```

`SendError` classifies a failure as **permanent or transient**, with an optional
`RetryAfter` hint. This is what stops the retry loop from being useless:
`chat not found` fails identically on all five attempts and must be recorded
`FAILED` immediately, while HTTP 429 or a dropped connection deserves a retry.
Mapping each provider's errors into this shape is precisely the knowledge each
sender adapter exists to hold — hence a separate `errors.go` per channel.

An **unclassified** error is treated as transient: an unknown failure is more
often a blip than a dead end.

### `port/system.go`

`IDGenerator`, `Clock`, `RateLimiter`, `WebhookNotifier`.

`Clock` and `IDGenerator` exist so the domain never reads ambient state:
`New` takes `now time.Time` and a generated id as arguments.

---

## 9. Application services

One file per use case, in `package service`, flat.

| File | Use case | Binary |
| --- | --- | --- |
| `submit.go` | Validate, persist, publish; return id + effective priority | gateway |
| `query.go` | Read full status including per-channel state | gateway |
| `dispatch.go` | Consume one event, send, record the outcome | dispatcher |
| `reconcile.go` | Republish stuck PENDING; fail-and-retry stuck PROCESSING | see §19 |
| `register.go` | Register / deactivate / list webhooks | gateway |
| `notify.go` | Fire the status callback on a terminal status | dispatcher |

Rule of thumb for placement: **if a function calls a port interface, it is a use
case and belongs here. If it decides something purely in memory, it is a domain
rule and belongs in `core/domain`.**

`dispatch.go` must treat `notification.ErrInvalidTransition` as **success**, not
failure — it is the expected outcome of a JetStream redelivery:

```go
if errors.Is(err, notification.ErrInvalidTransition) {
    return nil // already done; ack and move on
}
```

Retry policy (is this error worth another attempt? has the limit been reached?)
is a pure decision and may be extracted to
`domain/notification/retrypolicy.go` once it grows beyond a couple of lines.

---

## 10. Configuration

**Environment variables only. No config file.** Loaded via `spf13/viper`.

### Per-binary configs

```go
type GatewayConfig struct {
    App, GRPC, DB, MQ, RateLimit, Telemetry settings.X
}
type DispatcherConfig struct {
    App, DB, MQ, Sender, Webhook, Telemetry settings.X
}
```

A single shared config would make the gateway fail at startup for a missing
Telegram token it never uses. Each binary's dependencies are now readable from
its type.

### Viper traps — all four apply here

1. **`AutomaticEnv` does not work with `Unmarshal`.** Viper only fills keys it
   already knows. With no config file it knows none, and `Unmarshal` silently
   returns an empty struct. Every key must be registered.
2. Tags are **`mapstructure`**, not `env`.
3. Keys nest with dots; env vars do not. `SetEnvKeyReplacer(".", "_")` maps
   `db.dsn` → `NOTIF_DB_DSN`.
4. **Never use the viper singleton.** Always `viper.New()`.

### Registration by reflection, not by hand

Listing every key manually in `default.go` means a new field silently stays zero
the day someone forgets a line. Instead, walk the struct once and `BindEnv`
every path:

```go
func bindEnvs(v *viper.Viper, spec any, parts ...string)
```

Division of responsibility:

- `bindEnvs` → "this key exists"
- `setDefaults` → "and if you don't supply it, use this"
- `Validate()` → "and this one you must supply"

`Validate()` is **mandatory**, not optional: viper has no `required` tag, so it
is the only thing standing between a missing DSN and a failure at first use.
Fail at startup, not on the third request.

### Secrets

```go
type Secret string
func (s Secret) String() string { return "[REDACTED]" }
func (s Secret) Reveal() string { return string(s) }
```

Every token, password and DSN uses this type, so `fmt.Printf("%v", cfg)` is
safe and only deliberate `.Reveal()` calls expose a value.

### `.env`

Viper reads process environment, not files. Load `.env` best-effort with
`joho/godotenv` at the top of `config` — it does not overwrite variables already
set, so production is unaffected.

---

## 11. Infrastructure and adapters

### infra

Constructors returning concrete types, with real logic behind them: pool tuning,
connection retry, reconnect handlers, health checks. If a file here is a
three-line wrapper with nothing else, merge it into the adapter — the boundary
is only worth its cost when something lives behind it.

### Logging

**The type `core/service` sees is `*slog.Logger`.** If zerolog is used, it is
wired underneath as a `slog.Handler` in `infra/telemetry/zerolog.go`. Core never
imports zerolog.

Metrics and tracing must **not** appear in `core`. They belong in adapters, as
gRPC interceptors and NATS middleware, so business logic contains no
observability code while everything is still measured.

### gRPC server

Per the agreed split, there is **no `server.go` in the adapter**: constructing,
running and gracefully stopping the server is `bootstrap`'s job. The adapter
keeps only a thin `register.go`, so the generated protobuf package is imported
by the API layer rather than by the wiring layer.

Interceptor order is fixed, outermost first:

```
recovery → logging → auth → ratelimit → handler
```

`recovery` must be outermost or a panic inside `logging` kills the server.
`ratelimit` must follow `auth` because the quota is per source and the source is
unknown before authentication.

`auth` derives the Source from the token and places it in the context. The
Source is **never** read from the request body — with this design, spoofing is
structurally impossible rather than merely discouraged.

---

## 12. Messaging design

### One event per channel

The gateway publishes **one `DispatchEvent` per requested channel**, not one per
notification. Consequences, all intended:

- a slow WhatsApp call cannot delay the email
- a Telegram failure retries alone, without re-sending what already succeeded
- the dispatcher's logic collapses to "send this one channel"

This is what makes `SaveDelivery` (rather than whole-aggregate save) necessary.

### Thin events

The event carries **identifiers only** — no title, no body. The dispatcher must
load the row anyway to learn which channels are still unsettled, so duplicating
the payload would buy only a stale copy and a much larger stream.

### Subjects

Priority must affect **scheduling**, not just be stored. A single stream and
consumer would leave a `CRITICAL` message queued behind five thousand `NORMAL`
ones. Split the subject and give each its own consumer with its own worker
count:

```
notify.critical.telegram
notify.high.email
notify.normal.email
...
```

### Retry belongs to JetStream

Do **not** implement backoff with `time.Sleep` in the dispatcher: it blocks a
worker and loses all retry state on restart. Use JetStream's own machinery —
`MaxDeliver`, `BackOff []time.Duration`, `NakWithDelay`. On a transient
`SendError`, NAK (honouring `RetryAfter` when present); on a permanent one,
record `FAILED` and ack.

`AckWait` must exceed the send timeout, or a message is redelivered while the
first attempt is still in flight.

After `MaxDeliver`, messages must go to a **DLQ** with a defined terminal state.

---

## 13. Persistence and migrations

### Tables

- **`sources`** — id, name, max_priority, is_active, **allow_custom_target**,
  timestamps
- **`source_channels`** — (source_id, channel) → target, is_active. The default
  half of the hybrid target rule.
- **API keys** — needed by `adapter/auth/apikey.go`. Absent from the original
  design; store a hash, never the key.
- **`webhooks`** — id, source_id, callback_url, secret, is_active, timestamps
- **`notifications`** — id (ULID, VARCHAR(26)), source_id, idempotency_key,
  title, body, requested_priority, effective_priority, status, metadata JSONB,
  timestamps, with `UNIQUE (source_id, idempotency_key)`
- **Per-channel delivery state** — see the open decision below

`UNIQUE (source_id, idempotency_key)` behaves correctly with NULL keys in
Postgres: NULLs are distinct, so many rows may omit the key.

### Per-channel state: JSONB vs child table

The original design used a `recipients JSONB` column. Two later decisions
undermine it:

1. **Concurrent writes.** With per-channel fan-out, four workers update the same
   row simultaneously. JSONB requires `jsonb_set` plus in-statement recomputation
   of the aggregate status, or accepts lost updates.
2. **`ListStuck` becomes impractical.** Finding deliveries stuck in PROCESSING
   requires a per-delivery `updated_at`, which in JSONB is a nested field that
   indexes poorly and compares awkwardly.

A child table makes both trivial:

```sql
notification_deliveries (
    notification_id, channel, target,
    status, attempts, last_error,
    created_at, updated_at,
    PRIMARY KEY (notification_id, channel)
)
```

Per-row locking removes lost updates entirely, and the reconciler query becomes
`WHERE status = 'PROCESSING' AND updated_at < now() - interval '5 minutes'`.

Cost: a join on read, and a divergence from the original design.
**Recommendation: the child table.** Final call is open — see §19.

### Indexes

```sql
CREATE INDEX ON notifications (status);
CREATE INDEX ON notifications (created_at DESC);
CREATE INDEX ON notification_deliveries (status, updated_at);
CREATE INDEX ON webhooks (source_id);
```

Drop the `uuid-ossp` extension from the original migration: ULIDs are generated
in the application, so it is dead weight.

### goose

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
goose create init sql -s      # -s gives sequential numbering, not timestamps
```

```
GOOSE_DRIVER=postgres
GOOSE_DBSTRING=postgres://...
GOOSE_MIGRATION_DIR=migrations
```

File format:

```sql
-- +goose Up
CREATE TABLE ...;

-- +goose Down
DROP TABLE ...;
```

**Trap:** goose splits on `;`, which breaks any PL/pgSQL function body. Wrap
those explicitly:

```sql
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
```

Migrations run as a **separate deployment step**, not on service startup —
several replicas racing for the lock at boot is not worth the convenience.

---

## 14. Divergences from the original DESIGN.md

| Original | Now | Reason |
| --- | --- | --- |
| One message per notification | One per channel | Independent retry and scaling per channel |
| `recipients JSONB` | Child table (recommended) | Concurrent writes; reconciler queries |
| Single status enum | `DeliveryStatus` and `Status` split | `PARTIAL` is meaningless per channel; `DELIVERED` meaningless per notification |
| No recipient addressing | Hybrid resolution + `source_channels` | The original had no way to say where to send |
| Single stream | Subject per priority + channel | Priority was stored but had no effect |
| Backoff in application code | JetStream `MaxDeliver` + `BackOff` | Sleeping blocks workers and loses state on restart |
| No API key storage | API key table | Auth was specified but unstorable |
| No dual-write handling | `ListStuck` + reconciler | Silent message loss between INSERT and publish |
| `uuid-ossp` extension | Removed | Unused; ULIDs come from the app |
| No error taxonomy | `pkg/errs` + sentinels | — |

---

## 15. Bootstrap and lifecycle

`main` handles only signals, config and "build and run":

```go
func main() {
    if err := run(); err != nil { slog.Error("fatal", "err", err); os.Exit(1) }
}

func run() error {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    cfg, err := config.LoadGateway()
    if err != nil { return err }

    app, err := bootstrap.NewGateway(ctx, cfg)
    if err != nil { return err }
    defer app.Close(context.Background())

    return app.Run(ctx)
}
```

`bootstrap.NewGateway` owns all wiring. **No DI framework.** In Go, `main` (here
delegated to `bootstrap`) *is* the injection container; a hand-written
constructor of ~30 lines states plainly what is connected to what. Revisit
`google/wire` only if that function passes ~200 lines.

**Shutdown runs in reverse order of construction.** Closers are pushed onto a
stack: the gRPC server stops accepting first, then NATS, then the database pool
last. Closing the pool first would kill in-flight requests.

`*pgxpool.Pool` has `Close()` with no context and no error, so it does not
satisfy a `Closer` interface directly — a small adapter in `bootstrap` bridges
it. That is bootstrap's job.

Run concurrent servers with `errgroup.WithContext`, so one failure cancels the
rest.

---

## 16. Testing conventions

- **External test packages** (`package foo_test`) by default, so tests exercise
  only the exported surface. Use an internal test package **only** when the
  subject is genuinely unexported — `deliveryTransitions` and `aggregate` are
  the current examples.
- **Table-driven tests** with named cases.
- **Assert both layers of an error**: `errors.Is` for the sentinel and
  `errs.IsType` for the classification.
- **Fakes over mocks.** `core/service/fakes_test.go` holds in-memory
  implementations of the ports; no Postgres or NATS in a use-case test.
- **Deterministic time and ids** via injected `Clock` and `IDGenerator`.
- Tests that guard a subtle property must say so in a comment. Existing
  examples worth preserving:
  - the priority constants stay ordered (nothing else would fail to compile)
  - `AllChannels()` returns a fresh slice
  - `Deliveries()` returns a copy
  - error messages do not leak targets or configuration
  - `Restore` still loads a row the current constructor would reject
- `go test -race ./...` is the standard command; it earns its cost once the
  dispatcher has concurrent workers.

---

## 17. Tooling

### buf

Chosen over raw protoc for three reasons: declarative plugin configuration,
dependency management for `googleapis` (needed by grpc-gateway), and breaking
change detection.

```bash
buf lint
buf generate
buf breaking --against '.git#branch=master'    # in CI
```

Run `buf config init` and adapt — the config schema differs between buf major
versions, so do not copy a `version:` block blindly.

`gen/` is committed so a fresh clone builds without protoc installed.

### Makefile

```makefile
include .env
export

MIGRATE = goose -dir $(GOOSE_MIGRATION_DIR)

migrate-up:     ; $(MIGRATE) up
migrate-down:   ; $(MIGRATE) down
migrate-status: ; $(MIGRATE) status
migrate-create: ; $(MIGRATE) create $(name) sql -s

proto:          ; buf generate
lint:           ; golangci-lint run
test:           ; go test -race ./...
build:          ; go build -o bin/ ./cmd/...
```

### Repository hygiene

`.idea/` is in `.gitignore` but its files were committed earlier; run
`git rm -r --cached .idea` once. `.env` must never be committed;
`.env.example` documents the variables.

**Never configure git author/committer identity on this repository.**
The default branch is `master`.

---

## 18. Implementation order

Layers 1–5 are pure and small, so writing them fully is safe. From step 6 on,
work in **vertical slices** — a thin path all the way through — rather than
completing each layer horizontally, so something runs and gives feedback early.

```
1. go.mod, Makefile, .gitignore, .env.example        ✅ done
2. pkg/errs                                          ✅ done
3. core/shared                                       ✅ done
4. core/domain/notification, source, webhook         🔶 in progress (owner: user)
5. core/port
────────────────────────────── core is fully testable with zero infrastructure
6. migrations (goose)
7. infra/database → adapter/db/postgres
8. core/service/submit.go + fakes_test.go
9. api/proto + buf → adapter/api/grpc → bootstrap/gateway.go
   ✅ the gateway boots and writes a row; verify with grpcurl
────────────────────────────── first complete vertical slice
10. infra/messagequeue → adapter/mq/nats (publisher)
11. adapter/mq/nats (consumer) + core/service/dispatch.go
12. first sender (Telegram — easiest to test by hand)
    ✅ end-to-end delivery works
────────────────────────────── second vertical slice
13. remaining senders, webhooks, reconciler, retry/DLQ, observability
```

---

## 19. Open decisions

Resolve each before writing the code that depends on it.

1. **Per-channel state: `recipients` JSONB or a `notification_deliveries` child
   table?** Blocks the first migration. *Recommendation: child table.*
2. **API key table in the first migration or a later one?**
3. **`source_channels` in the first migration or a later one?**
4. **Does `notification` keep importing `source`?** Today
   `New(id, *source.Source, req, now)` couples two aggregates at package level.
   The alternative passes resolved values (`sourceID`, `ceiling`,
   `[]ResolvedTarget`) so `notification` depends only on `shared`, with
   `EnsureActive` and `ResolveTargets` moving to `source`. *The user has taken
   ownership of the domain layer and will decide.*
5. **Rename `adapter/api/grpc` → `grpcsrv`?** Without it, every file importing
   `google.golang.org/grpc` needs an alias.
6. **Where does the reconciler run?** Goroutine in the gateway (guarded by
   `pg_try_advisory_lock` so only one replica works at a time), in the
   dispatcher, or a third `cmd/reconciler` binary. *Recommendation: gateway
   goroutine with the advisory lock; move to a CronJob later if needed.*
7. **`expires_at` on notifications?** A CRITICAL alert delivered six hours late
   is worse than not delivered.
8. **Per-channel content.** One `body` for all channels does not survive
   contact with reality: WhatsApp's Business API forbids free-form text outside
   the 24-hour window and requires approved templates; Telegram caps at 4096
   characters; email needs a subject. Decide whether to add per-channel
   overrides or accept the limitation in the MVP.
9. **Target normalisation.** `+98 912 123 4567` is rejected today because E.164
   forbids spaces. Normalise in `adapter/api/grpc/mapper.go` (keeps the domain
   strict) or in the domain. *Recommendation: at the boundary.*
10. **Distributed rate limiting.** An in-memory counter is meaningless once the
    gateway is scaled horizontally; Redis or NATS KV will be needed.

---

## 20. Rules for the implementing agent

**Do not**

- Add a dependency to `core/domain` beyond stdlib, `core/shared` and `pkg/errs`.
- Export `Notification.status` or `Notification.deliveries`, or return the
  internal slice from `Deliveries()`.
- Assign the aggregate status anywhere but `recomputeStatus`.
- Call `ParseID`, `ParseChannel` or `ParsePriority` on the read path from our own
  database. Convert directly; see the rationale on `Restore`.
- Put an HTTP status or a gRPC code in `pkg/errs`.
- Return an error without a sentinel attached via `WithErr`.
- Put the rejected value, our accepted formats, or source configuration into an
  error `message` — that is what `reason` is for.
- Use `time.Now()` or generate an id inside `core`. Both are injected.
- Use the viper singleton, or a package-level logger.
- Implement retry backoff with `time.Sleep` in the dispatcher.
- Read the Source from a request body.
- Configure a git author or committer identity, or create a `main` branch.

**Do**

- Write the test alongside the file, not after the layer.
- Grow `errors.go` one sentinel at a time, when the code that returns it is
  written — not in advance.
- Keep helper functions in the file of their **only** caller, unless the helper
  defines the meaning of a type (`aggregate` belongs with `Status`; a
  single-use error constructor does not).
- Ask before resolving anything in §19.
```
