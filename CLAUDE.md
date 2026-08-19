# CLAUDE.md

This file only points. Every rule lives in `docs/CONVENTIONS.md`.

**Hard rule — read the conventions first (STRICT, NON-NEGOTIABLE):**
- Before doing **anything** in this repository — answering, exploring, editing, running commands —
  you **must** read this file first:

```
docs/CONVENTIONS.md
```

- Every rule in that file is binding and applies to the whole session.

**Hard rule — read the architecture before writing code (STRICT, NON-NEGOTIABLE):**
- Before writing, editing, or reviewing **any** Go code in this repository, you **must**
  read this file first:

```
docs/ARCHITECTURE.md
```

- All code **must** follow it. Code that contradicts it is wrong, even if it compiles, even if
  it passes its tests, and even if the surrounding code already does it that way.
- If a task genuinely cannot be done within it, **stop and say so** — do not deviate silently.
  Changing the architecture is its own decision, made with the user, and the change is written
  into that file before any code depends on it.

**Hard rule — repository data lives in one file (STRICT, NON-NEGOTIABLE):**
- `docs/CONFIG.md` holds every piece of repository **data**: addresses, hosts, ports, paths,
  stage names, registries, image names, branch names, environment keys, build targets.
- Whenever you need one of those, **read it from that file** instead of grepping the tree or
  guessing — and whenever you learn a new one, **add it there** in the same commit.
- **Never write a secret into it.** No passwords, tokens, keys, or connection strings containing
  credentials. Those belong in `.env` (git-ignored) and the deployment secrets. That file records
  names and locations, not values.

**Hard rule — change reports (STRICT, NON-NEGOTIABLE):**
- Every instruction that changes this repository produces a new report under `docs/changes/`,
  written **in the same commit as the change**.
- Read every file in that directory before starting new work.
- Format and full rules in `docs/CONVENTIONS.md`.

**Hard rule — commit gating (STRICT, NON-NEGOTIABLE):**
- You have **no right to `git commit`** until the user gives a **direct order to commit**.
- A **direct order** means a message in which the user **directly writes the commit instruction**
  to you. Any phrasing or language counts and no exact literal match is required, **but** it must
  be a **direct, explicit** commit instruction — never implied, inferred, or bundled inside a
  request about something else.
- "Fix it", "do it", "apply", "save", "ok", and any similar vague instruction are **never** a
  direct order to commit. Make the change, leave it in the working tree, and stop.
- Never commit on your own initiative. When it is genuinely unclear whether the message is a
  direct order to commit, **ask** instead of committing.

**Hard rule — push gating (STRICT, NON-NEGOTIABLE):**
- You have **no right to `git push`** until the user's message is **exactly** `push kon` —
  literally that phrase, **not one character more, not one character less** (no extra word, no
  typo, no different phrasing, no other language, no surrounding punctuation).
- Even a one-character difference means you **must NOT** push.
- The commit order does **not** push. Commit and push are **separate** instructions.
- Push only ever targets the **current feature branch**; never push/merge the base branch, never
  self-merge. After a successful push, give the user the Merge Request link (targeting
  `master`).
- When in doubt, do **not** push.

**Reference skills (Go):**
- Installed skills carry book-level reference material for the language this service is built on.
  The entry point is `go-books`, which routes to the six converted Go books. Reach for them
  **while** writing code, not only when stuck.
- They are references, not authorities. `docs/CONVENTIONS.md` and `docs/ARCHITECTURE.md` win on
  anything about this codebase's own conventions.
- Full description in `docs/CONVENTIONS.md`.
- Any new rule, standard, or convention — anything that has the shape or nature of a convention —
  **must** be added to that file, never to this one. This file only points to it.
