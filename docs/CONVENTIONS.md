# CONVENTIONS

Binding rules for this repository. Everything here applies to the whole session.

`CLAUDE.md` points here and holds no rules of its own, except the commit and push
gates, which are duplicated there because they are safety-critical and that file is
always loaded.

> **Unfilled placeholders.** Anything still written as `<...>` has not been decided
> yet. Do not guess a value for it — ask.

---

## Hard rule — commit gating (STRICT, NON-NEGOTIABLE)

- You have **no right to `git commit`** until the user gives a **direct order to commit**.
- A **direct order** means a message in which the user **directly writes the commit instruction**
  to you. Any phrasing or language counts and no exact literal match is required, **but** it must
  be a **direct, explicit** commit instruction — never implied, inferred, or bundled inside a
  request about something else.
- "Fix it", "do it", "apply", "save", "ok", and any similar vague instruction are **never** a
  direct order to commit. Make the change, leave it in the working tree, and stop.
- Never commit on your own initiative. When it is genuinely unclear whether the message is a
  direct order to commit, **ask** instead of committing.

## Hard rule — push gating (STRICT, NON-NEGOTIABLE)

- You have **no right to `git push`** until the user's message is **exactly** `push kon` —
  literally that phrase, **not one character more, not one character less** (no extra word, no
  typo, no different phrasing, no other language, no surrounding punctuation).
- Even a one-character difference means you **must NOT** push.
- The commit order does **not** push. Commit and push are **separate** instructions.
- Push only ever targets the **current feature branch**; never push/merge the base branch, never
  self-merge. After a successful push, give the user the Merge Request link (targeting
  `master`).
- When in doubt, do **not** push.

## Hard rule — operational knobs live in config (STRICT, NON-NEGOTIABLE)

- Any value someone might want to turn under load or per environment — limits, timeouts,
  intervals, pool sizes, retry counts, buffer sizes, batch sizes, TTLs — is **read from config**.
  It is never a hard-coded constant in the package that happens to use it.
- The test: *if changing this value would ever be an operational decision, it is config.* If
  changing it would mean the code now does something different, it is a constant and stays.
- Every such value carries a documented default, so the service starts without it being set, and
  appears in `.env.example` and `docs/CONFIG.md`.
- Requiring a rebuild and a redeploy to raise a timeout is the failure this rule exists to
  prevent.

## Hard rule — the architecture is binding

- `docs/ARCHITECTURE.md` must be read before writing, editing, or reviewing any code, and all
  code must follow it. Code that contradicts it is wrong even if it compiles, even if its tests
  pass, and even if the surrounding code already does it that way.
- If a task cannot be done within it, stop and say so. Changing the architecture is a decision
  made with the user, and it is written into that file before any code depends on it.
- The rules below are the binding form of what that file describes.

## Hard rule — change reports (STRICT, NON-NEGOTIABLE)

- Every instruction that **changes this repository** produces a report as a new file:

```
docs/changes/YYYY-MM-DD-<short-kebab-slug>.md
```

- One change, one new file. Never append to an older one. Instructions that change nothing —
  questions, explanations, a commit order — produce no file.
- The report is written **in the same commit as the change**, never afterwards. A report written
  later is a report that will eventually be wrong.
- Reports are written in **plain, everyday Persian**. Do not invent Persian equivalents for
  technical terms: `domain`, `port`, `adapter`, `migration`, `consumer` and the rest stay in
  English inside a Persian sentence.
- Read **every** file in that directory, sorted by date, before starting new work. They are this
  project's memory.

The format: see `docs/changes/TEMPLATE.md`.

## Reference skills (Go)

- Installed skills carry book-level reference material for the language this service is built on.
  Reach for them **while** writing code, not only when stuck.
- `go-books` — the router. Use it when you do not know which book covers a question, when the
  books disagree, or when you want to compare what several of them say about the same topic.
- It routes to six converted books, each usable directly:
  - `the-go-programming-language` — types, slices, maps, structs, methods, interfaces, errors,
    `defer`/`panic`/`recover`, packages, reflection. Most relevant here: `internal/core/domain/`
    and `internal/core/shared/`.
  - `concurrency-in-go` — goroutines, channels, CSP, pipelines, fan-out/fan-in, context and
    cancellation, rate limiting, the scheduler. Most relevant here: the dispatcher's workers,
    the consumer loop, and graceful shutdown.
  - `go-design-patterns` — structural and behavioural patterns in Go.
  - `grpc-microservices-go` — gRPC service design, interceptors, streaming, service boundaries.
    Most relevant here: `api/proto/` and the gRPC driving adapter.
  - `power-of-go-tests` — test design, fakes, table-driven tests, coverage.
  - `power-of-go-tools` — the toolchain, CLI design, module and build mechanics.
