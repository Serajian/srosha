# Srosha — Infrastructure & Deployment

Companion to `SPEC.md`. That document describes **what the application is**;
this one describes **where it runs and how it gets there**.

Everything under [§2](#2-what-is-already-live) is **already deployed and
verified**. Do not recreate it, and do not add `nats` or `postgres` services to
the application's compose file.

Host: a single Linux server running Docker + Dokploy, 15 GiB RAM and a 96 GB
disk with about 12 GB free, shared with several unrelated applications. It also
runs **nginx**, which owns 80 and 443 — see [§1](#1-topology) before assuming
Traefik does.

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
Cloudflare            proxied A records; gRPC needs Network -> gRPC on
   │
   ▼
nginx :80 :443        TLS ends HERE. nginx owns both ports, not Traefik.
   │
   ├─ panel / admin ─► 127.0.0.1:8000 ─► Traefik `web` ─► console 8090 / 8092
   │
   └─ api, grpc_pass ► 127.0.0.1:50051 ─────────────────► gateway 50051
                                                          Traefik not involved

 gateway ──────────┐
 dispatcher ───────┤   srosha-net  (private; nothing on it is routable)
 console ──────────┼──► postgres:5432    (data on a volume)
                   ├──► nats:4222        (JetStream, file store on a volume)
                   └──► adminer:8080     (reachable on the tailnet only)
```

- `srosha-net` is an **external** docker network, created once by hand. It is
  deliberately *not* `dokploy-network`: that network is shared with every other
  app on this host, and putting the broker there would let any of them connect.
- **nginx holds 80 and 443.** Traefik is behind it, plain HTTP, published on
  `127.0.0.1:8000` alone. So Traefik never sees TLS, holds no certificate, and
  its `websecure` entrypoint is a door nobody knocks on — a router placed there
  loads, reports `enabled`, resolves a healthy backend, and receives nothing at
  all. Every router on this host is on `web`. See §13.
- **`api.srosha.ir` does not go through Traefik.** `grpc_pass` speaks HTTP/2
  straight to the backend and Traefik's `web` entrypoint is HTTP/1.1; enabling
  h2c there would mean editing the `traefik.yml` this host shares with every
  other application on it.
- `dispatcher` has no domain and no published port, and is not routable.
- **Two host ports on the whole machine**, both bound to an address the internet
  cannot reach: the gateway's 50051 on `127.0.0.1` so only nginx can call it,
  and Adminer's 8083 on loopback and the Tailscale address. `0.0.0.0` appears
  nowhere, and `docker port <container>` is how that is checked. Not 4222, not
  5432, not 8222.

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

### 2.2 The files are in the repository now

Every one of these used to be described here, in prose, and the prose went
stale — this document was still describing the *previous* host on the day the
service moved. The configuration itself now lives in
[`deployment/infra/`](../../deployment/infra/), with its own README saying what
applies each file and where it lands:

| | |
| --- | --- |
| postgres and adminer | `deployment/infra/postgres/docker-compose.yml` |
| nats | `deployment/infra/nats/docker-compose.yml` |
| nats users and limits | `deployment/infra/nats/nats-server.conf` |
| the three nginx server blocks | `deployment/infra/nginx/srosha.conf` |

**Read those, not a summary of them.** What follows is only what a file cannot
say about itself.

### 2.3 What the files do not say

**Postgres 18 keeps data in a version subdirectory.** The volume mounts at
`/var/lib/postgresql`, one level above `PGDATA`. Mounting the older
`/var/lib/postgresql/data` makes the image refuse to start with a confusing
message about `pg_upgrade`.

**The cluster locale is decided once, by `initdb`, and never again.** On a fresh
volume it is the single moment. Changing it later means dump, recreate, restore.

**`nats server info` does not work for these users.** It wants the `$SYS` system
account, which is not configured. That is expected, not a fault. Use `nats
stream ls`, `nats consumer report`, or the `/jsz` monitoring endpoint.

**Every JetStream client needs `$JS.API.>` and its own inbox** in addition to the
business subjects. A permission set covering only the business subjects fails
with opaque timeouts rather than a clear error — which is also why narrowing
these has to be verified against the real binaries and not reasoned about.

### 2.4 The NATS permission split, measured

| User | May publish | May subscribe |
| --- | --- | --- |
| `gateway` | `notify.>`, `$JS.API.INFO`, `$JS.API.STREAM.CREATE.NOTIFY`, `$JS.API.STREAM.UPDATE.NOTIFY` | `_INBOX.>` |
| `dispatcher` | the three above minus `notify.>`, plus `$JS.API.CONSUMER.CREATE.NOTIFY.dispatcher.>`, `$JS.API.CONSUMER.MSG.NEXT.NOTIFY.dispatcher`, `$JS.ACK.NOTIFY.dispatcher.>` | `_INBOX.>`, `$JSC.CI.>` |
| `admin` | everything | everything |

Both used to hold `$JS.API.>`, which is the whole JetStream admin surface. This
document claimed the split meant a `gateway` credential could not read anyone's
notifications; it was tested on 2026-09-01 and that was false. A direct
subscribe to `notify.>` is refused, which was the true half, but delivery
arrives on `_INBOX.>`, which the gateway may subscribe to, and every consumer
and stream verb was reachable:

```
add a second consumer beside the dispatcher's   refused -- WorkQueue holds the subject
delete the dispatcher's consumer                ALLOWED
then add your own and read every message body   ALLOWED
purge the stream                                ALLOWED
delete the stream                               ALLOWED
```

All five are refused now, and both binaries still do their work.

**The list was measured, not derived.** Reading the Go source gives four of the
gateway's subjects and misses `$JSC.CI.>` entirely — the consume machinery
subscribes to it and the name appears nowhere in this repository. A missing
subject fails as a timeout rather than an error, so a list that looks complete
is not evidence that it is.

To re-measure after upgrading the client library: set `trace: true` in
`nats-server.conf`, grant one user `>`, run the binary, and read the `[PUB` and
`[SUB` lines out of `docker logs`.

**The stream name is baked into those subjects.** It is `NOTIFY`, the default of
`NOTIF_MQ_STREAM`. Changing that variable without changing these lines takes
JetStream away from both binaries, quietly.


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
| gateway | 50051 | `127.0.0.1` | gRPC, for nginx's `grpc_pass` |
| gateway | 8080 | `expose` | REST via grpc-gateway + `/healthz` |
| dispatcher | 8081 | `expose` | `/healthz` only |
| console | 8090 / 8091 / 8092 | `expose` | portal, `/healthz`, admin |
| nats | 4222 | `expose` | clients |
| nats | 8222 | `expose` | monitoring JSON, **no authentication** |
| postgres | 5432 | `expose` | clients |
| adminer | 8083 | `127.0.0.1` + Tailscale | the database, from the tailnet |

**`ports:` needs an address in front of it, always.** A bare `ports: ["8083:8080"]`
binds `0.0.0.0` and publishes the service to the internet; `127.0.0.1:8083:8080`
publishes it to the machine. The two entries above are the only ones on this
host, and both carry an address. Everything else is `expose:`.

The check is `docker port <container>`: if a line's left-hand side has no
address on it, that service is on the public internet. That is how an unrelated
Adminer on this host was found published on `0.0.0.0:32768`.

Two of these would be actively dangerous published. NATS is a raw TCP protocol,
so no HTTP proxy in front offers it any protection; and the NATS monitoring
endpoint has no authentication of any kind — it would hand stream names,
message counts and connected clients to anyone.

**80 and 443 belong to nginx** — not to Traefik, and not to any compose file
here. Traefik itself is published only on `127.0.0.1:8000`.

---

## 5. Environment variables

Configuration is **environment-only**, loaded through viper with the prefix
`NOTIF_` and `.` in config keys mapped to `_` (see `SPEC.md` §10). Secrets live
in Dokploy's Environment tab, never in the compose file and never in git.

### Read the key list from one place

This section used to list the keys for each binary. It was a third copy — after
`.env.example` and the table in `docs/CONFIG.md` — and it rotted the way a third
copy does: it named `NOTIF_WEBHOOK_MAX_ATTEMPTS`, which has never existed (the
key is `NOTIF_WEBHOOK_MAX_FAILURES`), and it had no `console` section at all,
though that binary has been deployed since August.

- **[`.env.example`](../../.env.example)** — every key the code reads, grouped by
  which binary reads it, with the real defaults.
- **[`docs/CONFIG.md`](../CONFIG.md)** — which binary needs which group, and
  which keys are required.

Configuration is environment-only, prefixed `NOTIF_`, with `.` in a config key
mapped to `_`. Secrets live in Dokploy's Environment tab, never in a compose
file and never in git.

### What a key list cannot tell you

**An unset variable is not an empty one.** Compose substitutes `${VAR}` with an
empty string, but the reader treats empty as absent (`pkg/env/env.go`), so the
code's own default applies. Leaving an optional key out of Dokploy is safe; it
will not silently zero anything.

**Two keys must be said out loud even though they have defaults.**
`NOTIF_ADMIN_ADDR` defaults to `127.0.0.1:8092`, which inside a container is the
container's own loopback — Traefik cannot reach it and the admin panel answers
nothing. And `NOTIF_HTTP_ADDR` defaults to the dispatcher's `:8081`, so the
console needs `NOTIF_CONSOLE_HTTP_ADDR=:8091` or its health check watches a port
nobody probes.

**The infrastructure services take three of their own**, set on their own Dokploy
services and not the application's: `POSTGRES_PASSWORD`, the three
`NATS_*_PASSWORD`, and `ADMINER_BIND_IP`.

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

**`$` is the same trap, and it arrives with bcrypt.** The three
`NATS_*_PASSWORD` variables hold bcrypt hashes, which are full of `$`. Compose
expands `${...}` in the environment file, so a hash pasted as generated arrives
truncated at the third `$` — measured, the container received `$2a$11` and
nothing more. Double every `$` when pasting into Dokploy. `env_file:` is not an
escape hatch; it is interpolated too.

**Nothing here enforces any of this, and it showed.** On 2026-09-01 the `gateway`
and `dispatcher` NATS passwords turned out to be **eight characters** — against
the `openssl rand -hex 24` written three paragraphs above, in production, for
however long. No test failed, no check complained, and no log line said
anything: a short password is exactly as quiet as a long one.

What finally caught them was an accident. `nats server passwd` refuses a
password under ten characters, so two of the three hashes came back empty while
bcrypt was being rolled out. Both were rotated to `openssl rand -hex 24` in the
same window, which also meant updating `NOTIF_GATEWAY_MQ_URL` and
`NOTIF_DISPATCHER_MQ_URL`, since the plaintext lives in the client's URL.

Treat a tool refusing a credential as information, not as an obstacle to work
around. And when you touch a password here, check its length while you are
looking at it — that is the only inspection this rule gets.

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
├── docker-compose.yml          the deployed stack -- at the ROOT, see below
├── deployment/
│   ├── app/Dockerfile          one image, four binaries
│   └── infra/                  postgres, nats, nginx -- applied by hand
└── .dockerignore               at the root, because that is the build context
```

### The compose file is at the repository root, and has to be

Dokploy runs `docker compose --project-directory <repo root>`, and compose
resolves two things against that directory: relative build contexts, and the
`.env` it substitutes `${...}` from. Dokploy writes that `.env` beside the
compose file. With the file one level down those are two different directories
and neither works — the build context resolves outside the checkout, and every
`${...}` silently becomes an empty string.

### Read the file, not a copy of it

This section used to hold the whole of `docker-compose.yml` inline. It drifted,
as an inline copy always does, and by 2026-09-01 it described routing that no
longer existed. Open
[`docker-compose.yml`](../../docker-compose.yml) instead — every decision in it
carries the comment explaining itself.

Two things worth knowing before you read it:

- **Each binary gets its own NATS user.** `NOTIF_MQ_URL` is fed from
  `NOTIF_GATEWAY_MQ_URL` for one service and `NOTIF_DISPATCHER_MQ_URL` for the
  other. Do not collapse them into one credential.
- **`migrate` runs to completion and everything waits on it.** A failed
  migration stops the release rather than letting new code meet a schema it does
  not expect: the old containers are not replaced, so the previous version keeps
  serving.


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
  plus HTTP `/healthz` on 8080. Nothing in front of it speaks gRPC health:
  nginx `grpc_pass` does not health-check, and Traefik is not in that path.
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

Total host, measured 2026-09-01: **15 GiB RAM** (~7 GiB available) and a **96 GB
disk with 12 GB free — 89% full**, shared with unrelated apps.

| Service | Memory limit | Disk |
| --- | --- | --- |
| postgres | 1G | volume, `shared_buffers=256MB` |
| nats | 1G | volume, `max_file_store=2GB` |
| gateway | 512M | — |
| dispatcher | 512M | — |
| console | 512M | — |
| adminer | 128M | — |

Memory is comfortable: roughly 3.6 GB committed against 15 GiB.

**Disk is the constraint, and most of it is not ours.** 30 GB of it is docker
images for the ~30 applications running here, all of them in use. A
`docker builder prune` recovered 3 GB on 2026-09-01 and there is little else
safe to take.

`max_file_store` was `8GB` until that day — a figure chosen on a host with room
for it, left alone on a host without. A ceiling above the cliff brakes nothing.
It is `2GB` now, which is the rule to keep: **the JetStream ceiling has to sit
well below the free space, and `df -h /` is how you check that before raising
it.**

Set `logging` with `max-size` and `max-file` on **every** service. Unbounded
JSON log files are the most common way a small host runs out of disk, and when
the disk fills, Postgres dies along with everything else.

`max_file_store` is an emergency brake and not a size. The stream is WorkQueue,
so a message is deleted the moment it is acknowledged and the store is nearly
empty in normal running. The number only matters on the day nothing is
draining — and on that day it has to be reached before the disk is.

---

## 12. Operations and debugging

### Where the traffic is actually going

Four commands, in this order, and they cut the problem into halves rather than
guessing. This is the sequence that solved the 404 of 2026-09-01.

```bash
# 1. Who holds the public ports, and is Traefik even one of them?
ss -tlnp | grep -E ':(80|443) '
docker port dokploy-traefik

# 2. Does the origin answer at all, with Cloudflare taken out of the picture?
curl -sk -o /dev/null -w '%{http_code}\n' \
  --resolve panel.srosha.ir:443:127.0.0.1 https://panel.srosha.ir/

# 3. What does Traefik think it has? Its API is on the overlay, which the host
#    does not route into, so ask from a container that is already on it.
docker exec <a-srosha-container> \
  wget -qO- --timeout=5 http://dokploy-traefik:8080/api/http/routers | tr '}' '}\n' | grep -i srosha
docker exec <a-srosha-container> \
  wget -qO- --timeout=5 http://dokploy-traefik:8080/api/http/services | tr '}' '}\n' | grep -i srosha

# 4. And the application itself, with everything else taken out.
docker exec <console-container> wget -S -qO- http://127.0.0.1:8090/ 2>&1 | head -3
```

A router that is `enabled` with a service that is `UP` proves only that Traefik
is willing. It does not prove anything reaches it — see lesson 11.

For gRPC, the same split:

```bash
grpcurl -authority api.srosha.ir -import-path api/proto \
  -proto notification/v1/notification.proto -d '{"id":"..."}' \
  <server-ip>:443 notification.v1.NotificationService/Get   # the origin
grpcurl ... api.srosha.ir:443 ...                           # through Cloudflare
```

`Unauthenticated` is a **success** here: the call reached the gateway and its
interceptor refused it. `403` with an HTML body is Cloudflare's gRPC toggle.

### Adminer

`http://db.srosha.ir:8083/?pgsql=postgres` from any device on the tailnet. The
query string matters — the form opens on MySQL otherwise. Username `srosha`,
password `POSTGRES_PASSWORD`, database `srosha`.

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
11. **A healthy Traefik router can still receive nothing.** On 2026-09-01 the
    panel answered 404 while the router said `enabled`, the service said `UP`,
    the backend resolved to the right address, and the app itself returned a
    clean 303 when asked directly. Everything was true and the traffic never
    arrived, because Traefik does not hold 443 here — nginx does, and Traefik is
    published on `127.0.0.1:8000`. A router on `websecure` is a door nobody
    knocks on. The tell was in plain sight: of the dozens of routers on this
    host, ours were the only ones not on `web`.
    `docker port dokploy-traefik` and `ss -tlnp` settle it in two commands.
12. **`http2` is a socket option in nginx 1.24, not a server one.** Only the
    first server block loaded for `0.0.0.0:443` decides it, and the rest get
    `[warn] protocol options redefined` and are ignored. That warning is not
    cosmetic when the site is gRPC. `openssl s_client -alpn h2` is the answer:
    `ALPN protocol: h2` or it is off.
13. **Cloudflare answers gRPC with `403` and an HTML body when the toggle is
    off.** Network → gRPC, per zone. The origin never sees the call, so nothing
    in your own logs explains it. Test the origin directly first —
    `grpcurl -authority api.srosha.ir <server-ip>:443` — and an `Unauthenticated`
    from both sides means the whole path is good.

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
