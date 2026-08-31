# Deployment stack — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the three binaries on the server: a `healthcheck` subcommand so
the image needs no shell tools, one image built from the repository root, and a
compose file that joins the infrastructure **that is already running** rather
than deploying a second copy of it.

**Architecture:** `postgres`, `nats` and the `srosha-net` network are already
live on the host as separate Dokploy Compose services. This compose carries only
`gateway`, `dispatcher`, `console` and a profiled `migrate`, joins `srosha-net`
to reach the existing broker and database, and adds `dokploy-network` **only**
to the services that carry a domain. Each binary answers `healthcheck` by
asking its own `/readyz`, so the runtime image contains no `wget`, no `curl` and
no shell.

**Tech Stack:** Docker, Docker Compose v2, Dokploy, Traefik, Go 1.26,
goose v3.27.3.

**Spec:** `docs/superpowers/specs/2026-08-31-production-deployment-design.md`
(part B), **corrected against**
`docs/reference/srosha-infra-deploy.md`, which `docs/CONFIG.md` calls facts
about the running system. Read sections 2, 3, 4 and 11 of that document before
task 2. The spec was written without it and the first version of this plan
would have deployed a second postgres and a second nats.

**Prerequisite:** `refactor/admin-on-its-own-host` must be merged. Without it
the console refuses to start with `NOTIF_ADMIN_ADDR=:8092` in production.

## Global Constraints

- **Never `git commit` without a direct, explicit order from the owner.** Each
  task ends with the work in the tree, checks green, report written — then stop.
- Every commit carries a change report under `docs/changes/`, in Persian,
  following `docs/changes/TEMPLATE.md`.
- Branch: `feat/deployment-stack`, cut from `master`. One branch and four
  commits: the subcommand exists only to serve the compose healthcheck, so it is
  one theme, and each commit still brings its own report.
- **`ports:` is never used.** `expose:` only. `80` and `443` belong to Traefik
  and must never appear.
- **Do not define `postgres` or `nats` as services.** They are already deployed.
  Defining them here creates a second database with an empty volume beside the
  real one.
- Services address each other by **compose service name** — `postgres`, `nats`
  — never by Dokploy's suffixed container names.
- Secrets come from Dokploy's Environment tab through `${...}`. There is no
  `.env` on the server and `env_file:` must not be used.
- Hostnames: `api.srosha.ir`, `panel.srosha.ir`, `admin.srosha.ir`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/bootstrap/healthcheck.go` | `Probe(addr)` — ask this process's own `/readyz` and report yes or no |
| `internal/bootstrap/healthcheck_test.go` | that it accepts 200, refuses 503, refuses a closed port, and dials a bare `:port` |
| `cmd/gateway/main.go`, `cmd/dispatcher/main.go`, `cmd/console/main.go` | recognise the `healthcheck` argument, each with its own health address |
| `deployment/app/Dockerfile` | three binaries and goose, built static and stripped; a runtime image with no shell |
| `deployment/app/.dockerignore` | keep the build context to what the build needs |
| `deployment/app/docker-compose.yml` | four services, two networks, no published ports |
| `docs/CONFIG.md` | the Deployment section corrected to three binaries and three hostnames |

---

### Task 1: A binary can answer whether it is ready

**Files:**
- Create: `internal/bootstrap/healthcheck.go`
- Create: `internal/bootstrap/healthcheck_test.go`
- Modify: `cmd/gateway/main.go`, `cmd/dispatcher/main.go`, `cmd/console/main.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `bootstrap.Probe(addr string) error`, and the argument `healthcheck`
  accepted by all three binaries. Task 3's compose calls
  `["/app/gateway", "healthcheck"]` and the two others.

**Why here.** `cmd` already imports `bootstrap` and `config` and nothing else,
so this adds no import edge and `make arch-check` is untouched. Parsing the
argument stays in `main` for the reason already written above the signal
handling there: argv is the process's own, like signals.

- [ ] **Step 1: Write the failing test**

`internal/bootstrap/healthcheck_test.go`:

```go
package bootstrap_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/bootstrap"
)

// Probe asks the readiness endpoint, which already answers 503 when a
// dependency is down. It adds no judgement of its own: the question was
// answered in health.go and asking it twice is how two answers start to
// disagree.
func TestProbeReportsWhatReadyzSaid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		wantErr bool
	}{
		{"ready", http.StatusOK, false},
		{"a dependency is down", http.StatusServiceUnavailable, true},
		{"something else entirely", http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/readyz" {
						t.Errorf("asked for %q, want /readyz", r.URL.Path)
					}
					w.WriteHeader(tc.code)
				}))
			defer srv.Close()

			err := bootstrap.Probe(strings.TrimPrefix(srv.URL, "http://"))
			if tc.wantErr && err == nil {
				t.Error("Probe said ready when the endpoint did not")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Probe = %v, want nil", err)
			}
		})
	}
}

// A listener that is not there is not ready. This is the case that runs while
// a container is still starting, so it must be an error and not a panic.
func TestProbeRefusesAClosedPort(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	if err := bootstrap.Probe(addr); err == nil {
		t.Error("Probe said ready with nothing listening")
	}
}

// The address comes from the same config the server binds, and a server binds
// ":8080" to mean every interface. A client cannot dial that, so the host has
// to be filled in -- and it must be loopback, because the probe runs inside the
// container it is asking about.
func TestProbeDialsLoopbackForABarePort(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { reached = true }))
	defer srv.Close()

	_, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")

	if err := bootstrap.Probe(":" + port); err != nil {
		t.Fatalf("Probe(%q) = %v", ":"+port, err)
	}
	if !reached {
		t.Error("a bare :port was not dialled on loopback")
	}
}
```

- [ ] **Step 2: Run it and watch it fail to compile**

```bash
go test -count=1 ./internal/bootstrap/
```

Expected: `undefined: bootstrap.Probe`. A build failure, not a red test.

- [ ] **Step 3: Write `Probe`**

`internal/bootstrap/healthcheck.go`:

```go
package bootstrap

import (
	"fmt"
	"net"
	"net/http"
	"time"
)

// probeTimeout bounds one health check. Compose gives the command five seconds;
// this is under that, so a slow answer is our error rather than docker's kill.
const probeTimeout = 3 * time.Second

// Probe asks this process's own /readyz and reports whether it said ready.
//
// It exists so the runtime image needs no wget, curl or shell: a docker
// healthcheck can run the binary itself. It adds no judgement of its own --
// readiness is decided in the adapter, and a second opinion here is a second
// answer that can disagree with the first.
func Probe(addr string) error {
	url := "http://" + dialable(addr) + "/readyz"

	client := http.Client{Timeout: probeTimeout}
	res, err := client.Get(url) //nolint:noctx // the timeout above is the budget
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s said %s", url, res.Status)
	}
	return nil
}

// dialable turns a listen address into one a client can reach.
//
// A server binds ":8080" or "0.0.0.0:8080" to mean every interface, and
// neither is dialable. The probe runs inside the container it is asking about,
// so the answer is always loopback.
func dialable(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
```

- [ ] **Step 4: Run the tests**

```bash
go test -count=1 ./internal/bootstrap/
```

Expected: PASS, all four cases.

- [ ] **Step 5: Teach the three binaries the argument**

Each `run()` already loads its config first. The argument is checked straight
after, because a config that will not load is not a healthy container either —
and that is a true answer, not a false negative.

`cmd/gateway/main.go`, immediately after `config.LoadGateway()` succeeds:

```go
	// argv is the process's own, like the signals below.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return bootstrap.Probe(cfg.GRPC.HTTPAddr)
	}
```

`cmd/dispatcher/main.go` and `cmd/console/main.go`, in the same place, with
their own address:

```go
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return bootstrap.Probe(cfg.HTTP.Addr)
	}
```

The gateway's health listener is `NOTIF_GRPC_HTTP_ADDR`; the other two are
`NOTIF_HTTP_ADDR`. They are different keys and must not be collapsed.

- [ ] **Step 6: Run it against a real process**

```bash
make dev-up && make build
NOTIF_APP_ENV=development ./build/gateway &
sleep 3
NOTIF_APP_ENV=development ./build/gateway healthcheck; echo "exit=$?"
kill %1
```

Expected: `exit=0` while the gateway is up. Then with nothing running:

```bash
NOTIF_APP_ENV=development ./build/gateway healthcheck; echo "exit=$?"
```

