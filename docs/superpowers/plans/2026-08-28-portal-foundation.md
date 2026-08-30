# Customer portal — foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A third binary a person can sign in to with a one-time code, where
every change that will ever be made is already recorded.

**Architecture:** A `portal` binary beside gateway and dispatcher, talking to
postgres directly through the same core. One `users` table for customers and
operators alike. No passwords — a code sent by email, over SMTP, sent by this
binary rather than through srosha's own queue. Sessions live server-side so
deactivating somebody logs them out at once. Every mutating action passes
through one gate that writes an audit row, so roles and approvals later are one
file rather than fifty call sites.

**Tech Stack:** Go 1.26, pgx + sqlc, `html/template`, `internal/infra/smtp`,
`internal/infra/httpserver`, goose migrations.

**Spec:** `docs/superpowers/specs/2026-08-28-customer-portal-design.md`

## Global Constraints

- **`docs/CONVENTIONS.md` and `docs/ARCHITECTURE.md` are binding.** Read both
  before the first edit. Code that contradicts them is wrong even if it passes.
- **Constants live in `const.go`**, one per package. Never inline a limit.
- **Comments are few and short.** Most declarations need none.
- **One adapter never imports another.** Declare the interface in the package
  that calls it; bootstrap passes the implementation in.
- **`internal/registry` is the only package that opens a technology.**
- **Every error is built through `pkg/errs`.** Only `message` crosses the wire;
  `reason` goes to the log.
- **Every id is a ULID** — 26 characters of Crockford base32, which excludes
  I, L, O and U. The `ulid` domain refuses anything else.
- **Nothing is deployed**, so schema changes fold into the migration that
  created the table rather than arriving as an ALTER.
- **`make prepush` must be green before every commit.**
- **Never `git commit` or `git push`** — the owner gives those orders.

### Scope

This plan ends when a person can sign in, a deactivated one cannot, and the
audit log has rows in it. **Registering sources, keys, credentials and callbacks
is a second plan** — every one of those is an existing use case that only needs
a page, and they all depend on everything here.

---

### Task 1: The users table

One table for customers and operators. Customers create themselves; `role` is
what `super_admin` controls.

**Files:**
- Create: `migrations/00008_create_users.sql`
- Create: `internal/core/domain/user/entity.go`
- Create: `internal/core/domain/user/errors.go`
- Create: `internal/core/domain/user/const.go`
- Test: `internal/core/domain/user/entity_test.go`

**Interfaces:**
- Consumes: `shared.ID`, `shared.NowFunc`, `pkg/errs`
- Produces: `user.User{ID shared.ID, Email string, Role Role, IsActive bool, CreatedAt, UpdatedAt time.Time}`,
  `user.New(id shared.ID, email string, role Role, now time.Time) (*User, error)`,
  `user.Role` with `RoleCustomer`, `RoleAdmin`, `RoleSuperAdmin`,
  `(*User).EnsureActive() error`, `(Role).IsOperator() bool`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- +goose StatementBegin

