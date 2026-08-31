# Deployment stack — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One image carrying all three binaries plus goose, and a compose file
Dokploy can deploy, publishing nothing and reaching the internet only through
three Traefik routers.

**Architecture:** Dokploy builds the image on the server from a Git source, so
nothing is pushed to a registry. A multi-stage `Dockerfile` compiles
`cmd/gateway`, `cmd/dispatcher` and `cmd/console` and installs a pinned goose;
`command` selects which one a service runs. Migrations are a compose service
under a profile, so they never run during a deploy and always carry the sql of
the commit that is deploying.

**Tech Stack:** Docker, Docker Compose v2, Dokploy, Traefik, Go 1.26,
goose v3.27.3, `postgres:18-alpine`, `nats:2.14-alpine`.

**Spec:** `docs/superpowers/specs/2026-08-31-production-deployment-design.md`
(part B).

**Prerequisite:** the plan `2026-08-31-admin-on-its-own-host.md` must be merged
first. Without it the console refuses to start with `NOTIF_ADMIN_ADDR=:8092` in
production, and with the loopback default the admin router below reaches
nothing.

## Global Constraints

- **Never `git commit` without a direct, explicit order from the owner.** Each
  task ends with the work in the tree, checks green, report written — then stop.
- Every commit carries a change report under `docs/changes/`, in Persian,
  following `docs/changes/TEMPLATE.md`.
- Branch: `feat/deployment-stack`, cut from `master`.
- **`ports:` is never used in `docker-compose.yml`.** Every port is `expose:`.
  The dev compose is the only file that publishes anything, and it is not
  touched by this plan.
- Repository data — hostnames, image names, versions, paths — belongs in
  `docs/CONFIG.md`, and **no secret ever goes in it.**
- Hostnames: `api.srosha.ir`, `panel.srosha.ir`, `admin.srosha.ir`.
- Networks: `srosha-net` (external, private) and `dokploy-network` (Traefik),
  the second joined **alongside** the first, never instead of it.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `deployment/app/Dockerfile` | build all three binaries and goose; one small runtime image with CA certificates, running as a non-root user |
| `deployment/app/.dockerignore` | keep the build context to what the build needs |
| `deployment/app/docker-compose.yml` | the deployed stack: six services, two networks, no published ports |
| `docs/CONFIG.md` | the Deployment section corrected to three binaries and three hostnames; the goose pin recorded |

`deployment/app/docker-compose.dev.yml` is **not** modified. It is local
development and its own header already says the deployed file is written
elsewhere.

---

### Task 1: The image

**Files:**
- Create: `deployment/app/Dockerfile`
- Create: `deployment/app/.dockerignore`

**Interfaces:**
- Consumes: nothing.
- Produces: an image tagged `srosha:latest` containing `/app/gateway`,
  `/app/dispatcher`, `/app/console`, `/app/goose` and `/app/migrations/`.
  Task 2 refers to all five paths.

- [ ] **Step 1: Write the `.dockerignore`**

`deployment/app/.dockerignore`. The build context is the repository root (task 2
sets `context: ../..`), so this keeps the laptop's own output and secrets out of
it:

```
.git
.idea
build/
.env
.env.*
!.env.example
!.env.*.example
docs/
sdk/
*.md
```

`sdk/` is excluded on purpose: it is a separate Go module with its own tag and
nothing in the three binaries imports it.

- [ ] **Step 2: Write the Dockerfile**

`deployment/app/Dockerfile`:

```dockerfile
# One image, three binaries, and the tool that migrates the database.
#
# Which binary runs is `command` in the compose file. Building three images
# that differ only in their entrypoint would mean three builds of the same
# code, and three chances for them to be built from different commits.

FROM golang:1.26-alpine AS build

# git is for the module cache over VCS paths; ca-certificates is needed to
# fetch modules at all.
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Dependencies first, so a code change does not re-download the module graph.
COPY go.mod go.sum ./
RUN go mod download

# Pinned, deliberately. The Makefile's setup-dev installs goose @latest, which
# is right for a laptop and wrong for an image that has to be rebuildable: the
# tool that migrates a production database must not change because a day
# passed.
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.3

COPY . .

# CGO off so the binaries are static and the runtime stage needs no libc
# beyond alpine's own.
ENV CGO_ENABLED=0
RUN go build -o /out/gateway ./cmd/gateway \
 && go build -o /out/dispatcher ./cmd/dispatcher \
 && go build -o /out/console ./cmd/console


FROM alpine:3.22

# ca-certificates because every channel is TLS -- SMTP, Telegram, Bale,
# WhatsApp, Matrix, Gotify, FCM, APNs -- and a container without them fails
# every send with a certificate error that reads like a bug in our code.
#
# tzdata because a schedule is read in UTC and a container with no zone
# database cannot say so.
RUN apk add --no-cache ca-certificates tzdata

# Not root. Nothing here writes to disk: the templates and assets are compiled
# into the console binary by go:embed, and the only state is postgres and nats.
RUN adduser -D -u 10001 srosha

WORKDIR /app

COPY --from=build /out/gateway /out/dispatcher /out/console ./
COPY --from=build /go/bin/goose ./goose

# The sql travels with the code, so a migration can never be from a different
# commit than the binaries that expect it.
COPY migrations/ ./migrations/

USER srosha

# No ENTRYPOINT and no CMD on purpose: every service in the compose file names
# its own, and a default here would be a fourth answer to a question already
# answered three times.
```

- [ ] **Step 3: Build it**

```bash
docker build -f deployment/app/Dockerfile -t srosha:latest .
```

Expected: a successful build. Run it from the repository root — the Dockerfile
expects the root as its context.

- [ ] **Step 4: Prove all five things are in it**

```bash
docker run --rm srosha:latest /app/goose -version
docker run --rm --entrypoint /bin/sh srosha:latest -c 'ls /app /app/migrations | head -30'
docker run --rm --entrypoint /bin/sh srosha:latest -c 'id'
```

Expected: `goose version: v3.27.3`; the three binaries and `goose` in `/app`,
and the numbered `.sql` files under `/app/migrations`; and a uid of `10001`,
not `0`.

- [ ] **Step 5: Prove a binary starts and refuses for the right reason**

```bash
docker run --rm srosha:latest /app/console
```

Expected: a **configuration** error naming a missing `NOTIF_` key — not a
crash, not "no such file". A binary that cannot find its config is a binary
that was built and can run.

- [ ] **Step 6: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-deployment-image.md` in Persian, then **stop**.

---

### Task 2: The deployed compose

**Files:**
- Create: `deployment/app/docker-compose.yml`

**Interfaces:**
- Consumes: `/app/gateway`, `/app/dispatcher`, `/app/console`, `/app/goose`,
  `/app/migrations/` from task 1.
- Produces: services `postgres`, `nats`, `gateway`, `dispatcher`, `console`,
  `migrate`; networks `srosha-net`, `dokploy-network`; volumes `pgdata`,
  `jetstream`.

- [ ] **Step 1: Write the file**

`deployment/app/docker-compose.yml`. Copy the healthcheck comments from
`docker-compose.dev.yml` where the check is the same — they explain why
`pg_isready` is not enough and why nats is asked for `/healthz` rather than a
ping, and those reasons did not change.

```yaml
# The deployed stack. Local development is docker-compose.dev.yml, which is a
# different file for a different job.
#
# Nothing here publishes a host port. The only way in is Traefik, and only for
# the three routers below.

x-app: &app
  image: srosha:latest
  build:
    context: ../..
    dockerfile: deployment/app/Dockerfile
  restart: unless-stopped
  env_file: [.env]
  networks: [srosha-net]
  depends_on:
    postgres: {condition: service_healthy}