Expected: a non-zero exit and a message saying it is not ready. Both halves
matter — a check that always passes is worse than none.

- [ ] **Step 7: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-healthcheck-subcommand.md` in Persian, then
**stop**.

---

### Task 2: The image

**Files:**
- Create: `deployment/app/Dockerfile`
- Create: `deployment/app/.dockerignore`

**Interfaces:**
- Consumes: the `healthcheck` argument from task 1.
- Produces: an image tagged `srosha:latest` containing `/app/gateway`,
  `/app/dispatcher`, `/app/console`, `/app/goose` and `/app/migrations/`.

**Distroless, decided.** The runtime stage is
`gcr.io/distroless/static-debian12:nonroot`, which is what the `healthcheck`
subcommand makes possible: no shell, no package manager, no wget. The cost is
known and accepted — `docker exec … sh` does not exist, so a running container
cannot be poked at from inside. What replaces that is the binary itself: it
answers `healthcheck`, and its logs go to the docker json driver bounded below.
Do not substitute alpine to make a verification step easier; the steps below are
written for an image with no shell on purpose.

- [ ] **Step 1: Write the `.dockerignore`**

`deployment/app/.dockerignore` — the build context is the repository root:

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

`sdk/` is excluded deliberately: a separate Go module with its own tag, and
nothing in the three binaries imports it.

- [ ] **Step 2: Write the Dockerfile**

`deployment/app/Dockerfile`:

```dockerfile
# One image, three binaries, and the tool that migrates the database.
#
# Which binary runs is `command` in the compose file. Three images differing
# only in their entrypoint would be three builds of the same code, and three
# chances for them to be built from different commits.

FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Dependencies in their own layer, so a code change does not re-download the
# module graph.
COPY go.mod go.sum ./
RUN go mod download

# Pinned, deliberately. `make setup-dev` installs goose @latest, which is right
# for a laptop and wrong for an image that has to be rebuildable: the tool that
# migrates a production database must not change because a day passed.
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.3

COPY . .

# Static, so the runtime image needs no libc at all. -s -w drops the symbol
# table and DWARF; -trimpath keeps the build machine's paths out of the binary.
ENV CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o /out/gateway    ./cmd/gateway \
 && go build -ldflags="-s -w" -trimpath -o /out/dispatcher ./cmd/dispatcher \
 && go build -ldflags="-s -w" -trimpath -o /out/console    ./cmd/console


# distroless/static carries ca-certificates, tzdata and a nonroot user, and
# nothing else -- no shell, no package manager, no wget.
#
# Certificates matter: every channel is TLS, and without them each send fails
# with an opaque x509 error that reads like a bug in our code. The absence of
# wget is why cmd/* learned `healthcheck`.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/gateway /out/dispatcher /out/console ./
COPY --from=build /go/bin/goose ./goose

# The sql travels with the code, so a migration can never come from a different
# commit than the binaries that expect it.
COPY migrations/ ./migrations/

USER nonroot

# No ENTRYPOINT and no CMD on purpose: every service names its own command, and
# a default here would be a fourth answer to a question already answered three
# times.
```

`public/` is **not** copied. `go:embed` compiles the templates and assets into
the console binary; a copy here would be a second one that can disagree.

- [ ] **Step 3: Build it**

```bash
docker build -f deployment/app/Dockerfile -t srosha:latest .
```

Run it from the repository root. Expected: a successful build.

- [ ] **Step 4: Prove what is in it, without a shell**

There is no `ls` in this image, so the checks are the binaries themselves:

```bash
docker run --rm srosha:latest /app/goose -version
docker run --rm srosha:latest /app/gateway healthcheck; echo "exit=$?"
```

Expected: `goose version: v3.27.3`; and for the second, a **non-zero** exit
with a message about configuration or readiness — which proves the binary is
there, runs, and its healthcheck path works. A zero exit here would be wrong:
nothing is listening.

```bash
docker image inspect srosha:latest --format '{{.Config.User}} {{.Size}}'
```

Expected: `nonroot` and a size in the tens of megabytes, not hundreds.

- [ ] **Step 5: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-deployment-image.md` in Persian, then **stop**.

---

### Task 3: The deployed compose

**Files:**
- Create: `deployment/app/docker-compose.yml`

