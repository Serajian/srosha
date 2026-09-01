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
| Compose path | `docker-compose.yml`, at the **repository root** — the application only, see below |
| Dockerfile | `deployment/app/Dockerfile` — one image, four binaries: the three services and `migrate` |
| Ignore file | `.dockerignore` at the **repository root**, because that is the build context |
| Image | `srosha:latest`; the binary is selected by `command` |
| Runtime base | `alpine:3.22` — distroless was tried and the server cannot reach `gcr.io` |
| Isolated Deployment | OFF |

| Hostname | Reaches |
| --- | --- |
| `api.srosha.ir` | gateway, gRPC — the router sets `scheme=h2c` |
| `panel.srosha.ir` | console, the customer portal |
| `admin.srosha.ir` | console, the admin panel |

**The deployed compose is at the repository root, and has to be.** Dokploy runs
`docker compose --project-directory <repo root>`, and compose resolves both the
build context and the `.env` it substitutes `${...}` from against that
directory — while Dokploy writes that `.env` beside the compose file. With the
file one level down those are two different directories: the build context
resolves outside the checkout and every `${...}` becomes an empty string. Both
were seen on 2026-09-01, when Dokploy started passing `--project-directory`.

**The compose file deploys the application and nothing else.** `postgres` and
`nats` are already live as their own Dokploy compose services; defining them
again would bring up a second database beside the real one, with an empty
volume. See `docs/reference/srosha-infra-deploy.md`.

### Networks

| Name | Purpose |
| --- | --- |
| `srosha-net` | private bridge, `external: true`, created by hand. All three binaries, plus the separately-deployed postgres and nats. |
| `dokploy-network` | Traefik's shared network, `external: true`. **Only the services that carry a domain** — gateway and console — and it must be declared *alongside* `srosha-net`, never instead of it. |

Neither network is created by the compose file, so a missing one is a deploy
that fails at start. Listing both on a service with a domain is not tidiness:
attaching a domain makes Dokploy **replace** the service's network rather than
add to it, so a service listing only `srosha-net` loses its database and broker
the moment it gets a domain — with no deploy-time error.

### Watch paths for auto-deploy

```
cmd/**  internal/**  pkg/**  api/**  sdk/go/notification/**  sdk/go/go.mod
sdk/go/go.sum  migrations/**  go.mod  go.sum  deployment/app/**
docker-compose.yml
```

---

## Services and ports

Nothing publishes a host port. Every port below is `expose:` on the private
network; `ports:` is never used.

| Service | Port | Purpose |
| --- | --- | --- |
| gateway | 50051 | gRPC — `api.srosha.ir`, h2c behind the terminator |
| gateway | 8080 | `/healthz` |
| dispatcher | 8081 | `/healthz` |
| console | 8090 | the customer portal's pages — `panel.srosha.ir` |
| console | 8091 | `/healthz` |
| console | 8092 | the admin surface — `admin.srosha.ir`, see ARCHITECTURE.md |
| nats | 4222 | clients |
| nats | 8222 | monitoring JSON — unauthenticated, never published |
| postgres | 5432 | clients |

There is no REST surface and none is planned. srosha is called by other services,
not by browsers, and gRPC is what those speak: a second surface would be a second
contract to keep the first one honest against, for callers that do not exist.

That is about the **customer API**, which is still gRPC and still the only way to
send anything. The console serves HTML to people, which is not a second API: it
has no contract, no versioning and no client — nothing outside a browser is meant
to parse it, and nothing does.

Three ports in this table are reachable from outside — the gateway's gRPC, the
console's portal and the console's admin surface — and all three go out through
Traefik on `dokploy-network`, never through `ports:`. The admin surface is public
deliberately; what keeps a customer out of it is a cookie per host rather than a
network, and `docs/ARCHITECTURE.md` is where that is argued.

The health ports carry no API at all. `/healthz` is there for the platform to
decide whether a container is alive; nothing a customer writes should ever call
it, and nothing about it is a promise to them.

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