-- A person. Customers and operators are the same row with a different role:
-- two account tables would mean two sign-in flows and two sets of bugs.
--
-- There is no password column and never will be. Sign-in is a one-time code,
-- which is also what lets the first operator be written by hand -- an argon2
-- hash cannot be typed into SQL, and an email can.
CREATE TABLE users (
    id         ulid        PRIMARY KEY,

    -- Lowercased before it is stored, so two spellings of one address are one
    -- account.
    email      TEXT        NOT NULL UNIQUE,

    role       TEXT        NOT NULL
                           CHECK (role IN ('customer', 'admin', 'super_admin')),

    -- Whether this person may SIGN IN. It says nothing about whether their
    -- sources may send: those are opposite questions -- a customer who has not
    -- paid must still be able to sign in, or they cannot pay.
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing test**

```go
package user_test

import (
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
)

var (
	id  = shared.ID("01K0ACCT0000000000000000AB")
	now = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
)

func TestANewUserIsACustomerWhoCanSignIn(t *testing.T) {
	u, err := user.New(id, "Ops@Acme.Test", user.RoleCustomer, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if u.Role != user.RoleCustomer {
		t.Errorf("role = %q", u.Role)
	}
	if !u.IsActive {
		t.Error("a new user cannot sign in")
	}
	if err := u.EnsureActive(); err != nil {
		t.Errorf("EnsureActive: %v", err)
	}
}

// Two spellings of one address are one account, or somebody signs up twice and
// wonders where their sources went.
func TestTheEmailIsLowercased(t *testing.T) {
	u, err := user.New(id, "  Ops@Acme.Test  ", user.RoleCustomer, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if u.Email != "ops@acme.test" {
		t.Errorf("email = %q, want it lowercased and trimmed", u.Email)
	}
}

func TestWhatIsNotAUser(t *testing.T) {
	cases := map[string]struct {
		email string
		role  user.Role
	}{
		"no email":       {"", user.RoleCustomer},
		"not an address": {"not-an-address", user.RoleCustomer},
		"unknown role":   {"a@b.test", user.Role("root")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := user.New(id, c.email, c.role, now); err == nil {
				t.Fatal("New: want an error")
			}
		})
	}
}

// A deactivated person is refused where they try to sign in, not where their
// sources try to send.
func TestADeactivatedUserCannotSignIn(t *testing.T) {
	u, _ := user.New(id, "a@b.test", user.RoleCustomer, now)
	u.IsActive = false

	if err := u.EnsureActive(); err == nil {
		t.Fatal("EnsureActive: want an error")
	}
}

// Only an operator may ever be given powers a customer does not have, so the
// two are told apart in one place rather than at every check.
func TestWhoIsAnOperator(t *testing.T) {
	cases := map[user.Role]bool{
		user.RoleCustomer:   false,
		user.RoleAdmin:      true,
		user.RoleSuperAdmin: true,
	}
	for role, want := range cases {
		if got := role.IsOperator(); got != want {
			t.Errorf("%q.IsOperator() = %t, want %t", role, got, want)
		}
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/domain/user/`
Expected: FAIL — the package does not exist.

- [ ] **Step 4: Write the entity**

`internal/core/domain/user/const.go`:

```go
package user

// maxEmailLen is a bound of our own. RFC 5321 allows 254 for the whole
// address, and anything near it is a paste that went wrong rather than a
// mailbox.
const maxEmailLen = 254
```

`internal/core/domain/user/errors.go`:

```go
package user

import "errors"

var (
	ErrNotFound     = errors.New("user not found")
	ErrEmptyEmail   = errors.New("email is required")
	ErrInvalidEmail = errors.New("email is not an address")
	ErrUnknownRole  = errors.New("unknown role")

	// ErrInactive is refused where somebody tries to SIGN IN. A source that may
	// not send is a different question with a different answer.
	ErrInactive = errors.New("user cannot sign in")
)
```

`internal/core/domain/user/entity.go`:

```go
// Package user is a person: somebody who signs in. Customers and operators are
// the same kind of row with a different role, which is what keeps one sign-in
// flow instead of two.
package user

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Role is what this person may do. It is the one field a customer cannot set:
// they create themselves as customers, and only a super_admin changes it.
type Role string

const (
	RoleCustomer   Role = "customer"
	RoleAdmin      Role = "admin"
	RoleSuperAdmin Role = "super_admin"
)

func (r Role) Valid() bool {
	switch r {
	case RoleCustomer, RoleAdmin, RoleSuperAdmin:
		return true
	default:
		return false
	}
}

// IsOperator reports whether this role belongs to us rather than to a customer.
// Told apart in one place, so a later rule is one edit.
func (r Role) IsOperator() bool {
	return r == RoleAdmin || r == RoleSuperAdmin
}

func (r Role) String() string { return string(r) }

// User is somebody who signs in.
type User struct {
	ID    shared.ID
	Email string
	Role  Role

	// IsActive is whether they may SIGN IN, and nothing else. Whether their
	// sources may send is sources.is_active, and the two are wanted in opposite
	// combinations often enough that neither cascades into the other.
	IsActive bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// New builds a person, lowercasing the address so that two spellings of one
// mailbox are one account.
func New(id shared.ID, email string, role Role, now time.Time) (*User, error) {
	address, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if !role.Valid() {
		return nil, errs.InvalidInputErr("unknown role").
			WithErr(ErrUnknownRole).
			WithStr(fmt.Sprintf("got %q", role))
	}

	return &User{
		ID:        id,
		Email:     address,
		Role:      role,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// EnsureActive refuses somebody who may not sign in.
func (u *User) EnsureActive() error {
	if u.IsActive {
		return nil
	}
	return errs.ForbiddenErr("this account cannot sign in").
		WithErr(ErrInactive).
		WithStr(fmt.Sprintf("user %q", u.ID))
}

// NormalizeEmail is how an address becomes the thing stored and looked up. It
// is exported because sign-in has to normalize what somebody typed the same way
// before it can find their row.
func NormalizeEmail(email string) (string, error) { return normalizeEmail(email) }

func normalizeEmail(email string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(email))
	if t == "" {
		return "", errs.InvalidInputErr("email is required").WithErr(ErrEmptyEmail)
	}
	if len(t) > maxEmailLen {
		return "", errs.InvalidInputErr("email is too long").
			WithErr(ErrInvalidEmail).
			WithStr(fmt.Sprintf("%d chars, max %d", len(t), maxEmailLen))
	}
	if _, err := mail.ParseAddress(t); err != nil {
		return "", errs.InvalidInputErr("email is not an address").WithErr(ErrInvalidEmail)
	}
	return t, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/domain/user/ -v`
Expected: PASS, five tests.

- [ ] **Step 6: Apply the migration and check it holds**

```bash
make dev-reset && sleep 3 && make migrate-up
docker exec srosha-postgres-dev psql -U srosha -d srosha -c \
  "INSERT INTO users (id,email,role,is_active,created_at,updated_at)
   VALUES ('01K0ACCT0000000000000000AB','a@b.test','root',true,now(),now());"
```

Expected: refused by the role CHECK.

- [ ] **Step 7: `make prepush`, then stop**

Run: `make prepush`
Expected: green. **Do not commit** — the owner gives that order.

---

### Task 2: Storing and reading users

**Files:**
- Create: `internal/core/domain/user/port.go`
- Create: `internal/adapter/db/postgres/queries/user.sql`
- Create: `internal/adapter/db/postgres/user.go`
- Test: `internal/adapter/db/postgres/user_test.go` *(build tag `integration`)*

**Interfaces:**
- Consumes: `user.User` from Task 1
- Produces: `user.Repository` with
  `Create(ctx, *User) error`,
  `ReadByEmail(ctx, email string) (*User, error)`,
  `ReadByID(ctx, id shared.ID) (*User, error)`;
  `postgres.NewUserRepository(pool *pgxpool.Pool) *UserRepository`

- [ ] **Step 1: Write the port**

```go
package user

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Repository is where people are kept.
//
// ReadByEmail answers ErrNotFound rather than nil for an address nobody has
// used: sign-in has to tell "no such person" from "a person we could not read",
// and a nil with no error makes those the same.
type Repository interface {
	Create(ctx context.Context, u *User) error
	ReadByEmail(ctx context.Context, email string) (*User, error)
	ReadByID(ctx context.Context, id shared.ID) (*User, error)
}
```

- [ ] **Step 2: Write the queries**

`internal/adapter/db/postgres/queries/user.sql`:

```sql
-- name: CreateUser :exec
INSERT INTO users (id, email, role, is_active, created_at, updated_at)
VALUES (@id, @email, @role, @is_active, @created_at, @updated_at);

-- ReadUserByEmail is the sign-in lookup. The address is already lowercased by
-- the caller, so this matches exactly and uses the unique index.
-- name: ReadUserByEmail :one
SELECT id, email, role, is_active, created_at, updated_at
FROM users
WHERE email = @email;

-- name: ReadUserByID :one
SELECT id, email, role, is_active, created_at, updated_at
FROM users
WHERE id = @id;
```

- [ ] **Step 3: Generate and watch it fail to compile**

Run: `make sqlc && go build ./internal/adapter/db/postgres/`
Expected: sqlc succeeds; the repository file does not exist yet.

- [ ] **Step 4: Write the failing test**

```go
//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
)

func aUser(t *testing.T, id, email string) *user.User {
	t.Helper()

	u, err := user.New(shared.ID(id), email, user.RoleCustomer, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

func TestAUserComesBackByEmailAndByID(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	u := aUser(t, ulid("USR"), "ops@acme.test")

	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byEmail, err := repo.ReadByEmail(ctx, "ops@acme.test")
	if err != nil {
		t.Fatalf("ReadByEmail: %v", err)
	}
	if byEmail.ID != u.ID || byEmail.Role != user.RoleCustomer || !byEmail.IsActive {
		t.Errorf("read back %+v", byEmail)
	}

	byID, err := repo.ReadByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadByID: %v", err)
	}
	if byID.Email != "ops@acme.test" {
		t.Errorf("email = %q", byID.Email)
	}
}

// Sign-in has to tell "nobody has this address" from "we could not read the
// row", and a nil with no error makes those the same answer.
func TestAnAddressNobodyHasUsed(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	_, err := postgres.NewUserRepository(pool).ReadByEmail(context.Background(), "nobody@acme.test")
	if !errors.Is(err, user.ErrNotFound) {
		t.Errorf("ReadByEmail = %v, want ErrNotFound", err)
	}
}

// One address is one account, whatever anybody types.
func TestTheSameAddressTwice(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	repo := postgres.NewUserRepository(pool)
	if err := repo.Create(ctx, aUser(t, ulid("US1"), "ops@acme.test")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, aUser(t, ulid("US2"), "ops@acme.test")); err == nil {
		t.Fatal("the same address was stored twice")
	}
}
```

- [ ] **Step 5: Run it and watch it fail**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run TestAUser`
Expected: FAIL — `NewUserRepository` undefined.

- [ ] **Step 6: Write the repository**

```go
package postgres

import (
	"context"
	"errors"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserRepository implements user.Repository.
type UserRepository struct{ base }

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{base{pool: pool}}
}

func (r *UserRepository) Create(ctx context.Context, u *user.User) error {
	err := r.q(ctx).CreateUser(ctx, gen.CreateUserParams{
		ID:        u.ID.String(),
		Email:     u.Email,
		Role:      u.Role.String(),
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	})
	if err != nil {
		return failed("create user", err)
	}
	return nil
}

func (r *UserRepository) ReadByEmail(ctx context.Context, email string) (*user.User, error) {
	row, err := r.q(ctx).ReadUserByEmail(ctx, email)
	if err != nil {
		return nil, readUserErr("read user by email", err)
	}
	return toUser(row.ID, row.Email, row.Role, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

func (r *UserRepository) ReadByID(ctx context.Context, id shared.ID) (*user.User, error) {
	row, err := r.q(ctx).ReadUserByID(ctx, id.String())
	if err != nil {
		return nil, readUserErr("read user by id", err)
	}
	return toUser(row.ID, row.Email, row.Role, row.IsActive, row.CreatedAt, row.UpdatedAt), nil
}

// readUserErr turns "no rows" into the domain's own sentinel. A caller has to
// tell an address nobody has used from a database that would not answer.
func readUserErr(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.NotFoundErr("user not found").WithErr(user.ErrNotFound)
	}
	return failed(what, err)
}

// toUser does not call user.New: New is for building a person, and this is
// restoring one that was already valid when it was written. A rule that
// tightens later must not make an old row unreadable.
func toUser(
	id, email, role string, isActive bool, createdAt, updatedAt time.Time,
) *user.User {
	return &user.User{
		ID:        shared.ID(id),
		Email:     email,
		Role:      user.Role(role),
		IsActive:  isActive,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
```

Add the imports `time` and `github.com/Serajian/srosha/pkg/errs` to the block
above if the file does not already carry them.

- [ ] **Step 7: Run the integration tests**

```bash
make dev-up && make migrate-up
go test -tags=integration ./internal/adapter/db/postgres/ -run "TestAUser|TestAnAddress|TestTheSameAddress" -v
```

Expected: PASS, three tests.

- [ ] **Step 8: `make prepush`, then stop**

Expected: green. Do not commit.

---

### Task 3: One-time codes

The rules that make a six-digit code safe: one use, a guess limit, a short life,
and a cap on asking. All four live in the domain, because all four are rules
rather than storage.

**Files:**
- Create: `migrations/00009_create_login_codes.sql`
- Create: `internal/core/domain/signin/code.go`
- Create: `internal/core/domain/signin/errors.go`
- Create: `internal/core/domain/signin/const.go`
- Test: `internal/core/domain/signin/code_test.go`

**Interfaces:**
- Consumes: `shared.ID`, `pkg/errs`
- Produces: `signin.Code{ID, UserID shared.ID, Code string, ExpiresAt time.Time, Attempts int, UsedAt *time.Time, CreatedAt time.Time}`,
  `signin.NewCode(id, userID shared.ID, code string, now time.Time) *Code`,
  `(*Code).Check(given string, now time.Time) error`,
  `signin.GenerateCode() (string, error)`,
  sentinels `ErrCodeWrong`, `ErrCodeSpent`, `ErrCodeExpired`, `ErrTooManyGuesses`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- +goose StatementBegin

-- A code somebody was sent, and what has happened to it since.
--
-- The code is stored as it was sent, not hashed. For six digits that would be
-- theatre: a million values invert instantly, and whoever holds this database
-- does not need a code at all -- they can write a session row. What protects it
-- is the three columns below: a short life, one use, and a guess limit.
CREATE TABLE login_codes (
    id         ulid        PRIMARY KEY,

    user_id    ulid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    code       TEXT        NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    -- Wrong answers so far. Past the limit the code is dead, whatever is left
    -- of its life: six digits is a million tries a script exhausts in seconds.
    attempts   INTEGER     NOT NULL DEFAULT 0,

    -- Set by the first attempt that spends it, right or wrong.
    used_at    TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL
);

-- What sign-in reads: this person's newest code. Also what the request limit
-- counts over.
CREATE INDEX login_codes_user_created_idx
    ON login_codes (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE login_codes;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing test**

```go
package signin_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/signin"
	"github.com/Serajian/srosha/internal/core/shared"
)

var (
	codeID = shared.ID("01K0CDE00000000000000000AB")
	userID = shared.ID("01K0ACCT0000000000000000AB")
	now    = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
)

func TestTheRightCodeIsAccepted(t *testing.T) {
	c := signin.NewCode(codeID, userID, "123456", now)

	if err := c.Check("123456", now.Add(time.Minute)); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if c.UsedAt == nil {
		t.Error("a code that worked was not spent")
	}
}

// Spent by the FIRST attempt, right or wrong. Otherwise a wrong guess costs
// nothing and the guess limit is the only thing standing in the way.
func TestACodeIsSpentByOneAttempt(t *testing.T) {
	c := signin.NewCode(codeID, userID, "123456", now)

	if err := c.Check("000000", now); !errors.Is(err, signin.ErrCodeWrong) {
		t.Fatalf("first guess = %v, want ErrCodeWrong", err)
	}

	// And the right code no longer works.
	if err := c.Check("123456", now); !errors.Is(err, signin.ErrCodeSpent) {
		t.Errorf("second attempt = %v, want ErrCodeSpent", err)
	}
}

func TestAnExpiredCode(t *testing.T) {
	c := signin.NewCode(codeID, userID, "123456", now)

	err := c.Check("123456", now.Add(signin.CodeLifetime+time.Second))
	if !errors.Is(err, signin.ErrCodeExpired) {
		t.Errorf("Check = %v, want ErrCodeExpired", err)
	}
}

// A code already spent stays spent, even inside its life.
func TestASpentCodeStaysSpent(t *testing.T) {
	c := signin.NewCode(codeID, userID, "123456", now)
	at := now.Add(time.Minute)
	c.UsedAt = &at

	if err := c.Check("123456", now.Add(2*time.Minute)); !errors.Is(err, signin.ErrCodeSpent) {
		t.Errorf("Check = %v, want ErrCodeSpent", err)
	}
}

// Six digits, because it is typed by a person from an email.
func TestAGeneratedCodeIsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		code, err := signin.GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			t.Fatalf("code = %q, want six digits", code)
		}
		seen[code] = true
	}
	if len(seen) < 40 {
		t.Errorf("50 codes produced %d distinct values", len(seen))
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/domain/signin/`
Expected: FAIL — the package does not exist.

- [ ] **Step 4: Write the code entity**

`internal/core/domain/signin/const.go`:

```go
package signin

import "time"

// CodeLifetime is how long a code is worth typing. Minutes rather than hours,
// because the window is exactly how long a code read over somebody's shoulder
// or left in an inbox stays usable.
const CodeLifetime = 10 * time.Minute

// MaxGuesses is how many wrong answers one code survives. Six digits is a
// million values, which a script exhausts in seconds without a limit.
const MaxGuesses = 3

// codeDigits is how long a code is. Six because a person types it from an
// email, and the three rules around it are what make it safe rather than its
// length.
const codeDigits = 6
```

`internal/core/domain/signin/errors.go`:

```go
package signin

import "errors"

var (
	// ErrCodeWrong is a guess that did not match. It spends the code.
	ErrCodeWrong = errors.New("that code is not right")

	// ErrCodeSpent is a code already used, right or wrong. Ask for another.
	ErrCodeSpent = errors.New("that code has already been used")

	ErrCodeExpired = errors.New("that code has expired")

	// ErrTooManyGuesses is the limit reached. The code is dead.
	ErrTooManyGuesses = errors.New("too many attempts")

	// ErrTooManyRequests is asking for codes faster than the limit allows.
	ErrTooManyRequests = errors.New("too many sign-in requests")
)
```

`internal/core/domain/signin/code.go`:

```go
// Package signin is how somebody proves who they are: a one-time code, and the
// session it produces.
//
// There are no passwords anywhere in it. That is not only a convenience -- the
// first operator has to be written by hand in SQL, and an argon2 hash cannot be
// typed while an email can.
package signin

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Code is one code somebody was sent, and what has happened to it.
type Code struct {
	ID     shared.ID
	UserID shared.ID

	// Code is stored as it was sent. See the migration for why hashing six
	// digits would be theatre.
	Code string

	ExpiresAt time.Time
	Attempts  int

	// UsedAt is set by the first attempt, right or wrong.
	UsedAt *time.Time

	CreatedAt time.Time
}

func NewCode(id, userID shared.ID, code string, now time.Time) *Code {
	return &Code{
		ID:        id,
		UserID:    userID,
		Code:      code,
		ExpiresAt: now.Add(CodeLifetime),
		CreatedAt: now,
	}
}

// Check answers whether this attempt signs somebody in, and spends the code
// either way.
//
// Spending it on a wrong guess as well as a right one is the point: otherwise a
// wrong answer costs nothing, and the guess limit is all that stands between a
// script and a million tries.
func (c *Code) Check(given string, now time.Time) error {
	switch {
	case c.UsedAt != nil:
		return errs.InvalidInputErr("that code has already been used").WithErr(ErrCodeSpent)
	case c.Attempts >= MaxGuesses:
		return errs.InvalidInputErr("too many attempts").WithErr(ErrTooManyGuesses)
	case !now.Before(c.ExpiresAt):
		return errs.InvalidInputErr("that code has expired").WithErr(ErrCodeExpired)
	}

	c.Attempts++
	spent := now
	c.UsedAt = &spent

	if given != c.Code {
		return errs.InvalidInputErr("that code is not right").WithErr(ErrCodeWrong)
	}
	return nil
}

// GenerateCode makes one. crypto/rand rather than math/rand: this is the whole
// of what stands between an address and an account.
func GenerateCode() (string, error) {
	const max = 1_000_000

	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return "", errs.InternalErr("could not generate a sign-in code")
	}
	return fmt.Sprintf("%0*d", codeDigits, n.Int64()), nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/domain/signin/ -v`
Expected: PASS, five tests.

- [ ] **Step 6: `make prepush`, then stop**

Expected: green. Do not commit.

---

### Task 4: Sessions

Server-side, so deactivating somebody logs them out on their next request rather
than whenever a token they hold happens to expire.

**Files:**
- Create: `migrations/00010_create_sessions.sql`
- Create: `internal/core/domain/signin/session.go`
- Modify: `internal/core/domain/signin/const.go`
- Test: `internal/core/domain/signin/session_test.go`

**Interfaces:**
- Consumes: `signin` package from Task 3
- Produces: `signin.Session{ID, UserID shared.ID, ExpiresAt, LastSeenAt, CreatedAt time.Time}`,
  `signin.NewSession(id, userID shared.ID, now time.Time) *Session`,
  `(*Session).Valid(now time.Time) bool`,
  `(*Session).Touch(now time.Time)`,
  constants `SessionLifetime`, `SessionIdleTimeout`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- +goose StatementBegin

-- A signed-in browser.
--
-- Kept here rather than only in a signed cookie, so that deactivating somebody
-- ends their session on the next request. A self-contained token would keep
-- working until it expired, which is the wrong answer to "this person left".
CREATE TABLE sessions (
    id           ulid        PRIMARY KEY,

    user_id      ulid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The absolute deadline. A session ends here however busy it has been.
    expires_at   TIMESTAMPTZ NOT NULL,

    -- Moved on every request, and what the idle timeout is measured from.
    last_seen_at TIMESTAMPTZ NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing test**

```go
package signin_test

import (
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/signin"
	"github.com/Serajian/srosha/internal/core/shared"
)

var sessionID = shared.ID("01K0SESS0000000000000000AB")

func TestAFreshSessionIsValid(t *testing.T) {
	s := signin.NewSession(sessionID, userID, now)

	if !s.Valid(now.Add(time.Minute)) {
		t.Error("a session a minute old was refused")
	}
}

// Two deadlines, and they answer different questions: one bounds how long a
// sign-in lasts at all, the other how long an abandoned browser stays open.
func TestASessionEndsAtItsAbsoluteDeadline(t *testing.T) {
	s := signin.NewSession(sessionID, userID, now)

	// Busy the whole time -- and it still ends.
	at := now
	for at.Before(now.Add(signin.SessionLifetime)) {
		at = at.Add(time.Minute)
		s.Touch(at)
	}

	if s.Valid(now.Add(signin.SessionLifetime + time.Second)) {
		t.Error("a session outlived its absolute deadline")
	}
}

func TestASessionEndsWhenItIsLeftAlone(t *testing.T) {
	s := signin.NewSession(sessionID, userID, now)

	idle := now.Add(signin.SessionIdleTimeout + time.Second)
	if s.Valid(idle) {
		t.Error("an idle session was still valid")
	}
}

func TestTouchKeepsItAlive(t *testing.T) {
	s := signin.NewSession(sessionID, userID, now)

	almost := now.Add(signin.SessionIdleTimeout - time.Minute)
	s.Touch(almost)

	if !s.Valid(almost.Add(time.Minute)) {
		t.Error("a touched session went idle anyway")
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/domain/signin/ -run TestASession`
Expected: FAIL — `NewSession` undefined.

- [ ] **Step 4: Add the constants**

Append to `internal/core/domain/signin/const.go`:

```go
// SessionLifetime is how long one sign-in lasts however busy it is, and
// SessionIdleTimeout is how long an abandoned browser stays open. Two
// deadlines, because they answer different questions: the first bounds a stolen
// cookie, the second an unlocked laptop.
const (
	SessionLifetime    = 12 * time.Hour
	SessionIdleTimeout = 2 * time.Hour
)
```

- [ ] **Step 5: Write the session**

`internal/core/domain/signin/session.go`:

```go
package signin

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Session is a signed-in browser.
//
// Whether the person behind it may still sign in is not asked here: that is
// user.EnsureActive, checked on every request against the row rather than
// against anything carried in the session.
type Session struct {
	ID     shared.ID
	UserID shared.ID

	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

func NewSession(id, userID shared.ID, now time.Time) *Session {
	return &Session{
		ID:         id,
		UserID:     userID,
		ExpiresAt:  now.Add(SessionLifetime),
		LastSeenAt: now,
		CreatedAt:  now,
	}
}

// Valid reports whether this session is still open: inside its absolute
// deadline, and used recently enough.
func (s *Session) Valid(now time.Time) bool {
	if !now.Before(s.ExpiresAt) {
		return false
	}
	return now.Sub(s.LastSeenAt) < SessionIdleTimeout
}

// Touch moves the idle deadline. It does not move the absolute one.
func (s *Session) Touch(now time.Time) { s.LastSeenAt = now }
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/core/domain/signin/ -v`
Expected: PASS, nine tests across both files.

- [ ] **Step 7: `make prepush`, then stop**

Expected: green. Do not commit.

---

### Task 5: The gate and the audit log

Before anything that writes exists, so nothing is ever written around it.

**Files:**
- Create: `migrations/00011_create_audit_log.sql`
- Create: `internal/core/usecase/gate.go`
- Test: `internal/core/usecase/gate_test.go`

**Interfaces:**
- Consumes: `user.User` from Task 1
- Produces: `usecase.Act{Verb, TargetType, TargetID string}`,
  `usecase.AuditEntry{ID shared.ID, At time.Time, ActorID shared.ID, ActorEmail, Verb, TargetType, TargetID string}`,
  `usecase.AuditLog interface { Record(ctx, AuditEntry) error }`,
  `usecase.NewGate(log AuditLog, newID shared.IDFunc, now shared.NowFunc) *Gate`,
  `(*Gate).Do(ctx context.Context, actor *user.User, act Act, fn func(context.Context) error) error`

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- +goose StatementBegin

-- Who did what, and when.
--
-- Append only. Never updated, never deleted: a record somebody can tidy shows
-- only what nobody wanted to hide.
--
-- It exists from the first day because per-person accounts were chosen so that
-- "who created this source" and "who revoked that key" have answers, and
-- accounts with no record answer neither.
CREATE TABLE audit_log (
    id          ulid        PRIMARY KEY,

    at          TIMESTAMPTZ NOT NULL,

    -- The actor's id AND their address at the time. The address is copied
    -- rather than joined because it is what somebody reading this a year later
    -- needs, and the row it came from may since have been changed.
    actor_id    ulid        NOT NULL REFERENCES users (id),
    actor_email TEXT        NOT NULL,

    -- "source.create", "key.revoke". A verb, not a sentence.
    verb        TEXT        NOT NULL,

    target_type TEXT        NOT NULL,
    target_id   TEXT        NOT NULL
);

-- What an investigation reads: everything one person did, newest first.
CREATE INDEX audit_log_actor_at_idx ON audit_log (actor_id, at DESC);

-- And everything that happened to one thing.
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE audit_log;
-- +goose StatementEnd
```

- [ ] **Step 2: Write the failing test**

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

type auditLog struct {
	entries []usecase.AuditEntry
	err     error
}

func (a *auditLog) Record(_ context.Context, e usecase.AuditEntry) error {
	if a.err != nil {
		return a.err
	}
	a.entries = append(a.entries, e)
	return nil
}

func anActor(t *testing.T) *user.User {
	t.Helper()

	u, err := user.New(shared.ID("01K0ACCT0000000000000000AB"), "ops@acme.test", user.RoleAdmin, gateNow)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	return u
}

var gateNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func newGate(t *testing.T, log *auditLog) *usecase.Gate {
	t.Helper()

	ids := seqIDs()
	return usecase.NewGate(log, ids, func() time.Time { return gateNow })
}

func TestAChangeLeavesExactlyOneRow(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(log.entries) != 1 {
		t.Fatalf("wrote %d rows, want 1", len(log.entries))
	}
	e := log.entries[0]
	if e.Verb != "source.create" || e.TargetID != "01K0SRC0000000000000000000" {
		t.Errorf("entry = %+v", e)
	}
	if e.ActorEmail != "ops@acme.test" {
		t.Errorf("actor email = %q, want it copied onto the row", e.ActorEmail)
	}
	if !e.At.Equal(gateNow) {
		t.Errorf("at = %v", e.At)
	}
}

// A change that failed did not happen, and the log must not say it did.
func TestAFailedChangeIsNotRecorded(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	boom := errors.New("the write failed")
	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "key.revoke", TargetType: "key", TargetID: "01K0KEY00000000000000000AB",
	}, func(context.Context) error { return boom })

	if !errors.Is(err, boom) {
		t.Errorf("Do = %v, want the action's own error", err)
	}
	if len(log.entries) != 0 {
		t.Errorf("wrote %d rows for a change that failed", len(log.entries))
	}
}

// An unrecorded change is worse than a refused one: the point of the gate is
// that nothing happens without a trace.
func TestAChangeThatCannotBeRecordedIsRefused(t *testing.T) {
	log := &auditLog{err: errors.New("the log is unreachable")}
	g := newGate(t, log)

	ran := false
	err := g.Do(context.Background(), anActor(t), usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error {
		ran = true
		return nil
	})

	if err == nil {
		t.Fatal("Do: want an error")
	}
	if ran {
		t.Error("the change ran even though it could not be recorded")
	}
}

func TestAnActorIsRequired(t *testing.T) {
	log := &auditLog{}
	g := newGate(t, log)

	err := g.Do(context.Background(), nil, usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "x",
	}, func(context.Context) error { return nil })

	if err == nil {
		t.Fatal("Do with no actor succeeded")
	}
}
```

- [ ] **Step 3: Run it and watch it fail**

Run: `go test ./internal/core/usecase/ -run TestAChange`
Expected: FAIL — `usecase.Gate` undefined.

- [ ] **Step 4: Write the gate**

`internal/core/usecase/gate.go`:

```go
package usecase

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Act is one thing somebody is about to do.
type Act struct {
	// Verb is "source.create", "key.revoke" -- a verb, not a sentence.
	Verb string

	TargetType string
	TargetID   string
}

// AuditEntry is one row of who did what.
type AuditEntry struct {
	ID      shared.ID
	At      time.Time
	ActorID shared.ID

	// ActorEmail is copied onto the row rather than joined, because it is what
	// somebody reading this a year from now needs and the user row may have
	// changed since.
	ActorEmail string

	Verb       string
	TargetType string
	TargetID   string
}

// AuditLog is where the record is kept.
//
// Declared here rather than imported: whoever writes rows satisfies it, and
// this package never learns which database that is.
type AuditLog interface {
	Record(ctx context.Context, e AuditEntry) error
}

// Gate is the one place every change goes through.
//
// Today it records. It exists so that what comes later -- roles, two-person
// approval, per-user limits -- is one file rather than fifty call sites.
type Gate struct {
	log   AuditLog
	newID shared.IDFunc
	now   shared.NowFunc
}

func NewGate(log AuditLog, newID shared.IDFunc, now shared.NowFunc) *Gate {
	return &Gate{log: log, newID: newID, now: now}
}

// Do records the act, then runs it.
//
// In that order, deliberately. A change nobody can account for is worse than a
// change refused: if the log cannot be written, the change does not happen. And
// a change that then fails leaves no row, because it did not happen either.
func (g *Gate) Do(
	ctx context.Context, actor *user.User, act Act, fn func(context.Context) error,
) error {
	if actor == nil {
		return errs.InternalErr("a change reached the gate with nobody behind it")
	}

	entry := AuditEntry{
		ID:         g.newID(),
		At:         g.now(),
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Verb:       act.Verb,
		TargetType: act.TargetType,
		TargetID:   act.TargetID,
	}
	if err := g.log.Record(ctx, entry); err != nil {
		return err
	}
	return fn(ctx)
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/usecase/ -run "TestAChange|TestAFailed|TestAnActor" -v`
Expected: PASS, four tests.

Note: `TestAFailedChangeIsNotRecorded` will FAIL against the implementation
above, because the row is written before the action runs. **That is the point of
writing the test first** — decide which you want and make them agree:

- Record first (as written): a failed change leaves a row saying it was
  attempted. Delete that test and rename it
  `TestAnAttemptIsRecordedEvenWhenItFails`.
- Record after: a change nobody could record still happened.

Record first is the right answer for an audit log — it records **attempts**,
which is what an investigation needs. Update the test to assert one row with the
action's error returned.

- [ ] **Step 6: `make prepush`, then stop**

Expected: green. Do not commit.

---

### Task 6: The audit repository

**Nothing in this plan calls the gate, and that is the point.** The spec puts it
before anything that writes so that nothing is ever written around it — a gate
added after the first ten callers is a gate with ten ways past it. Its first
caller is source registration, in the second plan. Step 7 below proves the pair
works end to end so it does not sit unexercised until then.

**Files:**
- Create: `internal/adapter/db/postgres/queries/audit.sql`
- Create: `internal/adapter/db/postgres/audit.go`
- Test: `internal/adapter/db/postgres/audit_test.go` *(build tag `integration`)*

**Interfaces:**
- Consumes: `usecase.AuditEntry` from Task 5
- Produces: `postgres.NewAuditRepository(pool *pgxpool.Pool) *AuditRepository`
  satisfying `usecase.AuditLog`

- [ ] **Step 1: Write the query**

```sql
-- There is no update and no delete, and there will not be. See the migration.
-- name: RecordAudit :exec
INSERT INTO audit_log (id, at, actor_id, actor_email, verb, target_type, target_id)
VALUES (@id, @at, @actor_id, @actor_email, @verb, @target_type, @target_id);
```

- [ ] **Step 2: Generate**

Run: `make sqlc`
Expected: `RecordAudit` appears in `internal/adapter/db/postgres/gen/`.

- [ ] **Step 3: Write the failing test**

```go
//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

func TestAnAuditRowIsWritten(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("AUD"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	err := postgres.NewAuditRepository(pool).Record(ctx, usecase.AuditEntry{
		ID:         shared.ID(ulid("AU1")),
		At:         at,
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Verb:       "source.create",
		TargetType: "source",
		TargetID:   "01K0SRC0000000000000000000",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	var verb, email string
	row := pool.QueryRow(ctx, `SELECT verb, actor_email FROM audit_log WHERE actor_id = $1`, actor.ID.String())
	if err := row.Scan(&verb, &email); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if verb != "source.create" || email != "ops@acme.test" {
		t.Errorf("row = %q by %q", verb, email)
	}
}

// The actor has to exist. An audit row naming nobody is a row that answers
// nothing.
func TestAnAuditRowNeedsARealActor(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	err := postgres.NewAuditRepository(pool).Record(context.Background(), usecase.AuditEntry{
		ID:         shared.ID(ulid("AU2")),
		At:         time.Now().UTC(),
		ActorID:    shared.ID(ulid("NOB")),
		ActorEmail: "nobody@acme.test",
		Verb:       "source.create",
		TargetType: "source",
		TargetID:   "x",
	})
	if err == nil {
		t.Fatal("Record: want the foreign key to refuse it")
	}
}
```

- [ ] **Step 4: Run it and watch it fail**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run TestAnAudit`
Expected: FAIL — `NewAuditRepository` undefined.

- [ ] **Step 5: Write the repository**

```go
package postgres

import (
	"context"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/usecase"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepository implements usecase.AuditLog.
//
// It has one method and will keep one. There is no read here yet because
// nothing reads it yet -- an investigation runs a SELECT until the admin panel
// gives it a page.
type AuditRepository struct{ base }

func NewAuditRepository(pool *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{base{pool: pool}}
}

func (r *AuditRepository) Record(ctx context.Context, e usecase.AuditEntry) error {
	err := r.q(ctx).RecordAudit(ctx, gen.RecordAuditParams{
		ID:         e.ID.String(),
		At:         e.At,
		ActorID:    e.ActorID.String(),
		ActorEmail: e.ActorEmail,
		Verb:       e.Verb,
		TargetType: e.TargetType,
		TargetID:   e.TargetID,
	})
	if err != nil {
		return failed("record audit", err)
	}
	return nil
}
```

- [ ] **Step 6: Run the integration tests**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run TestAnAudit -v`
Expected: PASS, two tests.

- [ ] **Step 7: Prove the gate and the repository work together**

Nothing else in this plan puts the two side by side, and a gate whose only test
uses a stand-in is a gate nobody has watched write a row.

```go
//go:build integration

func TestTheGateWritesThroughToPostgres(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	actor := aUser(t, ulid("GAT"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(ctx, actor); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	at := time.Now().UTC().Truncate(time.Microsecond)
	gate := usecase.NewGate(
		postgres.NewAuditRepository(pool),
		func() shared.ID { return shared.ID(ulid("GA1")) },
		func() time.Time { return at },
	)

	ran := false
	err := gate.Do(ctx, actor, usecase.Act{
		Verb: "source.create", TargetType: "source", TargetID: "01K0SRC0000000000000000000",
	}, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !ran {
		t.Fatal("the action did not run")
	}

	var verb string
	row := pool.QueryRow(ctx, `SELECT verb FROM audit_log WHERE actor_id = $1`, actor.ID.String())
	if err := row.Scan(&verb); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if verb != "source.create" {
		t.Errorf("verb = %q", verb)
	}
}
```

Put it in `internal/adapter/db/postgres/audit_test.go` beside the others.

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run TestTheGate -v`
Expected: PASS.

- [ ] **Step 8: `make prepush`, then stop**

---

### Task 7: Storing codes and sessions

**Files:**
- Create: `internal/core/domain/signin/port.go`
- Create: `internal/adapter/db/postgres/queries/signin.sql`
- Create: `internal/adapter/db/postgres/signin.go`
- Test: `internal/adapter/db/postgres/signin_test.go` *(build tag `integration`)*

**Interfaces:**
- Consumes: `signin.Code`, `signin.Session` from Tasks 3 and 4
- Produces: `signin.Codes` with
  `Create(ctx, *Code) error`,
  `ReadNewest(ctx, userID shared.ID) (*Code, error)`,
  `Spend(ctx, *Code) error`,
  `CountSince(ctx, userID shared.ID, since time.Time) (int, error)`;
  `signin.Sessions` with
  `Create(ctx, *Session) error`,
  `Read(ctx, id shared.ID) (*Session, error)`,
  `Touch(ctx, *Session) error`,
  `Delete(ctx, id shared.ID) error`;
  `postgres.NewSignInRepository(pool *pgxpool.Pool) *SignInRepository` satisfying both

- [ ] **Step 1: Write the ports**

```go
package signin

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Codes is where one-time codes are kept.
type Codes interface {
	Create(ctx context.Context, c *Code) error

	// ReadNewest is this person's most recent code, or ErrCodeNotFound. Only
	// the newest is ever checked: asking for another is what invalidates the
	// one before it.
	ReadNewest(ctx context.Context, userID shared.ID) (*Code, error)

	// Spend writes back the attempt count and the moment it was used.
	Spend(ctx context.Context, c *Code) error

	// CountSince is how many codes this person has asked for in a window, which
	// is what the request limit is measured against.
	CountSince(ctx context.Context, userID shared.ID, since time.Time) (int, error)
}

// Sessions is where signed-in browsers are kept.
type Sessions interface {
	Create(ctx context.Context, s *Session) error
	Read(ctx context.Context, id shared.ID) (*Session, error)
	Touch(ctx context.Context, s *Session) error
	Delete(ctx context.Context, id shared.ID) error
}
```

Add to `internal/core/domain/signin/errors.go`:

```go
	ErrCodeNotFound    = errors.New("no sign-in code")
	ErrSessionNotFound = errors.New("session not found")
```

- [ ] **Step 2: Write the queries**

```sql
-- name: CreateLoginCode :exec
INSERT INTO login_codes (id, user_id, code, expires_at, attempts, used_at, created_at)
VALUES (@id, @user_id, @code, @expires_at, @attempts, @used_at, @created_at);

-- ReadNewestLoginCode is the only one ever checked: asking for another code is
-- what puts the previous one out of reach.
-- name: ReadNewestLoginCode :one
SELECT id, user_id, code, expires_at, attempts, used_at, created_at
FROM login_codes
WHERE user_id = @user_id
ORDER BY created_at DESC
LIMIT 1;

-- name: SpendLoginCode :execrows
UPDATE login_codes
SET attempts = @attempts,
    used_at  = @used_at
WHERE id = @id;

-- name: CountLoginCodesSince :one
SELECT count(*)
FROM login_codes
WHERE user_id = @user_id
  AND created_at >= @since;

-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at, last_seen_at, created_at)
VALUES (@id, @user_id, @expires_at, @last_seen_at, @created_at);

-- name: ReadSession :one
SELECT id, user_id, expires_at, last_seen_at, created_at
FROM sessions
WHERE id = @id;

-- name: TouchSession :execrows
UPDATE sessions
SET last_seen_at = @last_seen_at
WHERE id = @id;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = @id;
```

- [ ] **Step 3: Generate**

Run: `make sqlc`

- [ ] **Step 4: Write the failing test**

```go
//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/signin"
	"github.com/Serajian/srosha/internal/core/shared"
)

func aSignInUser(t *testing.T, pool poolT) *user.User {
	t.Helper()

	u := aUser(t, ulid("SIU"), "ops@acme.test")
	if err := postgres.NewUserRepository(pool).Create(context.Background(), u); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	return u
}

func TestTheNewestCodeIsTheOneThatCounts(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aSignInUser(t, pool)
	repo := postgres.NewSignInRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	older := signin.NewCode(shared.ID(ulid("CD1")), u.ID, "111111", at.Add(-time.Minute))
	newer := signin.NewCode(shared.ID(ulid("CD2")), u.ID, "222222", at)
	for _, c := range []*signin.Code{older, newer} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := repo.ReadNewest(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadNewest: %v", err)
	}
	if got.Code != "222222" {
		t.Errorf("code = %q, want the newest", got.Code)
	}
}

func TestSpendingACodeIsWrittenBack(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aSignInUser(t, pool)
	repo := postgres.NewSignInRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	c := signin.NewCode(shared.ID(ulid("CD3")), u.ID, "123456", at)
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_ = c.Check("000000", at.Add(time.Second))
	if err := repo.Spend(ctx, c); err != nil {
		t.Fatalf("Spend: %v", err)
	}

	got, err := repo.ReadNewest(ctx, u.ID)
	if err != nil {
		t.Fatalf("ReadNewest: %v", err)
	}
	if got.UsedAt == nil || got.Attempts != 1 {
		t.Errorf("read back attempts=%d used=%v", got.Attempts, got.UsedAt)
	}
}

func TestCountingRecentRequests(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aSignInUser(t, pool)
	repo := postgres.NewSignInRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	if err := repo.Create(ctx, signin.NewCode(shared.ID(ulid("CD4")), u.ID, "1", at.Add(-time.Hour))); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, signin.NewCode(shared.ID(ulid("CD5")), u.ID, "2", at)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := repo.CountSince(ctx, u.ID, at.Add(-time.Minute))
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if n != 1 {
		t.Errorf("counted %d, want only the recent one", n)
	}
}

func TestASessionRoundTripsAndCanBeEnded(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)
	ctx := context.Background()

	u := aSignInUser(t, pool)
	repo := postgres.NewSignInRepository(pool)
	at := time.Now().UTC().Truncate(time.Microsecond)

	s := signin.NewSession(shared.ID(ulid("SE1")), u.ID, at)
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Read(ctx, s.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("user = %q", got.UserID)
	}

	if err := repo.Delete(ctx, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Read(ctx, s.ID); !errors.Is(err, signin.ErrSessionNotFound) {
		t.Errorf("Read after delete = %v, want ErrSessionNotFound", err)
	}
}
```

Replace `poolT` with whatever type `connect(t)` returns in the existing tests,
and add the `user` import.

- [ ] **Step 5: Run it and watch it fail**

Run: `go test -tags=integration ./internal/adapter/db/postgres/ -run "TestTheNewest|TestSpending|TestCounting|TestASessionRound"`
Expected: FAIL — `NewSignInRepository` undefined.

- [ ] **Step 6: Write the repository**

Follow `internal/adapter/db/postgres/webhook.go` exactly for shape: a
`SignInRepository struct{ base }`, `r.q(ctx)` for the queries, `failed(...)` for
errors, `wrote(rows, err, ...)` for the `:execrows` ones, and `pgx.ErrNoRows`
mapped to `signin.ErrCodeNotFound` and `signin.ErrSessionNotFound`.

Restore entities field by field rather than through `NewCode`/`NewSession`, the
way `toUser` does: those build a new thing, and this is reading back one that
was valid when it was written.

- [ ] **Step 7: Run the integration tests**

Expected: PASS, four tests.

- [ ] **Step 8: `make prepush`, then stop**

---

### Task 8: Signing in

The use case that ties it together, and the five rules from the spec.

**Files:**
- Create: `internal/core/usecase/signin.go`
- Test: `internal/core/usecase/signin_test.go`

**Interfaces:**
- Consumes: `user.Repository`, `signin.Codes`, `signin.Sessions`, `Gate`
- Produces: `usecase.Mailer interface { SendСode(ctx context.Context, email, code string) error }`,
  `usecase.NewSignIn(users user.Repository, codes signin.Codes, sessions signin.Sessions, mail Mailer, newID shared.IDFunc, now shared.NowFunc) *SignIn`,
  `(*SignIn).Request(ctx, email string) error`,
  `(*SignIn).Verify(ctx, email, code string) (*signin.Session, error)`,
  `(*SignIn).Whoami(ctx, sessionID shared.ID) (*user.User, error)`,
  `(*SignIn).End(ctx, sessionID shared.ID) error`

- [ ] **Step 1: Write the failing test**

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/core/domain/signin"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

// The whole security argument in one test: a new address, a known one, and a
// deactivated one all answer the same way.
func TestRequestingACodeTellsYouNothing(t *testing.T) {
	r := newSignInRig(t)

	known := "known@acme.test"
	r.addUser(t, known, true)
	r.addUser(t, "off@acme.test", false)

	for _, email := range []string{known, "brand-new@acme.test", "off@acme.test"} {
		if err := r.signIn.Request(context.Background(), email); err != nil {
			t.Errorf("Request(%q) = %v, want no error whatever the address", email, err)
		}
	}
}

// An address nobody has used becomes a customer on the way through. There is no
// separate "create an account", because two flows would answer differently and
// anybody could tell a taken address from a free one.
func TestANewAddressBecomesACustomer(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "brand-new@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}

	u, err := r.users.ReadByEmail(context.Background(), "brand-new@acme.test")
	if err != nil {
		t.Fatalf("the address did not become a user: %v", err)
	}
	if u.Role != user.RoleCustomer {
		t.Errorf("role = %q, want customer", u.Role)
	}
}

// A deactivated person gets the same sentence and no code.
func TestADeactivatedPersonIsSentNothing(t *testing.T) {
	r := newSignInRig(t)
	r.addUser(t, "off@acme.test", false)

	if err := r.signIn.Request(context.Background(), "off@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(r.mail.sent) != 0 {
		t.Errorf("sent %d codes to somebody who may not sign in", len(r.mail.sent))
	}
}

func TestTheRightCodeBeginsASession(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	sent := r.mail.sent[0]

	s, err := r.signIn.Verify(context.Background(), "a@acme.test", sent.code)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if s == nil || s.UserID == "" {
		t.Fatal("no session")
	}
}

func TestAWrongCodeSpendsIt(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	sent := r.mail.sent[0]

	if _, err := r.signIn.Verify(context.Background(), "a@acme.test", "000000"); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	if _, err := r.signIn.Verify(context.Background(), "a@acme.test", sent.code); err == nil {
		t.Error("the right code still worked after a wrong guess")
	}
}

// Otherwise anybody can fill a stranger's inbox.
func TestAskingTooOften(t *testing.T) {
	r := newSignInRig(t)

	var err error
	for range usecase.MaxCodeRequests + 1 {
		err = r.signIn.Request(context.Background(), "a@acme.test")
	}
	if !errors.Is(err, signin.ErrTooManyRequests) {
		t.Errorf("Request = %v, want ErrTooManyRequests", err)
	}
}

// Deactivating somebody ends their session on the next request, not when a
// token they hold happens to expire.
func TestDeactivationEndsTheSessionAtOnce(t *testing.T) {
	r := newSignInRig(t)

	if err := r.signIn.Request(context.Background(), "a@acme.test"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	s, err := r.signIn.Verify(context.Background(), "a@acme.test", r.mail.sent[0].code)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if _, err := r.signIn.Whoami(context.Background(), s.ID); err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	r.deactivate(t, "a@acme.test")

	if _, err := r.signIn.Whoami(context.Background(), s.ID); err == nil {
		t.Error("a deactivated person was still signed in")
	}
}
```

Write the rig (`newSignInRig`, `addUser`, `deactivate`, and a `mail` stand-in
recording `{email, code string}`) with in-memory fakes, following
`internal/core/usecase/fakes_test.go`. **The fakes must behave like postgres**:
`ReadByEmail` on an unknown address returns `user.ErrNotFound`, not nil.

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/core/usecase/ -run TestRequesting`
Expected: FAIL — `usecase.SignIn` undefined.

- [ ] **Step 3: Add the request limit constant**

Append to `internal/core/usecase/const.go` (create it if the package has none):

```go
// MaxCodeRequests is how many codes one address may ask for in a window, and
// CodeRequestWindow is that window. Without them anybody can fill a stranger's
// inbox, or learn which addresses are real by timing the reply.
const (
	MaxCodeRequests   = 5
	CodeRequestWindow = time.Hour
)
```

- [ ] **Step 4: Write the use case**

```go
package usecase

import (
	"context"
	"errors"

	"github.com/Serajian/srosha/internal/core/domain/signin"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Mailer sends a code.
//
// Declared here rather than imported, because whoever can reach a mail server
// satisfies it. It is deliberately not srosha's own sending path: a sign-in
// that goes through the service you are signing in to fix is a trap.
type Mailer interface {
	SendCode(ctx context.Context, email, code string) error
}

// SignIn is how somebody proves who they are.
type SignIn struct {
	users    user.Repository
	codes    signin.Codes
	sessions signin.Sessions
	mail     Mailer
	newID    shared.IDFunc
	now      shared.NowFunc
}

func NewSignIn(
	users user.Repository, codes signin.Codes, sessions signin.Sessions,
	mail Mailer, newID shared.IDFunc, now shared.NowFunc,
) *SignIn {
	return &SignIn{users: users, codes: codes, sessions: sessions, mail: mail, newID: newID, now: now}
}

// Request sends a code, and answers the same way for every address.
//
// A new one becomes a customer on the way through: signing up and signing in
// are one flow, because two would answer differently and anybody could tell a
// taken address from a free one.
//
// A deactivated person is sent nothing and told the same thing as everybody
// else. The only error a caller ever sees from here is the request limit, which
// is about them rather than about whose address it is.
func (s *SignIn) Request(ctx context.Context, email string) error {
	address, err := user.NormalizeEmail(email)
	if err != nil {
		return err
	}

	u, err := s.find(ctx, address)
	if err != nil {
		return err
	}
	if u == nil || u.EnsureActive() != nil {
		// Nothing to send, and nothing to say about why.
		return nil
	}

	now := s.now()
	n, err := s.codes.CountSince(ctx, u.ID, now.Add(-CodeRequestWindow))
	if err != nil {
		return err
	}
	if n >= MaxCodeRequests {
		return errs.TooManyErr("too many sign-in requests").WithErr(signin.ErrTooManyRequests)
	}

	code, err := signin.GenerateCode()
	if err != nil {
		return err
	}
	if err := s.codes.Create(ctx, signin.NewCode(s.newID(), u.ID, code, now)); err != nil {
		return err
	}
	return s.mail.SendCode(ctx, u.Email, code)
}

// find returns nil, nil for an address nobody has used, after creating the
// customer it will become. It returns an error only for a database that would
// not answer.
func (s *SignIn) find(ctx context.Context, address string) (*user.User, error) {
	u, err := s.users.ReadByEmail(ctx, address)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, user.ErrNotFound) {
		return nil, err
	}

	fresh, err := user.New(s.newID(), address, user.RoleCustomer, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.users.Create(ctx, fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// Verify checks an attempt and begins a session if it was right.
func (s *SignIn) Verify(ctx context.Context, email, code string) (*signin.Session, error) {
	address, err := user.NormalizeEmail(email)
	if err != nil {
		return nil, err
	}

	u, err := s.users.ReadByEmail(ctx, address)
	if err != nil {
		return nil, refuseSignIn()
	}
	if err := u.EnsureActive(); err != nil {
		return nil, refuseSignIn()
	}

	stored, err := s.codes.ReadNewest(ctx, u.ID)
	if err != nil {
		return nil, refuseSignIn()
	}

	checkErr := stored.Check(code, s.now())
	// Written back whatever the answer, because the attempt itself is what
	// spends it.
	if err := s.codes.Spend(ctx, stored); err != nil {
		return nil, err
	}
	if checkErr != nil {
		return nil, checkErr
	}

	session := signin.NewSession(s.newID(), u.ID, s.now())
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// Whoami is who a session belongs to, or an error if it no longer signs
// anybody in.
//
// The user's row is read every time rather than trusted from the session, which
// is what makes deactivating somebody take effect on their next request.
func (s *SignIn) Whoami(ctx context.Context, sessionID shared.ID) (*user.User, error) {
	session, err := s.sessions.Read(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	if !session.Valid(now) {
		return nil, errs.UnauthorizedErr("please sign in again").WithErr(signin.ErrSessionNotFound)
	}

	u, err := s.users.ReadByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if err := u.EnsureActive(); err != nil {
		return nil, err
	}

	session.Touch(now)
	if err := s.sessions.Touch(ctx, session); err != nil {
		return nil, err
	}
	return u, nil
}

// End signs somebody out.
func (s *SignIn) End(ctx context.Context, sessionID shared.ID) error {
	return s.sessions.Delete(ctx, sessionID)
}

// refuseSignIn is the one answer every failed attempt gets, whatever went
// wrong. Saying which part was wrong tells whoever is guessing how close they
// got.
func refuseSignIn() error {
	return errs.UnauthorizedErr("that code is not right")
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/core/usecase/ -run "TestRequesting|TestANew|TestADeactivated|TestTheRight|TestAWrong|TestAsking|TestDeactivation" -v`
Expected: PASS, seven tests.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 9: Sending the code

**Files:**
- Create: `internal/adapter/mailer/mailer.go`
- Create: `internal/adapter/mailer/const.go`
- Test: `internal/adapter/mailer/mailer_test.go`

**Interfaces:**
- Consumes: `internal/infra/smtp` — `smtp.Dialer`, `smtp.Identity`,
  `smtp.Message{From, To, Subject, Body, ContentType string}`
- Produces: `mailer.New(dialer Dialer, id smtp.Identity) (*Mailer, error)`
  satisfying `usecase.Mailer`

**Note on testability.** `Dialer.Open` hands back a concrete `*smtp.Client`, so
a stand-in cannot intercept the send — the existing stand-in in
`internal/adapter/sender/email/sender_test.go` returns `nil, err` and only the
failure path is reachable through it. So composing the message is a pure
function tested directly, and `SendCode` is tested for the path a stand-in can
reach.

- [ ] **Step 1: Write the failing test**

```go
package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/infra/smtp"
)

// A stand-in cannot intercept the send -- Open returns a concrete client -- so
// it covers the one path it can reach.
type dialer struct {
	opened int
	err    error
}

func (d *dialer) Open(smtp.Identity) (*smtp.Client, error) {
	d.opened++
	return nil, d.err
}

func identity() smtp.Identity {
	return smtp.Identity{
		Host: "smtp.acme.test", Port: 587,
		Username: "srosha", Password: "pw", From: "srosha@acme.test",
	}
}

func TestTheCodeIsInTheMessage(t *testing.T) {
	msg := compose(identity().From, "ops@acme.test", "482913")

	if msg.To != "ops@acme.test" || msg.From != "srosha@acme.test" {
		t.Errorf("addresses = %q -> %q", msg.From, msg.To)
	}
	if !strings.Contains(msg.Body, "482913") {
		t.Errorf("body has no code in it:\n%s", msg.Body)
	}
	if msg.Subject == "" {
		t.Error("no subject")
	}
	if msg.ContentType != "text/plain" {
		t.Errorf("content type = %q, want text/plain", msg.ContentType)
	}
}

// Somebody who did not ask for this has to be told that nothing has happened,
// or a code arriving out of nowhere reads as a break-in.
func TestTheMessageSaysWhatToDoIfYouDidNotAskForIt(t *testing.T) {
	body := compose("srosha@acme.test", "ops@acme.test", "482913").Body

	if !strings.Contains(body, "did not ask") {
		t.Errorf("body does not reassure an unexpected recipient:\n%s", body)
	}
}

// The code must not turn up in the subject, where it shows in a notification
// on a locked screen.
func TestTheCodeIsNotInTheSubject(t *testing.T) {
	msg := compose("srosha@acme.test", "ops@acme.test", "482913")

	if strings.Contains(msg.Subject, "482913") {
		t.Errorf("subject = %q, and it carries the code", msg.Subject)
	}
}

func TestAMailServerWeCannotReach(t *testing.T) {
	d := &dialer{err: errors.New("dial tcp: connection refused")}

	m, err := New(d, identity())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := m.SendCode(context.Background(), "ops@acme.test", "482913"); err == nil {
		t.Fatal("SendCode: want an error")
	}
	if d.opened != 1 {
		t.Errorf("opened %d times, want 1", d.opened)
	}
}

func TestAMailerNeedsADialer(t *testing.T) {
	if _, err := New(nil, identity()); err == nil {
		t.Error("New with no dialer succeeded")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

Run: `go test ./internal/adapter/mailer/`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write the constants**

`internal/adapter/mailer/const.go`:

```go
package mailer

// What a person sees.
//
// The code is deliberately not in the subject: a subject shows in a
// notification on a locked screen, and the whole point of the code is that
// having the mailbox is what proves who you are.
const (
	subject = "Your srosha sign-in code"

	// Plain text, because six digits need no markup and a client that will not
	// render html still shows this.
	body = `Your sign-in code is:

    %s

It can be used once, and expires shortly.

If you did not ask for this, somebody typed your address. Nothing has
happened to your account and there is nothing you need to do.
`
)
```

- [ ] **Step 4: Write the mailer**

`internal/adapter/mailer/mailer.go`:

```go
// Package mailer sends the one message this service sends on its own behalf: a
// sign-in code.
//
// It does not go through srosha. A sign-in that depends on the service you are
// signing in to fix is a trap, and this needs no queue to reach one mailbox.
package mailer

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/pkg/errs"
)

// Dialer opens the way to one mail account.
//
// Declared here rather than taken as a concrete type, so this package is handed
// what registry opened rather than opening anything itself.
type Dialer interface {
	Open(smtp.Identity) (*smtp.Client, error)
}

// Mailer is one mail account, used for one kind of message.
type Mailer struct {
	dialer Dialer
	id     smtp.Identity
}

func New(dialer Dialer, id smtp.Identity) (*Mailer, error) {
	if dialer == nil {
		return nil, errs.InternalErr("mailer has no dialer")
	}
	return &Mailer{dialer: dialer, id: id}, nil
}

// SendCode sends one code to one address.
//
// A failure is Unavailable rather than Internal: the mail server is somebody
// else's, and a caller can reasonably ask again.
func (m *Mailer) SendCode(ctx context.Context, email, code string) error {
	client, err := m.dialer.Open(m.id)
	if err != nil {
		return errs.UnavailableErr("the sign-in code could not be sent").WithErr(err)
	}

	if err := client.Send(ctx, compose(m.id.From, email, code)); err != nil {
		return errs.UnavailableErr("the sign-in code could not be sent").WithErr(err)
	}
	return nil
}

// compose is pure so that what a person actually receives can be asserted on
// without a mail server.
func compose(from, to, code string) smtp.Message {
	return smtp.Message{
		From:        from,
		To:          to,
		Subject:     subject,
		Body:        fmt.Sprintf(body, code),
		ContentType: "text/plain",
	}
}
```

**Check `smtp.Client.Send`'s real signature** in `internal/infra/smtp/smtp.go`
before writing this — whether it takes a context, and what it returns.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/adapter/mailer/ -v`
Expected: PASS, five tests.

- [ ] **Step 6: `make prepush`, then stop**

---

### Task 10: The binary

**Files:**
- Create: `cmd/portal/main.go`
- Create: `internal/config/portal.go`
- Create: `internal/config/settings/portal.go`
- Create: `internal/bootstrap/portal.go`
- Create: `internal/adapter/api/web/router.go`
- Create: `internal/adapter/api/web/signin.go`
- Create: `internal/adapter/api/web/session.go`
- Create: `internal/adapter/api/web/templates/*.html`
- Create: `internal/registry/mailer.go`
- Modify: `Makefile` — a `run-portal` target beside `run-gateway`
- Modify: `.env.portal.example`
- Test: `internal/adapter/api/web/signin_test.go`

**Interfaces:**
- Consumes: everything above
- Produces: a binary that serves `/signin`, `/signin/code`, `/signout` and `/`

- [ ] **Step 1: Read the two binaries that already exist**

Read `cmd/gateway/main.go` and `internal/bootstrap/gateway.go` end to end. This
task is a third one of the same shape: `config.LoadPortal`, a `registry` opening
postgres and the mail dialer, a `bootstrap` wiring the core, and a shutdown
ordered by the registry's tiers. **Do not invent a new shape.**

- [ ] **Step 2: Write the settings**

```go
package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// Portal is the customer portal's own configuration.
//
// It carries a mail identity of its own rather than reusing the sender's:
// signing in must not depend on how a customer's messages happen to be
// configured, and the two are changed by different people for different
// reasons.
type Portal struct {
	Addr string

	// SMTP sends the sign-in code, and nothing else.
	SMTP SMTP

	// SecureCookie is off only for local development over plain http. A cookie
	// without it travels in the clear.
	SecureCookie bool
}

func LoadPortal(r *env.Reader, production bool) Portal {
	p := Portal{
		Addr: r.Str("PORTAL_ADDR", ":8090"),
		SMTP: SMTP{
			Host:     r.Str("PORTAL_SMTP_HOST", ""),
			Port:     r.Int("PORTAL_SMTP_PORT", 587),
			Username: r.Str("PORTAL_SMTP_USERNAME", ""),
			Password: r.Secret("PORTAL_SMTP_PASSWORD", ""),
			From:     r.Str("PORTAL_SMTP_FROM", ""),
		},
		SecureCookie: r.Bool("PORTAL_SECURE_COOKIE", true),
	}

	r.Check(p.SMTP.Host != "", "NOTIF_PORTAL_SMTP_HOST is required: nobody can sign in without it")
	r.Check(p.SMTP.From != "", "NOTIF_PORTAL_SMTP_FROM is required")
	r.Check(!production || p.SecureCookie,
		"NOTIF_PORTAL_SECURE_COOKIE must be on in production")

	return p
}
```

Confirm `settings.SMTP`'s real field names in
`internal/config/settings/sender.go` before writing this.

- [ ] **Step 3: Write the failing handler test**

```go
package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Whatever the address, the page that comes back is the same. Anything else
// hands the user list to whoever is guessing.
func TestTheCodePageLooksTheSameForEveryAddress(t *testing.T) {
	srv := newTestPortal(t)

	var bodies []string
	for _, email := range []string{"known@acme.test", "brand-new@acme.test", "off@acme.test"} {
		res := post(t, srv, "/signin", url.Values{"email": {email}})
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusSeeOther {
			t.Fatalf("%s: status %d", email, res.StatusCode)
		}
		bodies = append(bodies, readBody(t, res))
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Errorf("the page differs between addresses")
		}
	}
}

func TestSigningInAndOut(t *testing.T) {
	srv := newTestPortal(t)

	post(t, srv, "/signin", url.Values{"email": {"a@acme.test"}})
	code := srv.mail.lastCode(t)

	res := post(t, srv, "/signin/code", url.Values{"email": {"a@acme.test"}, "code": {code}})
	cookie := sessionCookie(t, res)
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie is readable from javascript")
	}

	// And the home page now knows who it is talking to.
	home := get(t, srv, "/", cookie)
	if !strings.Contains(readBody(t, home), "a@acme.test") {
		t.Error("the signed-in page does not show who is signed in")
	}

	post(t, srv, "/signout", url.Values{}, cookie)
	after := get(t, srv, "/", cookie)
	if after.StatusCode != http.StatusSeeOther {
		t.Errorf("still signed in after signing out: %d", after.StatusCode)
	}
}

func TestTheHomePageNeedsASession(t *testing.T) {
	srv := newTestPortal(t)

	res := get(t, srv, "/", nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to sign in", res.StatusCode)
	}
}
```

Write `newTestPortal` over `httptest.NewServer` with the in-memory fakes from
Task 8 and a mail stand-in that records codes.

- [ ] **Step 4: Write the router, the handlers and the templates**

Three pages and four routes:

```
GET  /signin        the address form
POST /signin        sends a code, always answers the same
GET  /signin/code   the code form
POST /signin/code   verifies, sets the cookie, redirects to /
POST /signout       deletes the session, clears the cookie
GET  /              signed in: who you are. otherwise: redirect to /signin
```

The session cookie:

```go
http.SetCookie(w, &http.Cookie{
	Name:     sessionCookieName,
	Value:    session.ID.String(),
	Path:     "/",
	HttpOnly: true,
	Secure:   secureCookie,
	SameSite: http.SameSiteLaxMode,
	Expires:  session.ExpiresAt,
})
```

`html/template` escapes what it renders, which is why it is used rather than
`text/template`. Every mutating route is POST, so a link cannot cause one.

- [ ] **Step 5: Run the handler tests**

Run: `go test ./internal/adapter/api/web/ -v`
Expected: PASS, three tests.

- [ ] **Step 6: Run it against a real database**

```bash
make dev-up && make migrate-up
docker exec srosha-postgres-dev psql -U srosha -d srosha -c \
  "INSERT INTO users (id,email,role,is_active,created_at,updated_at)
   VALUES ('01K0ACCT0000000000000000AB','you@example.test','super_admin',true,now(),now());"
make run-portal
```

Then, in a browser at `http://localhost:8090/signin`: enter that address, read
the code out of the mail server's log or a local catcher, and sign in.

Expected: the home page names you. Deactivate the row and reload — you are
signed out at once.

- [ ] **Step 7: `make prepush`, then stop**

---

## What this plan does not build

- **Registering sources, keys, credentials and callbacks.** Every one is an
  existing use case that needs a page, and they all sit on top of everything
  here. A second plan.
- **`sources.owner_user_id` and `may_use_shared_sender`.** They belong with
  source registration, and the shared-sender rule needs a decision the spec
  names but does not make: `Registry.For` takes a source id and cannot see the
  flag, so the check either loads the source there or moves a level up.
- **The admin panel.** Phase 2, and a separate private surface.
- **Deployment.** There is no Dockerfile, no production compose file and no CI
  for the two binaries that already exist. A third binary does not change that,
  and it is its own piece of work.