**Interfaces:**
- Consumes: everything from tasks 1 and 2.
- Produces: services `gateway`, `dispatcher`, `console`, `migrate`; the two
  external networks.

**Read `docs/reference/srosha-infra-deploy.md` §3 first.** Trap 1 is not
theoretical: attaching a domain makes Dokploy **replace** a service's network
with `dokploy-network` rather than add to it, so a service that lists only
`srosha-net` loses its database and queue the moment it gets a domain — with no
deploy-time error, only runtime failures. Both networks are listed explicitly
on `gateway` and `console` for exactly this reason, and `console` carries two
domains.

- [ ] **Step 1: Write the file**

`deployment/app/docker-compose.yml`:

```yaml
# The application only. postgres, nats and the srosha-net network are already
# deployed as their own Dokploy compose services -- see
# docs/reference/srosha-infra-deploy.md section 2. Defining them here would
# bring up a second database beside the real one, with an empty volume.
#
# Nothing publishes a host port. The only way in is Traefik, and only for the
# three routers below.

x-logging: &logging
  # Unbounded json logs are the most common way a small host runs out of disk,
  # and when the disk fills postgres dies with everything else.
  driver: json-file
  options:
    max-size: "10m"
    max-file: "3"

x-build: &build
  context: ../..
  dockerfile: deployment/app/Dockerfile

services:
  gateway:
    build: *build
    image: srosha:latest
    command: ["/app/gateway"]
    restart: unless-stopped
    expose: ["50051", "8080"]
    environment:
      NOTIF_APP_ENV: ${NOTIF_APP_ENV}
      NOTIF_GRPC_ADDR: ${NOTIF_GRPC_ADDR}
      NOTIF_GRPC_HTTP_ADDR: ${NOTIF_GRPC_HTTP_ADDR}
      NOTIF_DB_DSN: ${NOTIF_DB_DSN}
      # Its own NATS user. See the permission table in the infra document --
      # a gateway credential cannot read anyone's notifications, and that
      # split is verified by test.
      NOTIF_MQ_URL: ${NOTIF_GATEWAY_MQ_URL}
      NOTIF_CRYPTO_KEYS: ${NOTIF_CRYPTO_KEYS}
      NOTIF_CRYPTO_KEY_ID: ${NOTIF_CRYPTO_KEY_ID}
      NOTIF_RATELIMIT_PER_MINUTE: ${NOTIF_RATELIMIT_PER_MINUTE}
      NOTIF_RETENTION_AGE: ${NOTIF_RETENTION_AGE}
      NOTIF_TELEMETRY_LOG_LEVEL: ${NOTIF_TELEMETRY_LOG_LEVEL}
    healthcheck:
      test: ["CMD", "/app/gateway", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 20s
    deploy:
      resources:
        limits: {memory: 512M}
    # Both, explicitly. A domain would otherwise replace srosha-net and take
    # the database and the broker with it -- infra document, Trap 1.
    networks: [srosha-net, dokploy-network]
    logging: *logging
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
    build: *build
    image: srosha:latest
    command: ["/app/dispatcher"]
    restart: unless-stopped
    expose: ["8081"]
    environment:
      NOTIF_APP_ENV: ${NOTIF_APP_ENV}
      NOTIF_HTTP_ADDR: ${NOTIF_DISPATCHER_HTTP_ADDR}
      NOTIF_DB_DSN: ${NOTIF_DB_DSN}
      NOTIF_MQ_URL: ${NOTIF_DISPATCHER_MQ_URL}
      NOTIF_CRYPTO_KEYS: ${NOTIF_CRYPTO_KEYS}
      NOTIF_CRYPTO_KEY_ID: ${NOTIF_CRYPTO_KEY_ID}
      NOTIF_RETENTION_AGE: ${NOTIF_RETENTION_AGE}
      NOTIF_SENDER_SMTP_HOST: ${NOTIF_SENDER_SMTP_HOST}
      NOTIF_SENDER_SMTP_PORT: ${NOTIF_SENDER_SMTP_PORT}
      NOTIF_SENDER_SMTP_USER: ${NOTIF_SENDER_SMTP_USER}
      NOTIF_SENDER_SMTP_PASSWORD: ${NOTIF_SENDER_SMTP_PASSWORD}
      NOTIF_SENDER_TELEGRAM_TOKEN: ${NOTIF_SENDER_TELEGRAM_TOKEN}
      NOTIF_SENDER_BALE_TOKEN: ${NOTIF_SENDER_BALE_TOKEN}
      NOTIF_TELEMETRY_LOG_LEVEL: ${NOTIF_TELEMETRY_LOG_LEVEL}
    healthcheck:
      test: ["CMD", "/app/dispatcher", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 20s
    deploy:
      resources:
        limits: {memory: 512M}
    # No dokploy-network: no domain, and it must not be routable.
    networks: [srosha-net]
    logging: *logging

  console:
    build: *build
    image: srosha:latest
    command: ["/app/console"]
    restart: unless-stopped
    expose: ["8090", "8091", "8092"]
    environment:
      NOTIF_APP_ENV: ${NOTIF_APP_ENV}
      # NOTIF_HTTP_ADDR defaults to :8081, which is the dispatcher's. The
      # console's health listener is 8091 and must be set.
      NOTIF_HTTP_ADDR: ${NOTIF_CONSOLE_HTTP_ADDR}
      NOTIF_PORTAL_ADDR: ${NOTIF_PORTAL_ADDR}
      NOTIF_ADMIN_ADDR: ${NOTIF_ADMIN_ADDR}
      NOTIF_ADMIN_LIST_LIMIT: ${NOTIF_ADMIN_LIST_LIMIT}
      NOTIF_DB_DSN: ${NOTIF_DB_DSN}
      NOTIF_CRYPTO_KEYS: ${NOTIF_CRYPTO_KEYS}
      NOTIF_CRYPTO_KEY_ID: ${NOTIF_CRYPTO_KEY_ID}
      NOTIF_CONSOLE_SMTP_HOST: ${NOTIF_CONSOLE_SMTP_HOST}
      NOTIF_CONSOLE_SMTP_PORT: ${NOTIF_CONSOLE_SMTP_PORT}
      NOTIF_CONSOLE_SMTP_USER: ${NOTIF_CONSOLE_SMTP_USER}
      NOTIF_CONSOLE_SMTP_PASSWORD: ${NOTIF_CONSOLE_SMTP_PASSWORD}
      NOTIF_CONSOLE_SMTP_FROM: ${NOTIF_CONSOLE_SMTP_FROM}
      NOTIF_CONSOLE_SECURE_COOKIE: ${NOTIF_CONSOLE_SECURE_COOKIE}
      NOTIF_TELEMETRY_LOG_LEVEL: ${NOTIF_TELEMETRY_LOG_LEVEL}
    healthcheck:
      test: ["CMD", "/app/console", "healthcheck"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 20s
    deploy:
      resources:
        limits: {memory: 512M}
    networks: [srosha-net, dokploy-network]
    logging: *logging
    # Two routers on one container. The portal and the admin panel are two
    # listeners in one process, separated by host and by cookie -- see
    # docs/ARCHITECTURE.md.
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

  # Never up during a deploy -- that is what the profile buys. Run it yourself,
  # BEFORE merging the code that depends on the migration:
  #
  #   docker compose --profile migrate run --rm migrate
  #
  # Same image as the deploy, so the sql applied is the sql of this commit.
  migrate:
    build: *build
    image: srosha:latest
    profiles: ["migrate"]
    restart: "no"
    command: ["/app/goose", "up"]
    environment:
      GOOSE_DRIVER: postgres
      GOOSE_MIGRATION_DIR: /app/migrations
      GOOSE_DBSTRING: ${NOTIF_DB_DSN}
    networks: [srosha-net]
    logging: *logging

networks:
  srosha-net:
    external: true
  dokploy-network:
    external: true
```