`migrate` is a fourth binary and reads almost nothing: `NOTIF_APP_ENV`,
`NOTIF_DB_DSN`, `NOTIF_MIGRATION_LOCK_TIMEOUT` and the telemetry keys. It has no
broker, no crypto keys and no sending credentials — a tool that runs before the
service starts should not need the service's secrets.

| Group | Keys | gateway | dispatcher | console |
| --- | --- | --- | --- | --- |
| app | `NOTIF_APP_ENV`, `NOTIF_APP_SERVICE_NAME`, `NOTIF_APP_SHUTDOWN_TIMEOUT` | ✅ | ✅ | ✅ |
| grpc | `NOTIF_GRPC_ADDR`, `NOTIF_GRPC_HTTP_ADDR`, `NOTIF_GRPC_STOP_TIMEOUT` | ✅ | — | — |
| auth | `NOTIF_AUTH_KEY_TOUCH_AFTER` | ✅ | — | — |
| http | `NOTIF_HTTP_ADDR` | — | ✅ | ✅ |
| http server | `NOTIF_HTTP_SERVER_READ_HEADER_TIMEOUT`, `NOTIF_HTTP_SERVER_READ_TIMEOUT`, `NOTIF_HTTP_SERVER_WRITE_TIMEOUT`, `NOTIF_HTTP_SERVER_IDLE_TIMEOUT` | ✅ | ✅ | ✅ |
| http client | `NOTIF_HTTP_CLIENT_TIMEOUT`, `NOTIF_HTTP_CLIENT_DIAL_TIMEOUT`, `NOTIF_HTTP_CLIENT_TLS_TIMEOUT`, `NOTIF_HTTP_CLIENT_MAX_IDLE_CONNS`, `NOTIF_HTTP_CLIENT_MAX_IDLE_PER_HOST`, `NOTIF_HTTP_CLIENT_IDLE_CONN_TIMEOUT` | — | ✅ | ✅ |
| db | `NOTIF_DB_DSN`, `NOTIF_DB_MAX_CONNS`, `NOTIF_DB_MAX_CONN_LIFETIME`, `NOTIF_DB_MAX_CONN_IDLE_TIME`, `NOTIF_DB_HEALTH_CHECK_PERIOD`, `NOTIF_DB_CONNECT_TIMEOUT`, `NOTIF_DB_CONNECT_ATTEMPTS`, `NOTIF_DB_CONNECT_BACKOFF` | ✅ | ✅ | ✅ |
| mq | `NOTIF_MQ_URL`, `NOTIF_MQ_STREAM`, `NOTIF_MQ_DUPLICATE_WINDOW`, `NOTIF_MQ_MAX_AGE`, `NOTIF_MQ_CONNECT_TIMEOUT`, `NOTIF_MQ_MAX_RECONNECTS`, `NOTIF_MQ_RECONNECT_WAIT`, `NOTIF_MQ_DRAIN_TIMEOUT` | ✅ | ✅ | — |
| ratelimit | `NOTIF_RATELIMIT_PER_MINUTE` | ✅ | — | — |
| crypto | `NOTIF_CRYPTO_KEYS`, `NOTIF_CRYPTO_KEY_ID` | ✅ | ✅ | ✅ |
| dispatch | `NOTIF_DISPATCH_MAX_ATTEMPTS`, `NOTIF_DISPATCH_ACK_WAIT`, `NOTIF_DISPATCH_MAX_IN_FLIGHT` | — | ✅ | — |
| sender | `NOTIF_SENDER_SMTP_*`, `NOTIF_SENDER_TELEGRAM_TOKEN`, `NOTIF_SENDER_BALE_TOKEN`, `NOTIF_SENDER_WHATSAPP_TOKEN`, `NOTIF_SENDER_WHATSAPP_PHONE_NUMBER_ID`, `NOTIF_SENDER_MATRIX_TOKEN`, `NOTIF_SENDER_MATRIX_HOMESERVER`, `NOTIF_SENDER_GOTIFY_TOKEN`, `NOTIF_SENDER_GOTIFY_SERVER_URL`, `NOTIF_SENDER_FCM_SERVICE_ACCOUNT`, `NOTIF_SENDER_APNS_*` | — | ✅ | — |
| webhook policy | `NOTIF_WEBHOOK_ALLOW_INSECURE_URL`, `NOTIF_WEBHOOK_ALLOW_PRIVATE_URL` | ✅ | ✅ | ✅ |
| webhook | `NOTIF_WEBHOOK_TIMEOUT`, `NOTIF_WEBHOOK_MAX_FAILURES` | — | ✅ | — |
| telemetry | `NOTIF_TELEMETRY_LOG_LEVEL`, `NOTIF_TELEMETRY_LOG_FORMAT`, `NOTIF_TELEMETRY_LOG_SOURCE` | ✅ | ✅ | ✅ |
| alerts | `NOTIF_ALERT_GOTIFY_SERVER_URL`, `NOTIF_ALERT_GOTIFY_TOKEN`, `NOTIF_ALERT_QUEUE`, `NOTIF_ALERT_TIMEOUT`, `NOTIF_ALERT_READY_EVERY` | ✅ | ✅ | ✅ |
| console | `NOTIF_CONSOLE_SMTP_HOST`, `NOTIF_CONSOLE_SMTP_PORT`, `NOTIF_CONSOLE_SMTP_USER`, `NOTIF_CONSOLE_SMTP_PASSWORD`, `NOTIF_CONSOLE_SMTP_FROM`, `NOTIF_CONSOLE_SMTP_TIMEOUT`, `NOTIF_CONSOLE_SECURE_COOKIE`, `NOTIF_CONSOLE_TRIAL_PER_MINUTE` | — | — | ✅ |
| portal | `NOTIF_PORTAL_ADDR` | — | — | ✅ |
| admin | `NOTIF_ADMIN_ADDR`, `NOTIF_ADMIN_LIST_LIMIT` | — | — | ✅ |

