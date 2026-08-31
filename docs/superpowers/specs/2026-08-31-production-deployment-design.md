# srosha in production — design

srosha has never run anywhere but a laptop. Three binaries, eight channels, a
published SDK, a customer portal and an admin panel — and no Dockerfile, no
deployed compose, nothing that puts any of it on a server.

This specifies what does. It is written as **two parts, in order**, because the
first is a change to running code and the second is only files:

| | | |
| --- | --- | --- |
| **A** | The admin surface moves from a port to a host | code, and two documents that argue for the old shape |
| **B** | The image and the deployed stack | `Dockerfile`, `docker-compose.yml`, a one-shot migrate service |

They are separate branches. A comes first, and not for tidiness: without it, B
deploys an admin panel that nothing can reach, and an admin panel nothing can
reach means no source is ever approved, which means srosha sends nothing. The
stack would come up perfectly and be useless.

---

## What was already decided

`docs/CONFIG.md` has carried a Deployment section for a long time. Most of the
shape is not open:

| | |
| --- | --- |
| Host | one Linux server, Docker + Dokploy, shared with unrelated apps |
| Service type | **Compose with a Git source** — Dokploy builds the image on the server. There is no registry and nothing is pushed |
| Compose path | `deployment/app/docker-compose.yml` |
| Dockerfile | `deployment/app/Dockerfile` — one image, the binary chosen by `command` |
| Private network | `srosha-net`, `external: true`, created by hand |
| Traefik network | `dokploy-network`, joined **alongside** `srosha-net`, never instead of it |
| Published host ports | none. `ports:` is not used |
| Migrations | a separate deployment step, **never from an application entrypoint** |

`deployment/app/docker-compose.dev.yml` already exists and stays exactly as it
is. Its own header says the deployed one "is written on its own branch". This is
that branch.

**One line of that section is stale and this spec replaces it.** It says
"one image, **both** binaries" and "Domain on the gateway service only". It was
written when srosha had two binaries. It has three, and the third serves pages
to browsers.

---

## The thing that blocks everything

`internal/config/settings/console.go` refuses to start in production unless the
admin listener binds loopback:

```go
r.Check(!production || c.AdminAddr.bindsLoopback(),
    "NOTIF_ADMIN_ADDR must bind the loopback interface in production ...")
```

The comment above it says what it means by loopback: "an address that reaches
only **the machine it runs on**". That was true when the binary ran on a host.

Inside a container, the machine is the container:

```
console container
├── 127.0.0.1:8092   admin  ── reachable from inside this container. Only.
├── :8090            portal
└── :8091            healthz
```

Nothing reaches that listener. Not a `ports:` mapping — a published port maps to
the container's external interface, not to its loopback. Not another container
on `srosha-net` — that also arrives on the external interface. Not an SSH tunnel
to the host, for the same reason. The listener is alone in its own network
namespace.

So in production today, the admin panel exists and cannot be opened.

### Why the obvious workarounds were rejected

**Publish it on the host's loopback** (`127.0.0.1:8092:8092`, reached with
`ssh -L`) requires the process to bind the container's external interface, which
the guard refuses. It also puts a `ports:` line in a file whose whole discipline
is that it has none.

**Reach the container's IP over SSH** requires the process to bind the external
interface too, and the IP changes on every deploy.

**A sidecar sharing the network namespace** (`network_mode: "service:console"`)
satisfies the guard literally and defeats it in practice: a second container
whose only job is to punch a hole through the wall the first one built.

**Leave admin unreachable for now** is not viable. It is the only thing that
approves a source.

### The decision: the admin surface is public, on its own host

Not private-but-reachable. Public, through Traefik, at `admin.srosha.ir`.

This is a change to a documented security decision, so the argument is written
out rather than assumed.

**What authentication already gives, in numbers:**

| | |
| --- | --- |
| Code length | 6 digits |
| Guesses allowed | **3**, then the code is spent |
| Code lifetime | 10 minutes |
| Codes per address | 5 per hour |

A million values against three guesses. Brute force is not the risk and never
was.

**What the port separation was actually protecting against** was three facts
that are only dangerous together:

1. **Sign-in is self-serve.** `SignIn.find` creates the account for an address
   nobody has used. Anyone on the internet holds a valid session within a
   minute.
2. **That session is presented to the admin listener.** Cookies are not scoped
   by port — `web/const.go` says so itself: *"a cookie is not scoped by port, so
   a second name would not separate them anyway."*
3. So the distance from a stranger to the surface that can make somebody a
   `super_admin` is one boolean:

   ```go
   if !u.Role.IsOperator() { refuse }
   ```

   That line is correct and tested. It is also the only thing there, in a
   package where both surfaces are structs side by side because `web/admin` was
   impossible under `make arch-check`. The compiler is not helping.

**Fact 2 stops being true the moment the surfaces differ by host rather than by
port.** Cookies *are* scoped by host. A session cookie set on `panel.srosha.ir`
is not sent to `admin.srosha.ir` at all — not sent and refused, simply not sent.
Fact 3 stops being the only lock, because a customer's session cannot be
presented in the first place.