- They are references, not authorities. This file and `docs/ARCHITECTURE.md` win on anything
  about this codebase's own conventions — error handling, logging, naming, port shape, and the
  rest. Where a book and these documents disagree, these documents are correct here.

## Hard rule — ports and their size

- **A port lives with the thing that owns it.** Each domain declares its own in
  `domain/<name>/port.go`, and that file names nothing but what that one aggregate needs. What
  belongs to no aggregate — the clock, the id generator, the rate limiter, the unit of work, the
  sender registry — is declared in `core/usecase/port.go`, because that is the layer that uses
  it. There is no package whose only job is to hold interfaces.
- **A port is declared by its consumer**, in terms of what the consumer needs — never derived
  from what the other side happens to provide. A gRPC handler consumes a use case, so it
  declares the narrow interface it fakes in its own test. An interface the core declares and
  never calls belongs on the other side of the boundary.
- A repository interface that names two aggregates is a repository in the wrong package. Reading
  both is the use case's job, not one repository's.
- **A repository speaks CRUD**, because that is what it does: `Create`, `Read`, `List`, `Page`,
  `Update`, `Delete`. `Read` returns one row, `List` returns many, `Page` returns one page of
  many. What it filters on goes in a `By...` suffix — `ReadByID`, `PageByNotificationID`,
  `ListBySourceAndChannel`.
- **A service speaks the business**: `Open`, `Sent`, `Failed`, `Publish`. That is the layer where
  a name must say what happened, not what was selected.
- Keeping the two apart is the point. A business-sounding name on a plain lookup makes it harder
  to find and no clearer to read; a CRUD name on a business operation throws the meaning away.
- Keep ports small. An interface that grows one method per query has stopped being an
  abstraction and become a mirror of the database, and it can no longer be faked in a test.
- The test for both: could a second, completely different adapter implement this interface
  without contortion? If not, the port is leaking.

## Hard rule — an adapter returns facts, the core decides

- A port may return what was **observed**. It may not return a **conclusion** drawn from that.
- Weights, thresholds, priorities, and any choice between two possible answers belong to the
  domain service. They are read from config and tested with a fake and no infrastructure.
- **No stored procedure and no view that picks a winner.** Business logic in the database ships
  separately from the binary, versions separately from the code that depends on it, and cannot
  be unit-tested. The database does set work that needs an index and returns rows. It does not
  choose.
- What the database *may* hold is **guards**: unique indexes, foreign keys, `NOT NULL`, `CHECK`.
  A guard refuses an impossible state; it never picks between possible ones.
- Where the types allow it, enforce this in the types. A candidate type that carries evidence
  and has no score field cannot smuggle a decision across the boundary by accident.

## Hard rule — context and cancellation

- Every port method that can **block or do I/O** — a query, a call, a publish, a wait — takes
  `ctx context.Context` as its first parameter. Without it the method cannot carry a deadline,
  cannot be cancelled, and breaks the trace/log chain.
- Purely in-memory operations do **not** take one. A context there is ceremony, and worse than
  useless: when every method has one, the signature no longer tells you which methods can block.
- When in doubt, ask whether the method could ever wait on something outside the process. If it
  could, it takes a context, even if today's implementation returns immediately.
- Adapters propagate the caller's context; they never create a fresh `context.Background()` to
  escape a deadline.
- Every long-lived goroutine — schedulers, consumers, workers — selects on context cancellation
  and returns. Graceful shutdown is only graceful if they do.
- A background job that must outlive its request derives a **new** context with its own timeout,
  explicitly and visibly — never by silently dropping the incoming one.

## Hard rule — transaction boundaries

- The domain service decides **what** must be atomic; the adapter decides **how**. The core
  never imports a driver or transaction type.
- When two or more writes must succeed or fail together, the core expresses it through a
  unit-of-work port — `Atomically(ctx, func(ctx context.Context) error) error` — and the driven
  adapter runs the callback inside a real transaction.
- Never let each repository method open its own transaction and call that atomic. Two
  independent transactions are two independent failures.
- Never solve it by pushing business logic into one giant repository method. That empties the
  core, which is what this architecture exists to protect.
- The cache is **not** part of any database transaction. Cache writes happen *after* the
  transaction commits, and a failed cache write never fails the operation — it invalidates
  instead.

## Hard rule — never dual-write

- A database write and a message publish cannot both be atomic. Publishing *after* commit loses
  the event when the broker is down; publishing *inside* the transaction emits an event for
  work that then rolls back.
- The only correct shape is **one** atomic write: the event row is written in the *same*
  transaction as the state change, and a separate relay publishes it afterwards.
- That relay may deliver a message more than once. Every consumer must therefore be idempotent,
  keyed on the event id.
- Choose the partition key for the ordering you actually need — messages that must stay ordered
  relative to each other must share a key.