`NOTIF_MQ_URL` carries a **different** NATS user per binary. Do not collapse them.

### Operator alerts

Both Gotify values empty means alerts are off, which is every laptop. Set, they
carry what an operator has to act on: every audited change, whether each binary
came up, and a dependency going down or coming back.

**It does not go through srosha's own pipeline.** An alert that travelled the
path it reports on would be silent exactly when it matters, so the alerter holds
its own http client and reaches Gotify directly.

| Key | |
| --- | --- |
| `NOTIF_ALERT_GOTIFY_SERVER_URL` | the operator's own Gotify. https only — the token travels in the query string |
| `NOTIF_ALERT_GOTIFY_TOKEN` | an application token. **Secret** — Dokploy's Environment tab, never here |
| `NOTIF_ALERT_QUEUE` | how many alerts may wait before one is dropped. Default 64 |
| `NOTIF_ALERT_TIMEOUT` | one push. Default 10s. Nothing waits on it |
| `NOTIF_ALERT_READY_EVERY` | how often a binary asks itself whether its dependencies are there. Default 30s |

**There is no application id key, and there should not be.** Gotify ignores the
`appid` parameter entirely: the token is what selects the application. Verified
on 2026-09-01 against a real server — a message sent with `appid=999`, which
does not exist, landed in the token's own application exactly like one sent with
the right id and one sent with none.

**Whoever holds that token sees customer email addresses.** An alert for
`source.create` names the customer who registered, because the actor of that
audited action is the customer. That is the same visibility `/audit` has, and
the reason `/audit` is `super_admin` only. Treat the token accordingly.

### Names that exist only in the compose file

These are not application config keys. They are substitution names the deployed
compose reads from Dokploy's Environment tab, because one application key needs
a different value per service:

| Compose name | Feeds | Why it is separate |
| --- | --- | --- |
| `NOTIF_GATEWAY_MQ_URL` | the gateway's `NOTIF_MQ_URL` | one NATS user per binary, never shared |
| `NOTIF_DISPATCHER_MQ_URL` | the dispatcher's `NOTIF_MQ_URL` | the same |
| `NOTIF_DISPATCHER_HTTP_ADDR` | the dispatcher's `NOTIF_HTTP_ADDR` | `:8081` |
| `NOTIF_CONSOLE_HTTP_ADDR` | the console's `NOTIF_HTTP_ADDR` | `:8091`. The key's own default is `:8081`, which is the dispatcher's, so the console's health listener would sit on the wrong port if this were left out |

