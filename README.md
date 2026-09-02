<p align="center">
  <img src="docs/assets/brand/srosha-hero.png" alt="Srosha — async notification delivery across Email, Telegram, Bale, WhatsApp, Matrix, Gotify, FCM and APNs" width="100%">
</p>

<p align="center">
  <a href="https://go.dev"><img alt="Service: Go 1.26" src="https://img.shields.io/badge/service-Go%201.26-33A5DE?labelColor=0F172A"></a>
  <a href="sdk/go/README.md"><img alt="SDK: Go 1.25" src="https://img.shields.io/badge/SDK-Go%201.25-33A5DE?labelColor=0F172A"></a>
  <img alt="Status: in development" src="https://img.shields.io/badge/status-in%20development-4934A2?labelColor=0F172A">
  <img alt="Channels: 8" src="https://img.shields.io/badge/channels-8-3256AE?labelColor=0F172A">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-317DC6?labelColor=0F172A"></a>
</p>

**Srosha** is an asynchronous notification service written in Go. A client submits
a notification once; the service acknowledges immediately and delivers it out of
band across eight channels — **Email, Telegram, Bale, WhatsApp, Matrix, Gotify,
FCM and APNs** — with at-least-once delivery and per-channel retry. Final status is retrieved by
polling with the returned id, or pushed to a registered webhook signed with HMAC.

The name comes from *Soroush* (سروش), the messenger in Persian culture — hence
the folded-paper bird: the message itself, folded into a crane that flies and
delivers.

## How it works

<p align="center">
  <img src="docs/assets/brand/architecture.svg" alt="Architecture: a client submits to the gateway over gRPC; the gateway persists to PostgreSQL and publishes to NATS JetStream; the dispatcher consumes the queue, sends over the eight channels, records the outcome, and fires a signed status webhook back to the client" width="100%">
</p>

Four binaries from one image, sharing one core:

| Binary | Responsibility |
| --- | --- |
| **gateway** | Accepts requests over gRPC, authenticates, rate-limits, persists, publishes to the queue, returns an immediate ack |
| **dispatcher** | Consumes from NATS JetStream, performs the actual send per channel, records the outcome, fires status webhooks |
| **console** | Serves two web surfaces to people rather than programs: a customer portal for keys, sending identities and callbacks, and an operator's admin panel. Separated by host and by cookie, never by network |
| **migrate** | Runs the schema to completion and exits. Everything else waits on it, so a failed migration stops a release instead of letting new code meet a schema it does not expect |

The first split is the load-bearing one: intake and delivery scale and fail
independently, and a broken channel integration must never stop a request being
accepted.

Under the hood, the service follows a hexagonal architecture — the domain core
knows nothing about gRPC, PostgreSQL or NATS, and every technology hangs off a
port. The full design is documented in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Talking to it

gRPC is the only way to send anything, so every client library is a gRPC client
and `api/proto/notification/v1` is the one contract behind all of them. The
console serves HTML to people, which is not a second API: it has no contract, no
versioning and no client.

```bash
go get github.com/Serajian/srosha/sdk/go
```

The SDK needs Go **1.25**, not the 1.26 this service is built with. That is
deliberate: a client library should never make you upgrade your toolchain to
call a service.

```go
c, _ := srosha.New(ctx, "srosha.acme.test:443", apiKey)
defer c.Close()

c.Submit(ctx, srosha.Message{
    Title: "Your order shipped",
    Body:  "Tracking: 123",
    Routes: []srosha.Route{
        srosha.EmailTo("a@b.test"),
        srosha.TelegramTo("123456789"),
    },
})
```

Sending identities are registered once and never mentioned again; a message
names a channel, not an identity. See [`sdk/go`](sdk/go/README.md), and
[`sdk/`](sdk/README.md) for the rule every language SDK follows.

## Getting started

Requires Go 1.26, Docker, and `make`.

```bash
# start the local dependencies (PostgreSQL + NATS with JetStream)
make dev-up

# run the binaries locally
make run-gateway
make run-dispatcher
make run-console
```

Configuration is environment variables only, prefixed `NOTIF_`; every key with
its default is documented in [`.env.example`](.env.example). `make help` lists
every build, test and migration target.

## Project layout

```
cmd/            entrypoints: gateway, dispatcher, console, migrate
api/proto/      protobuf definitions (buf)
internal/       everything that knows what srosha is
  core/         domain + use cases — no infrastructure imports
  adapter/      driving (gRPC, HTTP) and driven (DB, queue, providers) adapters
  infra/        one package per technology: connect, health-check, close
  registry/     the only place a technology is opened
  bootstrap/    wires a binary together and shuts it down in order
  config/       environment configuration, loaded once at startup
migrations/     the schema, compiled into the migrate binary
public/         templates and assets the console serves, embedded
pkg/            generic packages with zero domain knowledge
sdk/            what a customer imports — one module per language
deployment/     the Dockerfile, and the infrastructure applied by hand
docs/           architecture, conventions, config, change reports
```

`make arch-check` enforces the dependency rule on every commit.

## Status

Srosha is under active development and not yet released, but it runs. All four
binaries are deployed and serving: the gRPC API, the customer portal, the admin
panel, and migrations on every release.

Four of the eight channels have carried a real message end to end — **Email,
Telegram, Bale and Gotify**. The other four are implemented and untested against
a live provider: Matrix and WhatsApp need an account, and FCM and APNs need a
real mobile app to issue a device token, which is a thing no test can fake.

Scope is deliberately narrow: text content only (plain, Markdown, HTML), no
attachments, no scheduling, no templating.

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — the binding design document
- [Conventions](docs/CONVENTIONS.md) — the rules every change follows
- [Config](docs/CONFIG.md) — every address, port, path and key in one place
- [Change reports](docs/changes/) — this project's memory, one file per change

## License

[MIT](LICENSE)