services:
  postgres:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    command: ["postgres", "-c", "shared_buffers=256MB"]
    volumes:
      # /var/lib/postgresql, not /data -- see docs/CONFIG.md.
      - pgdata:/var/lib/postgresql
    networks: [srosha-net]
    deploy:
      resources:
        limits: {memory: 1G}
    healthcheck:
      # pg_isready proves the socket answers. It does not prove the role exists
      # or that a query would succeed -- the service's own /readyz runs a real
      # query, which is the whole reason it does.
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 3s
      retries: 15

  nats:
    image: nats:2.14-alpine
    restart: unless-stopped
    # nats-server.conf is mounted by Dokploy (Advanced -> Volumes -> File
    # Mount). It carries one user per binary; the local broker has none, so a
    # permission mistake cannot show up in development.
    command: ["-c", "/etc/nats/nats-server.conf", "-js", "-sd", "/data/jetstream", "-m", "8222"]
    volumes:
      - jetstream:/data/jetstream
    networks: [srosha-net]
    deploy:
      resources:
        limits: {memory: 1G}
    healthcheck:
      # /healthz reports JetStream readiness, not just that the process is up.
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8222/healthz"]
      interval: 5s
      timeout: 3s
      retries: 10
      start_period: 5s

  gateway:
    <<: *app
    command: ["/app/gateway"]
    depends_on:
      postgres: {condition: service_healthy}
      nats: {condition: service_healthy}
    networks: [srosha-net, dokploy-network]
    expose: ["50051", "8080"]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/readyz"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    labels:
      - traefik.enable=true
      - traefik.docker.network=dokploy-network
      - traefik.http.routers.srosha-api.rule=Host(`api.srosha.ir`)
      - traefik.http.routers.srosha-api.entrypoints=websecure
      - traefik.http.routers.srosha-api.tls.certresolver=letsencrypt
      - traefik.http.routers.srosha-api.service=srosha-api
      - traefik.http.services.srosha-api.loadbalancer.server.port=50051
      # h2c is not optional: gRPC is HTTP/2 without TLS behind the terminator,
      # and Traefik speaks HTTP/1.1 to a backend unless told otherwise.
      - traefik.http.services.srosha-api.loadbalancer.server.scheme=h2c

  dispatcher:
    <<: *app
    command: ["/app/dispatcher"]
    depends_on:
      postgres: {condition: service_healthy}
      nats: {condition: service_healthy}
    expose: ["8081"]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8081/readyz"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s

  console:
    <<: *app
    command: ["/app/console"]
    networks: [srosha-net, dokploy-network]
    expose: ["8090", "8091", "8092"]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8091/readyz"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    # Two routers on one container: the portal and the admin panel are two
    # listeners in one process, and they are separated by host and by cookie.
    # See docs/ARCHITECTURE.md.
    labels:
      - traefik.enable=true
      - traefik.docker.network=dokploy-network
      - traefik.http.routers.srosha-panel.rule=Host(`panel.srosha.ir`)
      - traefik.http.routers.srosha-panel.entrypoints=websecure
      - traefik.http.routers.srosha-panel.tls.certresolver=letsencrypt
      - traefik.http.routers.srosha-panel.service=srosha-panel
      - traefik.http.services.srosha-panel.loadbalancer.server.port=8090
      - traefik.http.routers.srosha-admin.rule=Host(`admin.srosha.ir`)
      - traefik.http.routers.srosha-admin.entrypoints=websecure
      - traefik.http.routers.srosha-admin.tls.certresolver=letsencrypt
      - traefik.http.routers.srosha-admin.service=srosha-admin
      - traefik.http.services.srosha-admin.loadbalancer.server.port=8092

  # Never up during a deploy -- that is what the profile buys. Run it yourself:
  #
  #   docker compose --profile migrate run --rm migrate
  #
  # Same image as the deploy, so the sql it applies is the sql of this commit.
  migrate:
    <<: *app
    profiles: ["migrate"]
    restart: "no"
    command: ["/app/goose", "up"]
    environment:
      GOOSE_DRIVER: postgres
      GOOSE_MIGRATION_DIR: /app/migrations
      GOOSE_DBSTRING: ${NOTIF_DB_DSN}

networks:
  srosha-net:
    external: true
  dokploy-network:
    external: true

volumes:
  pgdata:
  jetstream:
```

- [ ] **Step 2: Validate it**

```bash
docker compose -f deployment/app/docker-compose.yml config -q
```

Expected: silence, and warnings only about unset variables. An error here is a
syntax or anchor mistake; a warning naming `POSTGRES_PASSWORD` is correct — the
values live in Dokploy's Environment tab, never in the repository.

- [ ] **Step 3: Prove the profile keeps migrate out of a deploy**

```bash
docker compose -f deployment/app/docker-compose.yml config --services
docker compose -f deployment/app/docker-compose.yml --profile migrate config --services
```

Expected: five services in the first, six in the second. If `migrate` appears in
the first, every deploy would run a migration and the rule in `docs/CONFIG.md`
would be broken by the file that claims to follow it.

- [ ] **Step 4: Prove nothing is published**

```bash
grep -n 'ports:' deployment/app/docker-compose.yml || echo "no published ports"
```

Expected: `no published ports`. This is a one-line check for the rule the whole
file is built around, so it is worth running rather than assuming.

- [ ] **Step 5: Bring the stack up locally, as far as it goes**

```bash
docker network create srosha-net 2>/dev/null || true
docker network create dokploy-network 2>/dev/null || true
docker compose -f deployment/app/docker-compose.yml up -d postgres
docker compose -f deployment/app/docker-compose.yml ps
```

Expected: postgres healthy. Stop after this check with

```bash
docker compose -f deployment/app/docker-compose.yml down
```

The application services are **not** expected to come up here: they need a real
`.env` with crypto keys, SMTP and NATS credentials, and nats needs the
`nats-server.conf` that only Dokploy mounts. Getting postgres healthy proves the
file, the networks and the volumes are right, which is what can be proved on a
laptop.

- [ ] **Step 6: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-deployment-compose.md` in Persian. Record what
step 5 could **not** prove — a report that implies the stack was tested end to
end when postgres was the only thing that started is worse than no report. Then
**stop**.

---

### Task 3: CONFIG.md tells the truth about the deployment

**Files:**
- Modify: `docs/CONFIG.md` — the Deployment section, and Migrations

**Interfaces:**
- Consumes: the files built in tasks 1 and 2. Do this last so it describes what
  exists.

- [ ] **Step 1: Fix the Deployment table**

The row `Dockerfile | deployment/app/Dockerfile — one image, both binaries` is
wrong: there are three, plus goose and the migration files. The row
`Domain | on the gateway service only; the dispatcher has none` is wrong too —
the console carries two hosts.

Replace both, and add the three hostnames as data:

```
| api.srosha.ir   | gateway, gRPC over h2c |
| panel.srosha.ir | console, the customer portal |
| admin.srosha.ir | console, the admin panel |
```

- [ ] **Step 2: Record the goose pin**

In the Migrations section, add that the deployed migration runs from the image
with goose pinned at `v3.27.3`, invoked as
`docker compose --profile migrate run --rm migrate`, and that this is what
"a separate deployment step, never from an application entrypoint" means in
practice. Note the difference from `make setup-dev`, which installs `@latest`.

- [ ] **Step 3: Say that `srosha-net` and `dokploy-network` are both external**

The Networks table already says `srosha-net` is created by hand. Add that
`dokploy-network` is external too and belongs to Dokploy, so neither is created
by this compose file and a missing one is a deploy that fails at start.

- [ ] **Step 4: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-deployment-config-entries.md` in Persian. Then
**stop** — and, because `docs/CONFIG.md` entries are confirmed with the owner
before they are written, show the diff and wait rather than assuming.

---

## What this plan does not do

| | |
| --- | --- |
| CI | declined by the owner. The local hooks run `precommit` and `prepush` |
| `nats-server.conf` | mounted by Dokploy, not held in the repository. It is the most likely first-deploy failure and it is outside this plan |
| The first real deploy | pressing deploy in Dokploy, creating `srosha-net` on the server, and filling the Environment tab are the owner's, and they need secrets |
| Proving a channel sends | no channel has ever completed a real send. That is the next thing after this, and it needs a deployed stack to be possible at all |
