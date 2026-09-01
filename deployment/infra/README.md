# The infrastructure this service runs on

**Nothing in this directory is deployed by anything.** No pipeline reads it, no
`docker compose up` in this repository brings it up, and a change here reaches
the server only when a person carries it there. Everything else under
`deployment/` is built and shipped automatically; this is not, and the
difference matters enough to say twice.

It is here because the alternative is worse. On 2026-09-01 the service moved to
a new host, and rebuilding these four files from memory took an afternoon —
including a long detour caused by a document that described the *previous*
host. A configuration nobody can read is a configuration nobody can check.

## What is here, and where it goes

| File | Applied by | Landing place |
| --- | --- | --- |
| `postgres/docker-compose.yml` | pasting into Dokploy | a Compose service named `srosha-postgres` |
| `nats/docker-compose.yml` | pasting into Dokploy | a Compose service named `srosha-nats` |
| `nats/nats-server.conf` | Dokploy → Advanced → Mounts → File Mount | mount path `nats-server.conf`, **not** `/etc/nats/...` |
| `nginx/srosha.conf` | copying to the server | `/etc/nginx/conf.d/srosha.conf`, then `nginx -t && systemctl reload nginx` |

Order matters once: the network, then postgres and nats, then the application.

```bash
docker network create srosha-net
```

`srosha-net` is `external: true` in every file here, which means "do not create
this, it already exists". Without it compose refuses to start and says so. It is
external because postgres, nats and the application are three separate Dokploy
services, and each would otherwise build a network of its own that the other two
cannot see — Dokploy offers no field for attaching a service to an existing one.

## No secrets, on purpose

Every value that would be a secret is a `${VAR}` or a `$VAR` reference resolved
from the environment at start. The passwords live in Dokploy's Environment tab
and nowhere else. Read `docs/CONFIG.md` for which keys exist; read nothing here
expecting a value.

Two of these fail loudly rather than quietly if a variable is missing, which is
deliberate and was tested:

- `nats-server` exits 1 with `variable reference for 'NATS_GATEWAY_PASSWORD'
  ... can not be found`. It does not start with an empty password.
- adminer's second port uses `${ADMINER_BIND_IP:?...}`, because an empty value
  there would expand to `:8083:8080` — a database login form on `0.0.0.0`.

## The mount path catches everyone once

Dokploy's File Mount path is relative to the files directory it manages, so it
is `nats-server.conf` and not the path inside the container. Get it wrong and
docker creates a **directory** where the file should be; the container exits 127
and writes nothing to its log. The build log saying `Detected: 1 mounts` is the
check.

## Where the rest of it is written down

`docs/CONFIG.md` holds the hostnames, ports and environment keys.
`docs/reference/srosha-infra-deploy.md` holds the topology and the traps.
