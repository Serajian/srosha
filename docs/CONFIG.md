# CONFIG

The single source for this repository's **data**: addresses, hosts, ports, paths,
stage names, registries, image names, branch names, environment keys, build targets.

Read a value from here instead of grepping the tree or guessing. When a new one is
learned, add it here in the same commit as the change that introduced it.

**Never a secret.** No passwords, tokens, keys, or connection strings containing
credentials. Those live in `.env`, which is git-ignored, and in the Dokploy
Environment tab. This file records **names and locations, not values**.

Facts about the running system come from `docs/reference/srosha-infra-deploy.md`,
which records infrastructure that is already deployed and verified.

---

## Git

| | |
| --- | --- |
| Remote | `git@github.com:Serajian/srosha.git` |
| Base branch | `master` — there is no `main` branch |
| Branch naming | `<type>/<slug>`, see `docs/CONVENTIONS.md` |
| Module path | `github.com/Serajian/srosha` |
| Go version | `1.26` |

---

## Deployment

Host: one Linux server running Docker + Dokploy, shared with unrelated apps.

| | |
| --- | --- |
| Dokploy project | `srosha` |
| Service type | Compose with a Git source (never Application or Database) |
| Compose path | `deployment/app/docker-compose.yml` |
| Dockerfile | `deployment/app/Dockerfile` — one image, both binaries |
| Image | `srosha:latest`; the binary is selected by `command` |
| Domain | on the **gateway service only**; the dispatcher has none |
| Isolated Deployment | OFF |

### Networks

| Name | Purpose |
| --- | --- |
| `srosha-net` | private bridge, `external: true`, created by hand. Both binaries plus postgres and nats. |
| `dokploy-network` | Traefik's shared network. **Gateway only**, and it must be declared *alongside* `srosha-net`, never instead of it. |

### Watch paths for auto-deploy

```
cmd/**  internal/**  pkg/**  api/**  gen/**  go.mod  go.sum  deployment/app/**
```

---

## Services and ports

Nothing publishes a host port. Every port below is `expose:` on the private
network; `ports:` is never used.

| Service | Port | Purpose |
| --- | --- | --- |
| gateway | 50051 | gRPC |
| gateway | 8080 | REST via grpc-gateway, and `/healthz` |
| dispatcher | 8081 | `/healthz` only |
| nats | 4222 | clients |
| nats | 8222 | monitoring JSON — unauthenticated, never published |
| postgres | 5432 | clients |

Services address each other by **compose service name** — `postgres`, `nats` —
never by Dokploy's suffixed container names.

### Local development

Not the deployed stack. `deployment/app/docker-compose.dev.yml` runs the
dependencies only; the binaries run on the host with `make run-gateway`.

| | |
| --- | --- |
| Compose file | `deployment/app/docker-compose.dev.yml` |
| postgres | `127.0.0.1:7001` → 5432, image `postgres:18-alpine` |
| nats | `127.0.0.1:7002` → 4222, image `nats:2.14-alpine`, JetStream on (`-js`) |
| Targets | `make dev-up`, `dev-down`, `dev-del`, `dev-reset`, `dev-ps`, `dev-logs`, `dev-ready` |

The ports follow the Makefile's `BASE_PORT` (7000) scheme and are published on
**loopback only** — the one place `ports:` is used, and only because a binary
running on this machine has to reach them. The deployed compose still publishes
nothing.

The local nats runs with **no accounts and no permissions**, while the deployed
one runs one user per binary from `nats-server.conf`. A permission mistake will
therefore not show up locally.

### PostgreSQL

| | |
| --- | --- |
| Image | `postgres:18-alpine` |
| Host | `postgres:5432` |
| Database | `srosha` |
| Role | `srosha` |
| Volume | `pgdata` mounted at `/var/lib/postgresql` (not `/data`) |
| Memory limit | 1G, `shared_buffers=256MB` |
| Password | in `.env` and the Dokploy Environment tab |

### NATS

