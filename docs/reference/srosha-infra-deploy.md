# Srosha — Infrastructure & Deployment

Companion to `SPEC.md`. That document describes **what the application is**;
this one describes **where it runs and how it gets there**.

Everything under [§2](#2-what-is-already-live) is **already deployed and
verified**. Do not recreate it, and do not add `nats` or `postgres` services to
the application's compose file.

Host: a single Linux server running Docker + Dokploy, ~7.2 GiB RAM, ~98 GB
disk, shared with several unrelated applications.

---

## Table of contents

1. [Topology](#1-topology)
2. [What is already live](#2-what-is-already-live)
3. [Networking model and its traps](#3-networking-model-and-its-traps)
4. [Ports](#4-ports)
5. [Environment variables](#5-environment-variables)
6. [Deploying the application from git](#6-deploying-the-application-from-git)
7. [The Dockerfile](#7-the-dockerfile)
8. [Auto-deploy on merge](#8-auto-deploy-on-merge)
9. [Migrations under auto-deploy](#9-migrations-under-auto-deploy)
10. [Health checks](#10-health-checks)
11. [Resource limits](#11-resource-limits)
12. [Operations and debugging](#12-operations-and-debugging)
13. [Lessons already paid for](#13-lessons-already-paid-for)
14. [Rules for the implementing agent](#14-rules-for-the-implementing-agent)

---

## 1. Topology

```
Internet
   │ :443
   ▼
Cloudflare / Traefik
   │
   ▼
 gateway ──────────┐
                   │   srosha-net  (private bridge, no host ports)
 dispatcher ───────┼──► nats:4222        (JetStream, file store on a volume)
                   └──► postgres:5432    (data on a volume)
```

- `srosha-net` is an **external** docker network, created once by hand. It is
  deliberately *not* `dokploy-network`: that network is shared with every other
  app on this host, and putting the broker there would let any of them connect.
- Only `gateway` is reachable from outside, and only through Traefik.
- `dispatcher` has no domain and no published port.
- Nothing publishes a host port. Not 4222, not 5432, not 8222.

---

## 2. What is already live

All three pieces below are deployed as **Dokploy Compose services** inside one
Dokploy project named `srosha`, and have been verified working.

Dokploy appends a random suffix to service names, so container names look like
`srosha-postgres-jdl26k-postgres-1`. **Never use those names in
configuration** — the compose service name is the DNS alias, so the app always
connects to `postgres` and `nats`.

### 2.1 The network

```bash
docker network create srosha-net
```

### 2.2 PostgreSQL

- Image `postgres:18-alpine`, currently 18.6
- Database `srosha`, user `srosha`, reachable at `postgres:5432`
- Data on the named volume `pgdata`, mounted at **`/var/lib/postgresql`**

The mount path matters. Postgres 18 stores data in a version-specific
subdirectory (`/var/lib/postgresql/18/docker`). Mounting the old
`/var/lib/postgresql/data` makes the image refuse to start with a confusing
message about `pg_upgrade`.

Health check queries the database rather than using `pg_isready`, which only
proves the server accepts sockets — it reports `healthy` even when the expected
role does not exist:

```yaml
healthcheck:
  test: ["CMD-SHELL", "psql -U $$POSTGRES_USER -d $$POSTGRES_DB -c 'select 1' || exit 1"]
```

### 2.3 NATS

- Image `nats:2.14-alpine`, currently 2.14.5
- Reachable at `nats:4222`; monitoring on `8222`, network-internal only
- JetStream enabled, file store on the named volume `jetstream` at
  `/data/jetstream`
- Limits: `max_memory_store: 256MB`, `max_file_store: 8GB`
- Config supplied through Dokploy **Advanced → Volumes → File Mount**
  (`nats-server.conf`), referenced from compose as
  `../files/nats-server.conf:/etc/nats/nats-server.conf:ro`

Three users with least-privilege permissions, **verified by test**:

| User | May publish | May subscribe |
| --- | --- | --- |
| `gateway` | `notify.>`, `$JS.API.>` | `_INBOX.>` only |
| `dispatcher` | `$JS.API.>`, `$JS.ACK.>` | `_INBOX.>`, `notify.>` |
| `admin` | everything | everything |

A `gateway` credential cannot read anyone's notifications — subscribing to
`notify.>` as that user returns a permissions violation. Preserve this split.

Every JetStream client needs `$JS.API.>` and its own inbox in addition to the
business subjects. A permission set covering only the business subjects fails
with opaque timeouts rather than a clear error.

`nats server info` does **not** work for these users: it requires the `$SYS`
system account, which is not configured. This is expected, not a fault. Use
`nats stream ls`, `nats consumer report`, or the `/jsz` monitoring endpoint.

---

## 3. Networking model and its traps

Dokploy gives each separately-deployed service its own docker network, so
services cannot resolve each other by name across services. There is no UI
field to attach a service to an existing network. This is exactly why all three
pieces are **Compose** services rather than Dokploy's built-in Application or
Database types — a compose file is the only place we can declare
`external: true`.

### Trap 1 — a domain silently severs the other network

When a domain is attached to a compose service, Dokploy adds
`dokploy-network` to it — **replacing** the service's existing network rather
than adding to it. A gateway that lists only `srosha-net` loses its database
and queue connections the moment a domain is attached, with no deploy-time
error, only runtime failures.

**The gateway must therefore declare both networks explicitly.**

```yaml
gateway:
  networks:
    - srosha-net
    - dokploy-network
```

Verify immediately after attaching a domain:

```bash
docker inspect <gateway-container> \
  --format '{{json .NetworkSettings.Networks}}' | jq keys
# must list BOTH
```

### Trap 2 — Isolated Deployment

Found under **Advanced → Enable Isolated Deployment**. It creates a dedicated
network named after the compose service, which fights with `srosha-net`. It is
also deprecated in current Dokploy. **Keep it OFF.**

### Trap 3 — the Import box

**Advanced → Import** warns that importing a template removes all existing
environment variables, mounts and domains. Never touch it on a configured
service.

### Verification habit

Use the **Preview Compose** button before every deploy. It shows exactly how
Dokploy rewrote the file, which is the fastest way to catch an unexpected
network change.

---

## 4. Ports

| Service | Port | Exposure | Purpose |
| --- | --- | --- | --- |
| gateway | 50051 | `expose` | gRPC |
| gateway | 8080 | `expose` | REST via grpc-gateway + `/healthz` |
| dispatcher | 8081 | `expose` | `/healthz` only |
| nats | 4222 | `expose` | clients |
| nats | 8222 | `expose` | monitoring JSON, **no authentication** |
| postgres | 5432 | `expose` | clients |

**Use `expose:`, never `ports:`.** `ports` publishes on the host and puts the
service on the public internet. NATS is a raw TCP protocol, so Traefik offers
it no protection, and the NATS monitoring endpoint has no authentication of any
kind — it would expose stream names, message counts and connected clients to
anyone.

80 and 443 belong to Traefik and must never appear in a compose file here.

---

## 5. Environment variables

Configuration is **environment-only**, loaded through viper with the prefix
`NOTIF_` and `.` in config keys mapped to `_` (see `SPEC.md` §10). Secrets live
in Dokploy's Environment tab, never in the compose file and never in git.

### Gateway

```
NOTIF_APP_ENV=production
NOTIF_APP_SHUTDOWN_TIMEOUT=15s
NOTIF_GRPC_ADDR=:50051
NOTIF_GRPC_HTTP_ADDR=:8080
NOTIF_DB_DSN=postgres://srosha:<pw>@postgres:5432/srosha?sslmode=disable
NOTIF_DB_MAX_CONNS=10
NOTIF_DB_MAX_LIFETIME=30m
NOTIF_MQ_URL=nats://gateway:<pw>@nats:4222
NOTIF_RATELIMIT_PER_MINUTE=600
NOTIF_TELEMETRY_LOG_LEVEL=info
```

### Dispatcher

```
NOTIF_APP_ENV=production
NOTIF_APP_SHUTDOWN_TIMEOUT=30s
NOTIF_HTTP_ADDR=:8081
NOTIF_DB_DSN=postgres://srosha:<pw>@postgres:5432/srosha?sslmode=disable
NOTIF_DB_MAX_CONNS=10
NOTIF_MQ_URL=nats://dispatcher:<pw>@nats:4222
NOTIF_SENDER_SMTP_HOST=...
NOTIF_SENDER_SMTP_PORT=587
NOTIF_SENDER_SMTP_USER=...
NOTIF_SENDER_SMTP_PASSWORD=...
NOTIF_SENDER_TELEGRAM_TOKEN=...
NOTIF_SENDER_BALE_TOKEN=...
NOTIF_SENDER_WHATSAPP_TOKEN=...
NOTIF_WEBHOOK_TIMEOUT=10s
NOTIF_WEBHOOK_MAX_ATTEMPTS=5
NOTIF_TELEMETRY_LOG_LEVEL=info
```

### Infrastructure services (already set)

```
# postgres service
POSTGRES_USER=srosha
POSTGRES_DB=srosha
POSTGRES_PASSWORD=<pw>

# nats service
NATS_GATEWAY_PASSWORD=<pw>
NATS_DISPATCHER_PASSWORD=<pw>
NATS_ADMIN_PASSWORD=<pw>
```

`POSTGRES_USER` and `POSTGRES_DB` must be set **explicitly**. Relying on
compose's `${VAR:-default}` substitution does not survive Dokploy's `.env`
handling, and the cluster then initialises with the wrong role.

### Password rules — learned the hard way

**Use only letters, digits and hyphens.** A password containing `#` was
silently truncated at the `#` by Dokploy's `.env` parsing: `D#D)yo00` arrived in
the container as `D`. Special characters break in at least three places:

| Layer | Breaks on |
| --- | --- |
| Dokploy `.env` | `#` starts a comment |
| shell | `)` `!` `$` `#` |
| connection URL | `#` `@` `:` `/` `?` need percent-encoding |

Generate with `openssl rand -hex 24`, or use a long hyphenated word phrase.
Length provides the security; special characters only provide bugs.

### Secrets in code

Every secret-typed config field uses `settings.Secret`, which redacts itself in
`String()`. Never log the config struct any other way.

Dokploy does not pick up environment changes automatically — the service must be
redeployed after editing them.

---

## 6. Deploying the application from git

The application is a **Compose service with a Git source**. Compose vs Raw and
Git vs Raw are independent axes; a Compose service can absolutely be sourced
from a repository.

It cannot be a Dokploy **Application**: those cannot be attached to an existing
external network, which is the whole reason this stack uses compose.

### Repository layout

```
srosha/
└── deployment/
    └── app/
        ├── docker-compose.yml
        └── Dockerfile
```

### `deployment/app/docker-compose.yml`

```yaml
services:
  gateway:
    build:
      context: ../..                          # repository root
      dockerfile: deployment/app/Dockerfile
    image: srosha:latest
    command: ["/app/gateway"]
    restart: unless-stopped

    expose:
      - "50051"
      - "8080"

    environment:
      NOTIF_APP_ENV: ${NOTIF_APP_ENV}
      NOTIF_GRPC_ADDR: ${NOTIF_GRPC_ADDR}
      NOTIF_DB_DSN: ${NOTIF_DB_DSN}
      NOTIF_MQ_URL: ${NOTIF_GATEWAY_MQ_URL}
      NOTIF_RATELIMIT_PER_MINUTE: ${NOTIF_RATELIMIT_PER_MINUTE}
      NOTIF_TELEMETRY_LOG_LEVEL: ${NOTIF_TELEMETRY_LOG_LEVEL}

    healthcheck:
      test: ["CMD", "/app/gateway", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 20s

    deploy:
      resources:
        limits:
          memory: 512M

    networks:
      - srosha-net
      - dokploy-network

    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

  dispatcher:
    # Same build and same image tag: the second build is a cache hit, and the
    # two binaries can never drift onto different commits.
    build:
      context: ../..
      dockerfile: deployment/app/Dockerfile
    image: srosha:latest
    command: ["/app/dispatcher"]
    restart: unless-stopped

    expose:
      - "8081"

    environment:
      NOTIF_APP_ENV: ${NOTIF_APP_ENV}
      NOTIF_DB_DSN: ${NOTIF_DB_DSN}
      NOTIF_MQ_URL: ${NOTIF_DISPATCHER_MQ_URL}
      NOTIF_SENDER_TELEGRAM_TOKEN: ${NOTIF_SENDER_TELEGRAM_TOKEN}
      NOTIF_SENDER_BALE_TOKEN: ${NOTIF_SENDER_BALE_TOKEN}
      NOTIF_SENDER_SMTP_HOST: ${NOTIF_SENDER_SMTP_HOST}
      NOTIF_SENDER_SMTP_PORT: ${NOTIF_SENDER_SMTP_PORT}
      NOTIF_SENDER_SMTP_USER: ${NOTIF_SENDER_SMTP_USER}
      NOTIF_SENDER_SMTP_PASSWORD: ${NOTIF_SENDER_SMTP_PASSWORD}
      NOTIF_TELEMETRY_LOG_LEVEL: ${NOTIF_TELEMETRY_LOG_LEVEL}

    healthcheck:
      test: ["CMD", "/app/dispatcher", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 20s

    deploy:
      resources:
        limits:
          memory: 512M

    # No dokploy-network: this service has no domain and must not be routable.
    networks:
      - srosha-net

    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

networks:
  srosha-net:
    external: true
  dokploy-network:
    external: true
```

Note the two different `NOTIF_MQ_URL` values, fed from
`NOTIF_GATEWAY_MQ_URL` and `NOTIF_DISPATCHER_MQ_URL`. Each binary uses its own
NATS user; do not collapse them into one credential.

### Dokploy settings

| Setting | Value |
| --- | --- |
| Service type | Compose |
| Source | Git (or the GitHub integration) |
| Branch | `master` |
| Compose Path | `deployment/app/docker-compose.yml` |
| Compose Type | Docker Compose (**not** Stack — Stack does not support `build`) |
| Isolated Deployment | OFF |
| Domain | on the **gateway service only** |

---

## 7. The Dockerfile

One image containing both binaries, selected at runtime by `command`. One
build, one push, one version — the two can never drift apart.

Requirements:

- Multi-stage: `golang:1.26` builder, minimal runtime.
- `CGO_ENABLED=0`, `-ldflags="-s -w"`, `-trimpath`.
- Build both: `go build -o /out/gateway ./cmd/gateway` and the same for
  `dispatcher`.
- Cache module downloads in their own layer (`go.mod`/`go.sum` copied and
  `go mod download` run before the source is copied).
- **CA certificates in the runtime image.** The senders make outbound TLS calls
  to Telegram, Bale and the WhatsApp API; without them every send fails with an
  opaque x509 error.
- Timezone data if any scheduling logic needs it.
- Run as a **non-root** user.
- `.dockerignore` excluding `.git`, `.env`, `bin/`, `*_test.go` fixtures and
  local tooling.

Also add a `healthcheck` subcommand to each binary so the compose health check
needs no `curl` or `wget` in the runtime image — a distroless image has
neither.

---

## 8. Auto-deploy on merge

Two options:

1. **GitHub integration** — connect GitHub in Dokploy settings, then enable
   Auto Deploy on the service. Dokploy creates the webhook itself.
2. **Manual webhook** — copy the URL from the service's Deployments tab into
   the repository's `Settings → Webhooks`, event `push`.

Either way, set **Watch Paths** so unrelated commits do not redeploy:

```
cmd/**
internal/**
pkg/**
api/**
gen/**
go.mod
go.sum
deployment/app/**
```

Without this, editing the README restarts the whole service.

The default branch is `master`. **Never create a `main` branch, and never
configure a git author or committer identity on this repository.**

---

## 9. Migrations under auto-deploy

Auto-deploy makes schema changes dangerous:

```
merge → automatic deploy → new code starts
      → migration has not run
      → code references a column that does not exist → crash loop
```

Migrations are run by goose as a **separate step**, never from the application
entrypoint (see `SPEC.md` §13). Several replicas racing for the lock at boot is
not worth the convenience, and a failed migration should stop the release
rather than crash-loop the app.

**Every migration must be backward compatible with the previous release.**
Expand, then contract:

| Release | Migration | Code |
| --- | --- | --- |
| N | add nullable column, backfill | ignores it |
| N+1 | — | writes and reads it |
| N+2 | make it NOT NULL, drop the old column | uses only the new one |

Never drop or rename a column in the same release whose code stops using it.
With auto-deploy there is no window in which old code is guaranteed to be gone.

Run migrations **before** merging the code that depends on them.

---

## 10. Health checks

- **Gateway**: the standard gRPC health protocol (`grpc.health.v1.Health`),
  plus HTTP `/healthz` on 8080 — Traefik's checks speak HTTP, not gRPC.
- **Dispatcher**: a minimal HTTP listener on 8081 serving `/healthz`.
- Readiness must reflect **dependencies**. A gateway that cannot reach Postgres
  or NATS is not ready even though its process is alive.

Liveness and readiness are different questions. The Postgres incident on this
host is the cautionary example: `pg_isready` reported `healthy` while every
connection failed with `role "srosha" does not exist`. A health check that does
not exercise the real dependency is worse than none, because it produces
confident wrong answers.

---

## 11. Resource limits

Total host: ~7.2 GiB RAM, ~98 GB disk, shared with unrelated apps.

| Service | Memory limit | Disk |
| --- | --- | --- |
| postgres | 1G | volume, `shared_buffers=256MB` |
| nats | 1G | volume, `max_file_store=8GB` |
| gateway | 512M | — |
| dispatcher | 512M | — |

Roughly 3 GB committed, leaving headroom.

Set `logging` with `max-size` and `max-file` on **every** service. Unbounded
JSON log files are the most common way a small host runs out of disk, and when
the disk fills, Postgres dies along with everything else.

`max_file_store: 8GB` is deliberately conservative. If a runaway loop fills it,
NATS should start refusing writes rather than filling the disk and taking the
database down with it.

---

## 12. Operations and debugging

### Which containers are on the private network

```bash
docker network inspect srosha-net --format '{{range .Containers}}{{.Name}} {{end}}'
```

An empty result usually means the container is **stopped**, not that the
network is misconfigured — `network inspect` lists only running containers. A
stopped container still shows the network in `docker inspect`, but with an
empty `EndpointID` and `IPAddress`.

### NATS

There is no official NATS dashboard. The monitoring port serves JSON only
(`/varz`, `/connz`, `/jsz`, `/healthz`) and has **no authentication**, so it
must never be published or given a domain.

Use a throwaway container on the same network:

```bash
docker run --rm -it --network srosha-net natsio/nats-box:latest

nats -s nats://nats:4222 --user admin --password '<pw>' stream ls
nats -s nats://nats:4222 --user admin --password '<pw>' stream info NOTIFY
nats -s nats://nats:4222 --user admin --password '<pw>' consumer report NOTIFY
```

Pass credentials with `--user`/`--password`, not in the URL: a password
containing `#` or `)` breaks URL parsing and shell quoting.

### Postgres

```bash
docker exec <postgres-container> psql -U srosha -d srosha -c 'select 1;'
```

### Logs

```bash
docker logs --tail 100 -f <container>
```

---

## 13. Lessons already paid for

Each of these cost real debugging time on this host. Do not rediscover them.

1. **Postgres 18 changed its data path.** Mount the volume at
   `/var/lib/postgresql`, not `/var/lib/postgresql/data`.
2. **`POSTGRES_*` variables only apply on first init of an empty volume.**
   Changing them later has no effect until the volume is deleted. The log line
   `Database directory appears to contain a database; Skipping initialization`
   is the tell.
3. **A volume cannot be removed while any container references it**, even a
   stopped one. `docker rm -f <container>` first.
4. **Set `POSTGRES_USER` and `POSTGRES_DB` explicitly.** Compose's `${VAR:-default}`
   substitution does not survive Dokploy's `.env` handling.
5. **`pg_isready` is not a health check.** It reported healthy against a
   cluster with no `srosha` role.
6. **`#` in a password gets truncated** by Dokploy's `.env` parsing. Verify any
   credential actually arrived: `docker exec <c> env | grep <PREFIX>`.
7. **Dokploy's file-mount UI is under Advanced → Volumes**, not "Mounts". The
   deploy log line `Detected: N mounts` confirms it registered; `0 mounts` for
   NATS means the config never arrived and the server will not start.
8. **JetStream appends its own subdirectory** to `store_dir`, so
   `/data/jetstream` becomes `/data/jetstream/jetstream`. Harmless, but
   surprising when reading paths.
9. **`nats server info` needs the `$SYS` account** and fails with
   "ensure the account used has system privileges". That is not an auth failure;
   `Authorization Violation` is.
10. **A "Started" container is not a working one.** Every deploy here reported
    success while the service was crash-looping. Always check `docker ps`
    status and the logs.

---

## 14. Rules for the implementing agent

**Do not**

- Add `nats` or `postgres` services to the application's compose file. They are
  already deployed separately.
- Use `ports:` for anything. Use `expose:`.
- Give the dispatcher a domain.
- Put the gateway on only one network.
- Enable Isolated Deployment, or touch the Import box on a configured service.
- Use Dokploy's Application or Database service types for anything in this
  stack — they cannot join `srosha-net`.
- Reference Dokploy's suffixed container names anywhere in configuration. Use
  the compose service names `postgres` and `nats`.
- Share one NATS credential between the gateway and the dispatcher.
- Run migrations from an application entrypoint.
- Use `latest` for any base image tag; pin a minor version.
- Bake secrets into an image, a compose file, or git.
- Create a `main` branch or configure a git author identity.

**Do**

- Declare both `srosha-net` and `dokploy-network` on the gateway.
- Verify with **Preview Compose** before deploying, and with
  `docker inspect ... .NetworkSettings.Networks` after attaching a domain.
- Make health checks exercise the real dependency.
- Cap memory and log size on every service.
- Keep migrations backward compatible with the previous release.
- Read `SPEC.md` for anything about the application itself.