WhatsApp needs two values, not one: Meta identifies the sending number
separately from the account that owns it. The id goes in the url and the token
in a header, and a source registering its own supplies the id as
`phone_number_id` in the credential's settings.

Matrix needs a homeserver as well as a token, and it is the one address in this
service a source chooses rather than a constant somewhere: the protocol is
federated, so there is no host that is right for everybody. It must be https —
an access token over plain http is a token in the clear.

Gotify needs a server url as well as an application token, and it is the one
address in this service a source chooses rather than a constant somewhere:
Gotify is self-hosted, so there is no host that is right for everybody. It
must be https — the application token travels as a query parameter, not a
header, so an http address would carry it in the clear.

The application token is the secret, sealed like every other credential's. It
is also the whole of the addressing **at Gotify's end**: the token decides
which application a message lands in, and the per-message address selects
nothing there. It is still load-bearing here — the duplicate guard is
`UNIQUE (notification_id, channel, address)`, so it is what keeps two Gotify
deliveries in one message from collapsing into one.

Verified 2026-09-01 against a stock Gotify 2.6.3, after months as a documented
guess: a message sent with `appid=999`, which did not exist, arrived in the
token's own application exactly like one sent with the right id and one sent
with none.

`NOTIF_SENDER_FCM_SERVICE_ACCOUNT` is **base64 of the whole service account json**,
and it is the only key in this file that is encoded. A service account is
multi-line json with a PEM private key inside it, and `.env` files, compose files
and secret managers each mangle that differently. Produce it with
`base64 -i service-account.json | tr -d '\n'`.

Nothing else is needed for FCM: the project id is inside the file. And the
encoding is an environment concern only — a source registering its own service
account sends the json itself.

APNs needs five: `NOTIF_SENDER_APNS_KEY` (base64 of the `.p8` file, for the same
reason), plus `_KEY_ID`, `_TEAM_ID`, `_TOPIC` and `_ENVIRONMENT`. Only the key is
secret — the key id is in the file's name, the team id names the developer
account, and the topic is the app's bundle id, which ships inside every copy of
the app.

`_ENVIRONMENT` is `production` or `sandbox`, defaulting to `production`. They are
**separate services with separate device tokens**: a token from a development
build is unknown to production, and the answer is `BadDeviceToken`, which reads
as a problem with the device when the mistake was the address of the service.

`NOTIF_CRYPTO_KEYS` is a JSON map of key id → key, one entry per key, and every
stored value names the key that sealed it. `NOTIF_CRYPTO_KEY_ID` says which of
them new values use. Rotation is therefore: add the second key, point the id at
it, let old values reseal themselves as they are read, and drop the first when
no value names it. No step stops the service.

Each key is 32 bytes for AES-256, standard base64 in the variable. Generate with
`openssl rand -base64 32`. **All three binaries read them** — the gateway seals a
credential when a source registers one over gRPC, the console seals one when a
customer registers it on a page and issues a callback's signing secret, and the
dispatcher opens both to send.

The console also reads the **webhook policy**, and only the policy: it validates
a callback address when a customer registers one, and never makes the callback.
The signing secrets are the dispatcher's and are not loaded there.

It reads the **http client** group too, which until this was the dispatcher's
alone. The senders page has a button that sends a real test message, so the
console opens a source's own credential and calls a provider directly. It holds
no sending identity of its own — the registry it builds has an empty fallback —
so it can never send as srosha. None of `NOTIF_SENDER_*` is read there, and
that is the boundary rather than an omission. See `docs/ARCHITECTURE.md`.

`NOTIF_CONSOLE_TRIAL_PER_MINUTE` caps that button per source, default `3`. **It
is not the source's sending quota and cannot be**: the gateway's limiter is a
separate bucket in a separate process, so a test costs a customer no message.
Zero is refused at startup — a cap of zero is not a limit, it is the button
broken forever.

`NOTIF_CONSOLE_*`, `NOTIF_PORTAL_ADDR` and `NOTIF_ADMIN_ADDR` are deliberately
separate groups, because one names the **binary** and the other two name a
**surface** each. The console carries the customer portal and the admin surface
in one process, sharing one mail account and one cookie rule, and each surface
brings its own address — because which of them is exposed is the whole security
argument.