**What remains true and must be recorded:** an operator signs in exactly like a
customer, with a six-digit code to their mailbox. There is no second factor.
Public admin therefore means: *whoever reads an operator's email owns srosha*.
Today that attack also needs SSH access to the server. This is a real reduction
in defence and it is accepted deliberately, not overlooked. Two cheap answers
exist — a Traefik IP allowlist, or a second credential for operators — and
neither is in scope here.

---

## Part A — the admin surface moves from a port to a host

### A cookie per surface

Each surface already builds its own `sessions`:

```go
// portal.go
sessions := newSessions(d.SignIn, d.SecureCookie)
// admin.go
sessions := newSessions(d.SignIn, d.SecureCookie)
```

They differ only in that they are two values. `newSessions` gains a name:

```go
func newSessions(signIn SignIn, name string, secure bool) *sessions
```

| Surface | Cookie |
| --- | --- |
| portal | `srosha_portal` |
| admin | `srosha_admin` |

`sessionCookieName` in `web/const.go` becomes two constants, and the comment
above it is rewritten. Its current reasoning — that a second name would not
separate them anyway — was true and is not any more. The premise it rested on
was that the surfaces differ by port.

**The cookie must stay host-only.** `sessions.begin` sets no `Domain`
attribute today, which makes the cookie host-only, which is exactly what makes
this work. Setting `Domain=srosha.ir` would send it to every subdomain and undo
the whole change. That absence is load-bearing and gets a comment saying so.

An operator therefore signs in twice: once at `panel.srosha.ir` as the customer
they also are, once at `admin.srosha.ir` as an operator. That is a gain, not a
cost — today one cookie wears both hats and nothing on screen says which.

### The guard goes

`AdminAddr.bindsLoopback` and its `r.Check` are deleted. Inside the container
the admin listener binds `:8092` like any other service, and Traefik routes to
it.

The default stays `127.0.0.1:8092`, because for local development it is still
the right default — a laptop is a machine where loopback means what the old
comment thought it meant everywhere.

Nothing in the code will prevent the admin surface being on the internet after
this, because it is on the internet on purpose. What replaces the guard is the
cookie split, which is a stronger thing: the old guard made the panel hard to
reach, and the new arrangement makes a customer's session impossible to present.

### Tests

Two exist and stay:

```
TestOperatorPagesRefuseACustomer
TestNoAdminRouteAnswersOnThePortal
```

Three are added, and they are the point of the change rather than decoration:

| Test | Asserts |
| --- | --- |
| a portal cookie is not a session on the admin surface | the admin surface reads `srosha_admin` and nothing else — presenting `srosha_portal` with a valid session id is anonymous |
| the reverse | the portal reads `srosha_portal` and nothing else |
| neither cookie carries a `Domain` | `Set-Cookie` has no `Domain=` — the property the whole separation rests on |

Two config tests go with the guard they test:
`TestProductionRefusesAnAdminAddressOnEveryInterface` and
`TestProductionAcceptsALoopbackAdminAddress`. The ones that check
`SecureCookie` stay — that guard is untouched.

### Documents

`docs/ARCHITECTURE.md`, section *Two surfaces in one binary*, is rewritten. It
currently says:

```
:8092   admin    private, never published
```

and, further down, **"The admin port is never published. Not in `ports:`, and
not on `dokploy-network`."** Both become false. What replaces them is the same
structure with a different separator: three handlers with no shared mux, a live
role read on every request, a cookie per host, and the two route tests.

The subsection *Cookies are not scoped by port* is not deleted. It stays and
gains its ending: it is why the surfaces could not be separated by port, and why
they are separated by host instead.

`docs/CONFIG.md` gains `admin.srosha.ir` in the ports table and loses the
sentence about the admin port staying private.

---

## Part B — the image and the stack

### One image, three binaries, and goose

```
deployment/app/Dockerfile
```

A builder stage on `golang:1.26-alpine` — matching `go.mod`'s `go 1.26` —
compiles all three binaries and installs goose **pinned**, not `@latest`:

```
github.com/pressly/goose/v3/cmd/goose@v3.27.3
```

`v3.27.3` is what this repository is developed against today. The `Makefile`'s
`setup-dev` installs `@latest`, which is fine for a laptop and is not fine for
an image that has to be rebuildable; that difference is deliberate and is
recorded in `docs/CONFIG.md`.

The runtime stage is `alpine`, not distroless, for one concrete reason: compose
healthchecks need a command, and busybox `wget` is already what
`docker-compose.dev.yml` uses against nats. It carries `ca-certificates` —
SMTP, Telegram, Bale, WhatsApp, Matrix, Gotify, FCM and APNs are all TLS — and
runs as a non-root user.

It contains:

```
/app/gateway  /app/dispatcher  /app/console   the three binaries
/app/goose                                    the migration tool
/app/migrations/                              the sql, travelling with the code
```

`public/` is **not** copied. `go:embed` compiles the templates and assets into
the console binary, so a volume or a copy would be a second copy that can
disagree with the first.

Which binary runs is `command`, exactly as CONFIG already says.

### The stack