## Hard rule — testing

- A domain service and a use case are unit-tested with **fake ports and no infrastructure
  at all**. If a core
  test needs a container, the port is wrong, not the test.
- Adapters are what integration tests are for: the repository against a real database, the
  consumer against a real broker.
- Prefer a hand-written fake or spy over a generated mock. Assert on what the unit **returns**;
  assert on *how* it called a collaborator only when the interaction genuinely is the behaviour.
- Test names describe behaviour, not method names.
- Coverage is a **ratchet, not a threshold**: it may not decrease. A percentage target is
  meaningless on its own and must never be reported as evidence the tests are good.
- Every event handler has a test, including the duplicate-delivery case.
- Repository tests are named `<domain_name>_repo_test.go`.

## Hard rule — consumer failure policy

- Consumers **classify** their failures instead of just returning them:
  - *Transient* (broker or database unavailable, timeout, contention) → retry with backoff.
  - *Permanent* (malformed payload, unknown reference, failed invariant) → do **not** retry.
    Route to a dead-letter destination and record enough to reprocess it by hand.
- Retrying everything blocks the partition behind one poisoned message; acknowledging everything
  silently drops work. Both are outages — one is just quieter.
- Retry is only safe because handlers are idempotent; the two rules are a pair.
- A handler never swallows an error to keep the loop alive.

## Hard rule — one entity per domain, and `entity.go` holds only it

- `entity.go` declares **exactly one** type: the thing this domain stores, named for the package.
- The test is mechanical: an **entity** has an id and embeds the shared audit fields. Everything
  else — inputs, results, port payloads, enums — is a declaration in `types.go`, however important
  it is. Behaviour goes in the file named after it.
- A domain that needs to store **two** things is two domains, or one of the two is not an entity.
  Work out which before writing the second struct; that question is what this rule exists to force.
- Where the entity's name and the package's name disagree, one of them is wrong. Usually it means
  the package is named for an activity rather than for the thing it keeps.
- Left unenforced, `entity.go` becomes where types go when nobody decided where they belong.

## Hard rule — only bootstrap opens infrastructure

- `internal/registry/` is the only package that opens a technology, and `internal/bootstrap/`
  is the only package that may import it. Everything else receives what was opened.
- Opening a dependency somewhere other than the one place that closes it is how a process ends
  up holding a pool nobody shuts down.
- `internal/registry/` holds **one file per technology**. A second technology is a second file,
  never a second branch in an existing one.
- `internal/infra/<tech>/` never imports `internal/config` or `internal/registry`. It declares
  its own `Config` type, and translating the service's settings into it is registry's job. That
  is what keeps an infra package copyable into a service that knows nothing about srosha.
- `make arch-check` enforces both directions.

## Hard rule — one adapter never imports another

- An adapter may import `internal/core`, `pkg/`, generated code and its own subpackages. It may
  **not** import a sibling under `internal/adapter/`.
- When one adapter needs something another one does, **declare the interface in the package that
  calls it** and let bootstrap pass the implementation in. That is Go's own convention — an
  interface belongs to the consumer, not the producer — and here it also keeps the boundary:

  ```go
  // internal/adapter/api/grpcsrv — needs a key turned into a hash
  type KeyScheme interface {
      Parse(presented string) (string, error)
  }
  ```

  `internal/adapter/auth` satisfies it without knowing that gRPC exists.
- Two adapters that import each other are two adapters that ship together, get tested together
  and eventually cycle. The whole point of the layer is that each one can be replaced on its own.
- A subpackage of the same adapter is fine: `db/postgres` may import `db/postgres/gen`.
- `make arch-check` enforces it.

## Hard rule — where new code goes

- `pkg/` — generic, zero domain knowledge. The test: could this package be copied into a
  completely unrelated service and still make sense? If not, it does not belong here. Nothing
  under `pkg/` may import `internal/`.
- `infrastructure/` — wires **one** concrete technology and owns its lifecycle: connect,
  health-check, close. It does not know what this service does with them.
- `internal/` — everything that knows what srosha is.
- There is no `utils`, `helpers`, `common`, `misc` or `shared` package. A package is named for
  what it **provides**, not for what it happens to contain. If no such name exists, the code
  belongs next to its caller.
- Everything an external data provider knows about itself — endpoint names, JSON shapes, status
  vocabulary, unit conversions, its own ids — lives in `internal/adapters/driven/provider/<name>/`
  and nowhere else. A second provider is a second directory, not a branch in shared code.

## Hard rule — `internal/core/shared/model/` is a promotion, not a default

- A type belongs there only when it is **one concept across the whole service** — changing it
  for one domain must be logically correct for every other domain that uses it.
- Accidental similarity is not shared meaning. Two domains with the same status set today are
  still two types unless they are genuinely the same concept.