- [ ] **Step 2: Validate it**

```bash
docker compose -f deployment/app/docker-compose.yml config -q
```

Expected: silence, and warnings only about unset variables. A warning naming
`NOTIF_DB_DSN` is correct — values live in Dokploy's Environment tab, never in
the repository.

- [ ] **Step 3: Prove the four rules the file exists for**

```bash
grep -c 'ports:' deployment/app/docker-compose.yml || echo "0 published ports"
grep -c 'env_file' deployment/app/docker-compose.yml || echo "0 env_file"
docker compose -f deployment/app/docker-compose.yml config --services
docker compose -f deployment/app/docker-compose.yml --profile migrate config --services
```

Expected, in order: no `ports:`; no `env_file`; **three** services and not five
— if `postgres` or `nats` appears, the file would deploy a second copy of live
infrastructure; and four with the profile.

- [ ] **Step 4: Prove both networks survive on the two routed services**

```bash
docker compose -f deployment/app/docker-compose.yml config \
  | grep -A4 -E '^\s+(gateway|console|dispatcher):' | grep -B1 -A3 networks
```

Expected: `gateway` and `console` list `srosha-net` **and**
`dokploy-network`; `dispatcher` lists only `srosha-net`. This is Trap 1, and it
is worth checking in the file as well as in Dokploy's Preview Compose.