| | |
| --- | --- |
| Image | `nats:2.14-alpine` |
| Host | `nats:4222` |
| Config | `nats-server.conf`, supplied via Dokploy Advanced → Volumes → File Mount |
| JetStream store | volume `jetstream` at `/data/jetstream` |
| Limits | `max_memory_store=256MB`, `max_file_store=8GB` |
| Memory limit | 1G |
| Users | `gateway`, `dispatcher`, `admin` — one credential each, never shared |
| Passwords | in `.env` and the Dokploy Environment tab |

---

## Application configuration

Environment variables only; there is no config file. Loaded through viper with
the prefix `NOTIF_`, and `.` in a config key mapped to `_`.

Every key, with its defaults and which binary needs it, is documented in
`.env.example`. Summary of what each binary requires:

| Group | Keys | gateway | dispatcher |
| --- | --- | --- | --- |
| app | `NOTIF_APP_ENV`, `NOTIF_APP_SERVICE_NAME`, `NOTIF_APP_SHUTDOWN_TIMEOUT` | ✅ | ✅ |
| grpc | `NOTIF_GRPC_ADDR`, `NOTIF_GRPC_HTTP_ADDR` | ✅ | — |
| http | `NOTIF_HTTP_ADDR` | — | ✅ |
| http server | `NOTIF_HTTP_SERVER_READ_HEADER_TIMEOUT`, `NOTIF_HTTP_SERVER_READ_TIMEOUT`, `NOTIF_HTTP_SERVER_WRITE_TIMEOUT`, `NOTIF_HTTP_SERVER_IDLE_TIMEOUT` | ✅ | ✅ |
| http client | `NOTIF_HTTP_CLIENT_TIMEOUT`, `NOTIF_HTTP_CLIENT_DIAL_TIMEOUT`, `NOTIF_HTTP_CLIENT_TLS_TIMEOUT`, `NOTIF_HTTP_CLIENT_MAX_IDLE_CONNS`, `NOTIF_HTTP_CLIENT_MAX_IDLE_PER_HOST`, `NOTIF_HTTP_CLIENT_IDLE_CONN_TIMEOUT` | — | ✅ |
| db | `NOTIF_DB_DSN`, `NOTIF_DB_MAX_CONNS`, `NOTIF_DB_MAX_CONN_LIFETIME`, `NOTIF_DB_MAX_CONN_IDLE_TIME`, `NOTIF_DB_HEALTH_CHECK_PERIOD`, `NOTIF_DB_CONNECT_TIMEOUT`, `NOTIF_DB_CONNECT_ATTEMPTS`, `NOTIF_DB_CONNECT_BACKOFF` | ✅ | ✅ |
| mq | `NOTIF_MQ_URL`, `NOTIF_MQ_STREAM`, `NOTIF_MQ_DUPLICATE_WINDOW`, `NOTIF_MQ_CONNECT_TIMEOUT`, `NOTIF_MQ_MAX_RECONNECTS`, `NOTIF_MQ_RECONNECT_WAIT`, `NOTIF_MQ_DRAIN_TIMEOUT` | ✅ | ✅ |
| ratelimit | `NOTIF_RATELIMIT_PER_MINUTE` | ✅ | — |
| sender | `NOTIF_SENDER_SMTP_*`, `NOTIF_SENDER_TELEGRAM_TOKEN`, `NOTIF_SENDER_BALE_TOKEN`, `NOTIF_SENDER_WHATSAPP_TOKEN` | — | ✅ |
| webhook | `NOTIF_WEBHOOK_TIMEOUT`, `NOTIF_WEBHOOK_MAX_ATTEMPTS` | — | ✅ |
| telemetry | `NOTIF_TELEMETRY_LOG_LEVEL`, `NOTIF_TELEMETRY_LOG_FORMAT`, `NOTIF_TELEMETRY_LOG_SOURCE` | ✅ | ✅ |

`NOTIF_MQ_URL` carries a **different** NATS user per binary. Do not collapse them.