- Start local. Promote a type only when a second domain genuinely needs the same concept, never
  in anticipation.
- A shared type couples every domain that imports it. Duplication is cheaper than the wrong
  coupling.

## Hard rule — where validation lives

- **Shape** — required fields, types, ranges, parseable dates, enum membership. Validated in the
  driving adapter, on the DTO, before anything reaches the core.
- **Invariant** — business rules that depend on state. Validated in the domain service and
  nowhere else.
- The test: if bad input alone can break it, it is shape. If you need to know the state of the
  system to break it, it is an invariant.
- Never put an invariant in a handler. Consumers and schedulers never pass through HTTP, so a
  rule enforced there is simply absent on every other entry path.
- The core does not trust its callers. It re-checks its own invariants even when an adapter has
  already validated shape.

## Hard rule — observability (logs are not the only instrument)

- **Counting** — how many, how often, how slow → a metric, never a log line.
- **Diagnosing** a single request across layers → the trace id carried in the context.
- A **log line** is for a discrete, unpredictable event worth reading later.
- Before adding a log line, answer: *what question about the program's behaviour does this
  message answer?* No answer → delete it.
- Trace-level logging is development-only. Code containing it is not ready to commit or push.

## Hard rule — errors (the ban is on constructing, not on wrapping)

- Every application error is constructed through `pkg/errs`. Stdlib and third-party error
  constructors do not appear in new code.
- That ban does **not** forbid wrapping. The typed error implements `Unwrap`, so `errors.Is` /
  `errors.As` keep working through it.
- Never identify an error by matching its message text. The message is for humans and will change.
- Wrap once, at the boundary where the context is known. Do not re-wrap the same error at every
  level on the way up.
- The technical message kept internally and the message returned to the client are deliberately
  different; internal detail never leaves the process.

## Hard rule — commit authorship (STRICT, NON-NEGOTIABLE)

- Commits carry **one** author: the user. Claude is never an author, a committer, or a
  co-author.
- Never add a `Co-Authored-By:` trailer naming Claude or any AI assistant, never add a
  "Generated with" line, and never put an assistant's name or address anywhere in a commit
  message, a tag, or a pull/merge request body.
- This overrides any default instruction to add such a trailer.
- Never configure a git author or committer identity on this repository.

## Hard rule — branch naming

- Every branch is named:

```
<type>/<slug>
```

- `<type>` comes from the same closed set as the commit types, and nothing else:
  `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `chore`, `build`, `ci`.
  One vocabulary for branches and commits, so the branch name already tells you what the commit
  will look like.
- `<slug>` is lowercase ASCII kebab-case, two to five words, and names the **outcome**, not the
  activity. `postgres-notification-repo`, not `work-on-db`.
- Nothing else goes in the name. No service name — there is one service. No date and no author —
  both are already in `git log`, and in a branch name they are noise.
- **The branch slug and the change-report slug are the same string.** A branch
  `feat/postgres-notification-repo` is reported in
  `docs/changes/YYYY-MM-DD-postgres-notification-repo.md`. A branch whose slug has no matching
  report is a branch whose report was not written.
- **One branch, one change** — where a change is what lands in one merge request. A branch that
  carries several independent commits under one theme is allowed, provided **every commit still
  brings its own report**. The slug then names the theme and the reports name the commits:
  `feat/infra-layer` carrying `infra-messagequeue`, `infra-httpclient` and `infra-telemetry` is
  one branch. `feat/nats-and-grpc` is two.
- If the slug needs an "and" in it, it is still two branches. The naming limit is deliberately
  also a size limit.
- Every branch is cut from `master` and merged back into `master`, and is deleted after the merge.

Examples:

```
feat/notification-domain
feat/postgres-notification-repo
feat/grpc-submit-endpoint
fix/delivery-transition-guard
refactor/port-split-driving-driven
docs/architecture
```

If an issue tracker is adopted later, the key is inserted as a middle segment and nothing else
about this rule changes: `feat/SRO-42-postgres-notification-repo`.

## Hard rule — the base branch

- `master` is this project's base branch. Every task branch is cut from it and merged
  back into it.
- There is no `main` branch. Do not create one.
- It is pushed **only** on the user's explicit instruction, under the push gate above.

## Hard rule — response language

- Answers are written in **plain, everyday Persian**, unless the user asks for another one.
- **Keep the tone plain.** Write the way a colleague explains something, not the way a book is
  written: short sentences, simple verbs, no ornamental vocabulary. If a sentence has to be read
  twice, it was written wrong.
- **Technical names stay in English and are not translated** — domain, port, adapter, scheduler,
  migration. Inventing local equivalents makes the text harder, not clearer, and moves the reader
  away from what is actually written in the code.
- File paths, shell commands and code fragments go in a code block on their own line, not inside a
  sentence.