`NOTIF_ADMIN_ADDR` defaults to `127.0.0.1:8092`. That is **a default and nothing
more** — it is right for a laptop, where the panel should not be on the network,
and it is wrong in a container, where loopback is the container's own namespace
and reaches nothing at all. Deployed, the admin surface listens like any other
service and Traefik routes to it by host.

**Nothing checks this value, and that is deliberate.** There used to be a
production guard demanding loopback, and it made the panel unopenable in the
container it was meant to protect — no source could be approved, so nothing
could ever be sent. What keeps a customer out of the admin surface is not the
network: it is a cookie scoped to `admin.srosha.ir`, a role read from the live
row on every request, and three handlers with no shared mux. See
`docs/ARCHITECTURE.md`, "Two surfaces in one binary, and what keeps them apart".

Its SMTP account is its own and not the sender's. Signing in must not depend on
how a customer's messages happen to be configured, and the mail does **not** go
through srosha's queue: a sign-in that depends on the service you are signing in
to fix is a trap.

`NOTIF_CONSOLE_SECURE_COOKIE` is off only for local development over plain http,
and the console refuses to start in production with it off.

`NOTIF_ADMIN_LIST_LIMIT` bounds every list the admin panel reads: the queue,
all sources, a source's message log, a source's own decision history, the
roster, and the global audit feed. One key for all of them, because it is one
concept -- how many rows one panel listing shows -- and separate keys per page
would be separate numbers that drift apart with nothing to notice. Default
`200`. Loading refuses a value at or below zero: a limit of zero would read as
a page with nothing on it, which is indistinguishable from a page that
genuinely has nothing to show. A listing that hits the cap says so on the
page, because a page that silently shows the newest N of a larger set is
telling an operator they are looking at everything when they are not.

### Password rule

Letters, digits and hyphens only. `#` is truncated by Dokploy's `.env` parsing;
`)` `!` `$` break in the shell; `#` `@` `:` `/` `?` need percent-encoding inside a
connection URL. Generate with `openssl rand -hex 24`.

---

## Pages and assets

Everything the console renders or serves lives at the repository root rather than
inside the Go tree, so changing a page or a stylesheet is not a hunt through
packages. It is still compiled into the binary: `public/embed.go` embeds it,
because `go:embed` cannot reach outside its own directory.

| | |
| --- | --- |
| Directory | `public/` |
| Served to a browser | `public/static/<surface>/` — `static/portal/`, `static/admin/` |
| Rendered on the server | `public/templates/<surface>/` — `templates/portal/`, `templates/admin/` |
| Served behind a guard | `public/guarded/<surface>/` — `guarded/admin/architecture.html` |
| Stylesheet | one per surface: `portal/portal.css`, `admin/admin.css` |
| Logo | `crane.svg`, copied into each surface's directory |
| Email bodies | `public/templates/email/` — rendered, never served, and not a surface |
| URL prefix | `/static/` — mapped to one surface's directory, never to `public/` |

**The three are not the same thing.** `static/` is fetched byte for byte;
`templates/` is rendered and never served. Serving `templates/` would hand out
the shape of every page and every field name in one request, so nothing may point
a file server at the root of that FS — `web.browserFiles(surface)` subs into
`static/<surface>`, so each surface sees its own assets and nothing else.

`guarded/` is the third, and it is what neither of the other two could hold: a
whole document, served byte for byte, that not everybody signed in may read.
Under `static/` it would be public to anybody who guessed the url; under
`templates/` it would be parsed as a template, which it is not.

**Nothing serves `guarded/` as a file system.** `web.guardedFile(surface, name)`
reads **one named file**, chosen where the surface is built and never by the
request, so the guard is on the route rather than on a directory whose contents
nobody listed. It is read at startup, so a missing file is a console that will
not start rather than a page that fails the first time somebody opens it.

Today it holds one file:

| | |
| --- | --- |
| File | `public/guarded/admin/architecture.html` |
| Route | `/architecture` on the admin surface — **`super_admin` only** |
| Source | `docs/assets/brand/srosha.architecture.json`, rendered by the `archify` skill |

`super_admin`, for the same kind of reason `/audit` is: the diagram names every
host, every port, every store and the private network they sit on. That is the
shape of the deployment, and an operator approving sources has no call to read
it.

Fonts are a stack, not files: nothing here may reach a font host at runtime, and
the portal has to work on a network that cannot. Vazirmatn is first in every
stack and takes over the moment its `woff2` is dropped in beside the stylesheet.

That applies to `guarded/` too, and it is not automatic there: the generated
diagram arrives with a Google Fonts `<link>` in its head, which is stripped
before the file is committed. `TestAnAdminIsRefusedOnTheArchitectureDiagram`
asserts the served bytes name no font host, so the strip cannot be forgotten the
next time the diagram is regenerated.

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
| Architecture diagram, interactive | `docs/assets/brand/srosha.architecture.json` — the source the admin surface's `/architecture` page is rendered from |
| Palette | `#3256AE` royal · `#33A5DE` sky · `#4934A2` violet · `#317DC6` azure · `#10182B` ink |
| Wordmark font | Onest (Google Fonts), weight 700 |

---

## Protobuf

| | |
| --- | --- |
| Tool | buf, `buf.yaml` and `buf.gen.yaml` at the repository root |
| Definitions | `api/proto/notification/v1/` |
| Proto package | `notification.v1` |
| Generated code | `gen/notification/v1/`, **committed** |
| Go import path | `github.com/Serajian/srosha/gen/notification/v1` |
| Plugins | `protoc-gen-go`, `protoc-gen-go-grpc` |
| Build target | `make proto` |

`gen/` is committed so a fresh clone builds with no protoc, no buf and no plugin
installed. It is generated output and never hand-edited; `make proto` rewrites
it and `buf lint` runs in the pre-commit hook.

The version is in the proto **package** and not only the directory, because the
package is what travels: two versions of a message with one fully qualified name
cannot share a wire.

---

## Migrations

| | |
| --- | --- |
| Tool | goose `v3.27.3`, as a library. Sequential numbering (`-s`) |
| Directory | `migrations/`, compiled into every binary by `go:embed` |
| Deployed as | the `migrate` service, which every other service waits for |
| Locally | `make migrate-up`, which uses the goose command line |
| Env keys | `NOTIF_MIGRATION_LOCK_TIMEOUT`; locally `GOOSE_*`, see below |
| When | a separate deployment step, never from an application entrypoint |

**Every deploy migrates, and every service waits for it.** `migrate` is an
ordinary service with `restart: "no"`, and the other three carry
`depends_on: migrate: condition: service_completed_successfully`. So a deploy is
build → migrate → start, and a failed migration stops the release: the running
containers are not replaced, and the previous version keeps serving.

It takes a **Postgres advisory lock**, which is why it is `cmd/migrate` and not
the goose command line — that has no lock flag. Two deploys at once queue rather
than both applying the same migration. `NOTIF_MIGRATION_LOCK_TIMEOUT` bounds the
wait, default `5m`, so a race ends as a failed release and not a hung one.

The sql is **embedded**, not copied into the image. Which migrations a build
carries is a fact about the build, and `migrations.Latest()` is what readiness
compares the database against.

`make setup-dev` installs the goose command line at `@latest` for local work.
Both read the same `goose_db_version` table.

| Command | Runs |
| --- | --- |
| `make migrate-up` | on a laptop, against the local port mapping |
| `make migrate-server` | on the server, from the image. A deploy already does this |
| `make migrate-server-status` | on the server. Changes nothing |

The numbering was rearranged when sources gained an owner: `users` is `00002` and
`sources` is `00003`, because `sources.owner_user_id` is a foreign key into a
table that has to exist first. Everything from `00004` shifted up by one. Nothing
was deployed, so the files were renumbered rather than the column arriving as an
ALTER.

