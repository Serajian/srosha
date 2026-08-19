# docs/reference

Background material, kept verbatim and never edited to track the code.
Neither file here is a plan to execute.

## srosha-spec-draft.md — opinions

An architecture/design proposal for Srosha produced in a chat session with Claude.
It is **a set of opinions, not a plan of record**.

- Decisions are made incrementally by the repository owner; agreement with any
  particular recommendation in it should not be assumed.
- The code is the source of truth where the two disagree.

Useful as context for how the system is meant to hang together (layering, error
model, messaging shape, open questions), not as a checklist.

## srosha-infra-deploy.md — facts

Companion to the spec, describing **where the service runs and how it gets there**:
the Dokploy host, the private docker network, the already-deployed Postgres and
NATS, ports, environment variables, and the deployment traps already paid for.

This one is different in kind. Its §2 records infrastructure that is **already
deployed and verified**, so those parts are **observed fact**, not proposal —
treat them as authoritative about the running system and do not recreate what
they describe. Its recommendations about code we have not written yet (Dockerfile
shape, health-check subcommands, migration policy) are still proposals.

Where either document disagrees with a decision the owner has made in
`docs/CONVENTIONS.md`, `docs/ARCHITECTURE.md` or `docs/CONFIG.md`, those files win.