```
srosha-net (external, private)        dokploy-network (Traefik)
├── postgres                          ├── gateway    → api.srosha.ir
├── nats                              ├── console    → panel.srosha.ir
├── gateway ─────────────────────────►└── console    → admin.srosha.ir
├── dispatcher
├── console ─────────────────────────►
└── migrate   (profile: migrate — never up during a deploy)
```

| Service | Image | On dokploy-network | Notes |
| --- | --- | --- | --- |
| postgres | `postgres:18-alpine` | no | volume `pgdata` at `/var/lib/postgresql`, 1G, `shared_buffers=256MB` |
| nats | `nats:2.14-alpine` | no | `-js`, volume `jetstream` at `/data/jetstream`, `nats-server.conf` mounted by Dokploy, 1G |
| gateway | `srosha:latest` | **yes** | `command: ["/app/gateway"]` |
| dispatcher | `srosha:latest` | no | `command: ["/app/dispatcher"]` |
| console | `srosha:latest` | **yes** | `command: ["/app/console"]`, two routers |
| migrate | `srosha:latest` | no | `profiles: ["migrate"]`, `command: ["/app/goose", "up"]` |

**No `ports:` anywhere.** Every port is `expose:` on the private network. The
only things the internet reaches are the three Traefik routers.

**Health.** Every binary already serves `/healthz` and `/readyz` from
`internal/adapter/api/http/health.go`; `/readyz` runs a real query rather than
proving a socket answers. Compose healthchecks use `/readyz` on
`NOTIF_GRPC_HTTP_ADDR` (gateway, 8080) and `NOTIF_HTTP_ADDR` (dispatcher 8081,
console 8091). postgres and nats reuse the checks
`docker-compose.dev.yml` already justifies in comments.

Every srosha service waits on `postgres` and, where it speaks to it, `nats`,
with `condition: service_healthy`.

### Traefik

Three routers on two containers. The console carries two, which is ordinary —
two `loadbalancer.server.port` values on one container:

| Router | Host | Container port |
| --- | --- | --- |
| `srosha-api` | `api.srosha.ir` | 50051, scheme **h2c** |
| `srosha-panel` | `panel.srosha.ir` | 8090 |
| `srosha-admin` | `admin.srosha.ir` | 8092 |

`h2c` on the api router is not optional: gRPC is HTTP/2 without TLS behind the
terminator, and Traefik sends HTTP/1.1 to a backend unless told.

### Migrations

The one-shot service is the whole mechanism:

```bash
docker compose --profile migrate run --rm migrate
```

It never comes up during a deploy — that is what the profile buys — and it uses
the same image as the deploy, so the sql it applies is the sql from that commit.
It reads goose's own three keys, which `docs/CONFIG.md` already names:
`GOOSE_DRIVER`, `GOOSE_MIGRATION_DIR`, `GOOSE_DBSTRING`.

This satisfies CONFIG's existing rule — a separate step, never an entrypoint —
without needing goose installed on the server or the migration files copied
there by hand.

### Configuration

No new application configuration. Every key already exists and is documented in
`.env.example` and `docs/CONFIG.md`; the compose passes them through from
Dokploy's Environment tab.

Two values are what production means:

```
NOTIF_APP_ENV=production
NOTIF_CONSOLE_SECURE_COOKIE=true      # already refused if off in production
NOTIF_ADMIN_ADDR=:8092                # possible only after part A
```

`NOTIF_MQ_URL` carries a different NATS user per binary and must not be
collapsed into one — CONFIG says so and the deployed broker enforces it while
the local one cannot.

### `docs/CONFIG.md`

The Deployment section is corrected in the same commit as part B: three
binaries, three hostnames, the migrate profile, the pinned goose version, and
the `srosha-net` / `dokploy-network` split as it actually ends up.

---

## Out of scope, deliberately

| | |
| --- | --- |
| CI | asked for and declined. The local hooks run `precommit` and `prepush` |
| A second factor for operators | named as the remaining risk, not built |
| Traefik IP allowlist | the same |
| Pushing an image to a registry | Dokploy builds from Git; there is nothing to push |
| Backups, monitoring, log shipping | not this piece of work |

---

## Risks

**No channel has ever completed a real send.** All eight are tested and none has
delivered a message to a real recipient. Deploying does not change that; it
makes it findable.

**Gotify's endpoint is a documented guess** and it is now in a published SDK
tag. If the server wants a *client* token, the token moves to an `X-Gotify-Key`
header and `appid` becomes load-bearing. See the comment above
`(*Sender).endpoint`. First thing to test against the real server.

**The operator's mailbox becomes the only lock.** Stated above, accepted, and
worth revisiting the first time there is more than one operator.

**`nats-server.conf` is mounted by Dokploy, not held in the repository.** So the
one file that defines the broker's users and permissions is not versioned with
the code that depends on them, and a permission mistake cannot show up locally —
`docs/CONFIG.md` already says the local broker runs with no accounts at all.
Unchanged by this work, and the most likely source of a first-deploy failure.

**`srosha-net` is created by hand.** A network that exists because somebody once
ran a command is a network that will be missing on the day the server is
rebuilt.