| Group | Keys | gateway | dispatcher | console |
| --- | --- | --- | --- | --- |
| reconcile | `NOTIF_RECONCILE_AFTER`, `NOTIF_RECONCILE_GIVE_UP`, `NOTIF_RECONCILE_SCHEDULE`, `NOTIF_RECONCILE_BATCH`, `NOTIF_RECONCILE_LEASE` | — | ✅ | — |
| retention | `NOTIF_RETENTION_AGE` | ✅ | ✅ | — |
| retention sweep | `NOTIF_RETENTION_SCHEDULE`, `NOTIF_RETENTION_BATCH` | — | ✅ | — |

**There is no signing secret here, and there was until it moved into the
database.** It was a json map of source id to secret, which made adding a source
a redeploy, and gave the source no defined way of learning theirs at all.

It is issued now on `WebhookService.Register` — a call already authenticated as
the source that needs it — returned exactly once, and kept sealed with the same
keyring a sending credential uses. `RotateSecret` issues a new one, which is the
only way back for a source that lost theirs. Each source still has its own: with
one shared secret, any source holding it could forge a signed callback to
another.

`RECONCILE_AFTER` is how long a delivery may sit pending before the scheduler
picks it up; `RECONCILE_GIVE_UP` is the age past which its next attempt is the
last one. The scheduler runs in the dispatcher, because recovery sends rather
than republishes. See `docs/ARCHITECTURE.md`.

`RECONCILE_SCHEDULE` is a cron spec or an interval descriptor — `@every 5m`,
`*/5 * * * *`, `0 3 * * *` — read in UTC, so a schedule means the same moment
wherever it runs. One second is the finest interval.

`RECONCILE_LEASE` is how long a claimed delivery stays the dispatcher that took
it. It covers a dispatcher that died holding a row — a send that merely failed
gives the row back — so it is set from the slowest send there could be, and
loading refuses a value at or below `NOTIF_DISPATCH_ACK_WAIT`.

`RETENTION_AGE` is how long a message is kept; its deliveries go with it, by the
foreign key, so there is one number and not two that could disagree. The sweep
deletes by **age alone** — it does not check that the deliveries settled — which
holds only while a delivery gives up long before a message is dropped: it gives
up in minutes, so one still pending a month later is a row recovery never saw
rather than work waiting to happen. Loading refuses an age under 24×
`NOTIF_RECONCILE_GIVE_UP`, so that reasoning cannot quietly stop being true.

`RETENTION_AGE` is read by **both** binaries, and it is the only key in this
table split across two rows for that reason. The dispatcher deletes by it. The
gateway never deletes anything — it refuses a `List` whose window reaches
further back, because serving one would hand back a short page with nothing
saying it was short, and the caller could not tell "you sent nothing then" from
"we deleted it". Set them from one value; two that disagree means a listing
refused for data that is still there, or served for data that is gone.

---

## Local development

Local host-port mappings only. Production publishes nothing.

| | |
| --- | --- |
| `BASE_PORT` | `7000` (Makefile) |
| postgres | `127.0.0.1:7001` |
| nats | `127.0.0.1:7002` |
| gateway gRPC | `50051`, read from `NOTIF_GRPC_ADDR` in `.env` |
| console portal | `8090`, read from `NOTIF_PORTAL_ADDR`; `make run-console` |
| console admin | `127.0.0.1:8092`, read from `NOTIF_ADMIN_ADDR`; never `0.0.0.0` |
| env file | `.env`, git-ignored; template in `.env.example` |

The console needs an SMTP host to start, and a local catcher will not do as it
stands: the dialer requires STARTTLS and mailpit serves plain SMTP by default. To
try a sign-in locally, read the code out of `login_codes` instead.

---

## Build targets

`make help` lists every target. The ones that matter elsewhere:

| Target | Does |
| --- | --- |
| `make build` | all three binaries into `build/` |
| `make run-gateway` / `run-dispatcher` / `run-console` | one binary, locally |
| `make proto` | `buf generate` into `gen/` (committed) |
| `make arch-check` | fails if `core/domain` imports outside stdlib, `core/shared`, `pkg/errs` |
| `make precommit` | deps, proto lint, format, lint, arch-check |
| `make prepush` | the above plus race tests |
| `make migrate-*` | goose up / down / status / create / reset |