### Decided, not yet read by any code

These are settled in `docs/ARCHITECTURE.md` and shape the `credentials` table, so
they are recorded here before the code exists. They move into the table above and
into `.env.example` in the same commit that first reads them.

| Key | Purpose |
| --- | --- |
| `NOTIF_CRYPTO_KEYS` | JSON, key id → key. **Secret.** A set, not one value, so a key can be changed without an outage. Same shape as `NOTIF_WEBHOOK_SECRETS`. |
| `NOTIF_CRYPTO_KEY_ID` | Which key in that set new values are encrypted with. Old values name their own. |

Each key is 32 bytes for AES-256, base64 in the variable. Generate with
`openssl rand -base64 32`.

### Password rule

Letters, digits and hyphens only. `#` is truncated by Dokploy's `.env` parsing;
`)` `!` `$` break in the shell; `#` `@` `:` `/` `?` need percent-encoding inside a
connection URL. Generate with `openssl rand -hex 24`.

---

## Brand assets

| | |
| --- | --- |
| Directory | `docs/assets/brand/` |
| Logo (vector, master) | `docs/assets/brand/srosha-logo.svg` |
| Logo PNG | `docs/assets/brand/srosha-{16,32,64,256,512}.png` |
| Monochrome | `docs/assets/brand/srosha-mono-512.png` (سرمه‌ای), `srosha-mono-white-512.png` (سفید) |
| README hero | `docs/assets/brand/srosha-hero.png` — 1280×400 |
| Architecture diagram | `docs/assets/brand/architecture.svg` — used in `README.md` |
| Palette | `#3256AE` royal · `#33A5DE` sky · `#4934A2` violet · `#317DC6` azure · `#10182B` ink |
| Wordmark font | Onest (Google Fonts), weight 700 |

---

## Migrations

| | |
| --- | --- |
| Tool | goose, sequential numbering (`-s`) |
| Directory | `migrations/` |
| Env keys | `GOOSE_DRIVER`, `GOOSE_MIGRATION_DIR`, `GOOSE_DBSTRING` |
| When | a separate deployment step, never from an application entrypoint |

| Group | Keys | gateway | dispatcher |
| --- | --- | --- | --- |
| reconcile | `NOTIF_RECONCILE_AFTER`, `NOTIF_RECONCILE_GIVE_UP` | — | ✅ |
| webhook | `NOTIF_WEBHOOK_SECRETS` | — | ✅ |

`NOTIF_WEBHOOK_SECRETS` holds one signing secret per source, keyed by source id.
Each source gets its own: with a single shared secret, any source holding it
could forge a signed callback to another. It is never stored in the database and
never returned by the API — it is handed to the source out of band. Adding a
source therefore needs a redeploy.

`RECONCILE_AFTER` is how long a delivery may sit pending before the scheduler
picks it up; `RECONCILE_GIVE_UP` is the age past which its next attempt is the
last one. The scheduler runs in the dispatcher, because recovery sends rather
than republishes. See `docs/ARCHITECTURE.md`.

---

## Local development

Local host-port mappings only. Production publishes nothing.

| | |
| --- | --- |
| `BASE_PORT` | `7000` (Makefile) |
| postgres | `127.0.0.1:7001` |
| nats | `127.0.0.1:7002` |
| gateway gRPC | `50051`, read from `NOTIF_GRPC_ADDR` in `.env` |
| env file | `.env`, git-ignored; template in `.env.example` |

---

## Build targets

`make help` lists every target. The ones that matter elsewhere:

| Target | Does |
| --- | --- |
| `make build` | both binaries into `build/` |
| `make proto` | `buf generate` into `gen/` (committed) |
| `make arch-check` | fails if `core/domain` imports outside stdlib, `core/shared`, `pkg/errs` |
| `make precommit` | deps, proto lint, format, lint, arch-check |
| `make prepush` | the above plus race tests |
| `make migrate-*` | goose up / down / status / create / reset |