- [ ] **Step 5: Bring up what can be brought up**

The application services cannot start on a laptop: they need real crypto keys,
SMTP and NATS credentials, and the broker that lives on the server. What can be
proved here is that the file, the networks and the build all work:

```bash
docker network create srosha-net 2>/dev/null || true
docker network create dokploy-network 2>/dev/null || true
docker compose -f deployment/app/docker-compose.yml build gateway
```

Expected: the build succeeds. Do not claim more than this was proved.

- [ ] **Step 6: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-deployment-compose.md` in Persian. Record what
step 5 could **not** prove — a report that implies the stack was tested end to
end when only a build ran is worse than no report. Then **stop**.

---

### Task 4: CONFIG.md tells the truth about the deployment

**Files:**
- Modify: `docs/CONFIG.md` — Deployment, Networks, Migrations
- Modify: `internal/config/settings/console.go` — one stale comment

**Interfaces:**
- Consumes: tasks 1–3. Do this last so it describes what exists.

- [ ] **Step 1: A leftover from the previous branch**

`internal/config/settings/console.go`, the doc comment on `AdminAddr`, still
says the panel is "Never published" and that staying off the network is "a
property of the process and not only a deployment fact". The guard that made
that true was deleted in `a862f85` and this comment was missed. Rewrite it to
say what is true: the default is loopback because that is right for a laptop,
and in a deployment the surface is served on `admin.srosha.ir` with a cookie
per host keeping customers out.

- [ ] **Step 2: Fix the Deployment table**

Two rows are wrong. `Dockerfile | ... one image, both binaries` — there are
three, plus goose and the migration files. `Domain | on the gateway service
only; the dispatcher has none` — the console carries two hosts.

Add the three hostnames as data, and add a row saying this compose defines the
**application only**, because postgres and nats are separate Dokploy services.

- [ ] **Step 3: Fix the Networks table**

It says `srosha-net` carries "Both binaries plus postgres and nats" and that
`dokploy-network` is "**Gateway only**". Three binaries now, and the console is
on `dokploy-network` too. Say that both networks are `external: true` and
neither is created by this file, so a missing one is a deploy that fails at
start.

- [ ] **Step 4: Record the migrate mechanism and the goose pin**

In Migrations, add that the deployed migration runs from the image with goose
pinned at `v3.27.3`, invoked as
`docker compose --profile migrate run --rm migrate`, run **before** merging the
code that depends on it, and that `make setup-dev` installs `@latest` instead —
right for a laptop, wrong for an image.

- [ ] **Step 5: Check and report**

```bash
go build ./... && make precommit
```

Write `docs/changes/2026-08-31-deployment-config-entries.md` in Persian. Then
**stop**, and show the `docs/CONFIG.md` diff rather than assuming: entries in
that file are confirmed with the owner before they are written.

---

## What this plan does not do

| | |
| --- | --- |
| CI | declined by the owner |
| Deploying postgres or nats | already live; touching them is a separate decision |
| `nats-server.conf` | mounted by Dokploy, not in the repository. The one file defining the broker's users and permissions is not versioned with the code that depends on them, and the local broker has no accounts at all, so a permission mistake cannot show up in development |
| The gRPC health protocol | the infra document asks for `grpc.health.v1.Health` on the gateway beside `/healthz`. Not implemented, and not needed by this compose, which calls the binary rather than probing over gRPC |
| The first real deploy | Dokploy settings, the Environment tab and creating `srosha-net` are the owner's, and need secrets |
| Proving a channel sends | no channel has ever completed a real send. That comes after this, and needs a deployed stack to be possible at all |
