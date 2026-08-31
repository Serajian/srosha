# Admin on its own host — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Separate the admin surface from the portal by **host and cookie**
instead of by port, so the panel can be reached in a container at all.

**Architecture:** `internal/adapter/api/web` builds two handlers from one
package. Each already constructs its own `*sessions`; today both write the same
cookie name because they differ only by port and a cookie is not scoped by port.
Give `sessions` a name, give each surface its own, and the browser stops sending
a customer's session to the admin host. The production guard that forced the
admin listener onto loopback is then deleted, because loopback inside a
container reaches nothing.

**Tech Stack:** Go 1.26, gin, `net/http` cookies, `internal/config/settings`.

**Spec:** `docs/superpowers/specs/2026-08-31-production-deployment-design.md`
(part A). Read it before task 1 — the security argument for making the panel
public lives there and is not repeated here.

## Global Constraints

- Module `github.com/Serajian/srosha`, Go **1.26**.
- **Never `git commit` without a direct, explicit order from the owner.** Every
  task below ends by leaving the work in the tree with `make precommit` green
  and its change report written — then stopping. "Commit" is the owner's word,
  not a step you take.
- Every commit carries a change report under `docs/changes/YYYY-MM-DD-<slug>.md`,
  written **in Persian**, following `docs/changes/TEMPLATE.md`. One change, one
  new file, never appended to an older one.
- Branch: `refactor/admin-on-its-own-host`, cut from `master`.
- Comments are few and short. Constants live in a package's `const.go`.
- `make precommit` must be green before you stop; `make prepush` before the
  branch is pushed.
- Hostnames are `panel.srosha.ir` (portal) and `admin.srosha.ir` (admin).

---

## File Structure

| File | Responsibility after this plan |
| --- | --- |
| `internal/adapter/api/web/const.go` | two cookie names instead of one, and the comment that explains why there are two |
| `internal/adapter/api/web/session.go` | `sessions` carries the name it reads and writes; `begin` documents the absent `Domain` |
| `internal/adapter/api/web/portal.go` | passes `portalCookieName` |
| `internal/adapter/api/web/admin.go` | passes `adminCookieName` |
| `internal/adapter/api/web/session_test.go` | the three tests that hold the separation |
| `internal/config/settings/console.go` | no loopback guard; `NOTIF_ADMIN_ADDR` is an address like any other |
| `internal/config/config_test.go` | the two tests of that guard go with it |
| `docs/ARCHITECTURE.md` | the separation is host + cookie + role, not port |
| `docs/CONFIG.md` | the ports table and the sentence about the admin port |

---

### Task 1: A cookie per surface

**Files:**
- Modify: `internal/adapter/api/web/const.go:11-18`
- Modify: `internal/adapter/api/web/session.go:14-27,35-58,63`
- Modify: `internal/adapter/api/web/portal.go:115`
- Modify: `internal/adapter/api/web/admin.go:170`
- Test: `internal/adapter/api/web/session_test.go`

**Interfaces:**
- Consumes: nothing from another task.
- Produces: `portalCookieName`, `adminCookieName` (unexported string constants in
  package `web`), and `newSessions(signIn SignIn, name string, secure bool) *sessions`.
  Task 2 does not use them; task 3 describes them.

- [ ] **Step 1: Write the failing tests**

Append to `internal/adapter/api/web/session_test.go`. These live in package
`web` — not `web_test` — because both constants and `sessions` are unexported,
and `sessions` is the only thing in this adapter that reads or writes the
cookie, so testing it here covers both surfaces.

```go
// A session cookie belongs to one surface. The admin surface reads its own
// name and nothing else, so a customer's portal session is not refused there:
// it is never presented.
//
// This is what replaced the loopback guard. If it goes green while the code is
// wrong, the panel is on the internet with one boolean in front of it.
func TestOneSurfaceDoesNotReadTheOthersCookie(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reads   string
		carries string
	}{
		{"admin ignores a portal cookie", adminCookieName, portalCookieName},
		{"portal ignores an admin cookie", portalCookieName, adminCookieName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newSessions(whoever{u: person(t, user.RoleAdmin)}, tc.reads, false)

			var got shared.ID
			var ok bool
			engine := gin.New()
			engine.GET("/x", func(c *gin.Context) { got, ok = s.ID(c) })

			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.AddCookie(&http.Cookie{
				Name: tc.carries, Value: "01K0SESS0000000000000000AB",
			})
			engine.ServeHTTP(httptest.NewRecorder(), req)

			if ok {
				t.Errorf("a %s cookie was read as a session by the %s surface: %q",
					tc.carries, tc.reads, got)
			}
		})
	}
}

// The cookie must stay host-only, and host-only means no Domain attribute.
//
// Setting Domain=srosha.ir would send a customer's session to the admin host
// and undo the whole separation, with nothing failing. So the absence is
// asserted rather than assumed.
func TestTheSessionCookieIsHostOnly(t *testing.T) {
	for _, name := range []string{portalCookieName, adminCookieName} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newSessions(whoever{}, name, false)

			engine := gin.New()
			engine.GET("/x", func(c *gin.Context) {
				s.begin(c, &session.Session{
					ID:        shared.ID("01K0SESS0000000000000000AB"),
					ExpiresAt: time.Now().Add(time.Hour),
				})
			})

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

			for _, c := range rec.Result().Cookies() {
				if c.Name == name && c.Domain != "" {
					t.Fatalf("cookie %q carries Domain=%q, so it is sent to every "+
						"subdomain -- the two surfaces are no longer separated",
						name, c.Domain)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run them and watch them fail to compile**

```bash
go test -count=1 ./internal/adapter/api/web/
```

Expected: a **build failure**, not a red test — `undefined: portalCookieName`,
`undefined: adminCookieName`, and `too many arguments in call to newSessions`.
A build failure is the correct first result here; if you see `ok`, you ran the
wrong package.

- [ ] **Step 3: Two names instead of one**

Replace the `sessionCookieName` block in `internal/adapter/api/web/const.go`:

```go
// Each surface has its own cookie, and the name is what separates them.
//
// It used to be one name for both, and it had to be: the surfaces differed by
// port, and a cookie is not scoped by port. They differ by host now --
// panel.srosha.ir and admin.srosha.ir -- and a cookie is scoped by host, so a
// customer's session is not refused at the admin surface. It is never sent.
//
// Neither is "session": a name that says which service it belongs to is one
// less thing to guess at in a browser's cookie list.
const (
	portalCookieName = "srosha_portal"
	adminCookieName  = "srosha_admin"
)
```

- [ ] **Step 4: Give `sessions` the name**

In `internal/adapter/api/web/session.go`, replace the struct, its constructor
and the doc comment above them:

```go
// sessions is the cookie, and the question of who is behind it. Nothing else in
// this adapter reads or writes one.
//
// Each surface builds its own with its own name, which is what keeps a
// customer's session off the admin host -- see const.go.
type sessions struct {
	signIn SignIn
	name   string
	secure bool
}

func newSessions(signIn SignIn, name string, secure bool) *sessions {
	return &sessions{signIn: signIn, name: name, secure: secure}
}
```

Then replace `sessionCookieName` with `s.name` in all three places: the two
`http.SetCookie` calls in `begin` and `clear`, and the `c.Cookie(...)` call in
`ID`.

In `begin`, add above the `http.SetCookie` call:

```go
	// No Domain, deliberately: without one the cookie is host-only, and
	// host-only is the whole separation between the two surfaces. Setting
	// Domain=srosha.ir would send this to the admin host too, and nothing
	// would fail.
```

- [ ] **Step 5: Pass the name at both call sites**

`internal/adapter/api/web/portal.go:115`:

```go
	sessions := newSessions(d.SignIn, portalCookieName, d.SecureCookie)
```

`internal/adapter/api/web/admin.go:170`:

```go
	sessions := newSessions(d.SignIn, adminCookieName, d.SecureCookie)
```

- [ ] **Step 6: Update the existing test helpers**

In `internal/adapter/api/web/session_test.go`, `guarded` builds a `sessions` and
adds a cookie by the old name. It is the portal's helper, so it takes the
portal's name:

```go
	sessions := newSessions(whoever{u: u}, portalCookieName, false)
```

```go
	req.AddCookie(&http.Cookie{Name: portalCookieName, Value: "01K0SESS0000000000000000AB"})
```

and in `cleared`:

```go
		if c.Name == portalCookieName && c.Value == "" {
```

- [ ] **Step 7: Run the package's tests**

```bash
go test -count=1 ./internal/adapter/api/web/
```

Expected: PASS, including `TestOperatorPagesRefuseACustomer` and
`TestNoAdminRouteAnswersOnThePortal`, which must not have changed.

- [ ] **Step 8: Prove the new test can fail**

Temporarily change `admin.go` to pass `portalCookieName`, run the package's
tests, and confirm `TestOneSurfaceDoesNotReadTheOthersCookie` goes red. Then put
it back and confirm green again.

A test that has never been seen red is a test nobody has checked. This one is
the whole reason the guard may be deleted in task 2.

- [ ] **Step 9: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-cookie-per-surface.md` from
`docs/changes/TEMPLATE.md`, in Persian. Then **stop** — the owner gives the
commit order.

---

### Task 2: The production loopback guard goes

**Files:**
- Modify: `internal/config/settings/console.go:68-89,112-115`
- Modify: `internal/config/config_test.go:101-118` and the loopback-accepting test below it

**Interfaces:**
- Consumes: nothing. Task 1 need not be finished for this to compile, but it
  must be finished before this is deployed — deleting the guard without the
  cookie split puts the panel on the internet behind one boolean.
- Produces: `settings.AdminAddr` with no `bindsLoopback` method.

- [ ] **Step 1: Delete the two tests of the guard**

In `internal/config/config_test.go`, remove
`TestProductionRefusesAnAdminAddressOnEveryInterface` and
`TestProductionAcceptsALoopbackAdminAddress` entirely, including the comment
above the second one about "the three spellings of only this machine".

Leave every `SecureCookie` test alone. That guard is untouched and still right.

- [ ] **Step 2: Run the config tests and watch them fail**

```bash
go test -count=1 ./internal/config/...
```

Expected: **PASS**, and that is the point — deleting a test cannot fail. The
verification here is the next step's, and the reason the order is this way round
is that `bindsLoopback` is otherwise still referenced and will not compile away.

- [ ] **Step 3: Delete the guard**

In `internal/config/settings/console.go`, remove the whole `bindsLoopback`
method with its comment, and remove this from `LoadConsole`:

```go
	r.Check(!production || c.AdminAddr.bindsLoopback(),
		"NOTIF_ADMIN_ADDR must bind the loopback interface in production "+
			"(127.0.0.1, ::1 or localhost): the admin panel is never published, "+
			"and an address like \":8092\" puts it on every interface")
```

If `net` becomes unused in that file, drop the import.

Leave the default alone:

```go
		AdminAddr:      AdminAddr(r.Str("ADMIN_ADDR", "127.0.0.1:8092")),
```

It is still the right default for a laptop, where loopback means what the
deleted comment thought it meant everywhere.

- [ ] **Step 4: Prove production now accepts the address a container needs**

Add to `internal/config/config_test.go`:

```go
// In a container the admin surface listens like any other service and Traefik
// routes to it by host. Loopback there is the container's own namespace, which
// nothing can reach -- see the deployment spec.
func TestProductionAcceptsAnAdminAddressAContainerCanUse(t *testing.T) {
	setMinimum(t)
	setConsoleMinimum(t)
	t.Setenv("NOTIF_APP_ENV", "production")
	t.Setenv("NOTIF_ADMIN_ADDR", ":8092")

	if _, err := config.LoadConsole(); err != nil {
		t.Fatalf("production refused the admin address a container needs: %v", err)
	}
}
```

- [ ] **Step 5: Run it**

```bash
go test -count=1 ./internal/config/...
```

Expected: PASS. If it fails with a message naming `NOTIF_ADMIN_ADDR`, the
`r.Check` was not removed.

- [ ] **Step 6: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-admin-listener-guard-removed.md` in Persian. Say
plainly what protection was removed and what replaced it — a report that
records only the deletion is a report that will read as a mistake later. Then
**stop**.

---

### Task 3: The documents that argue for the old shape

**Files:**
- Modify: `docs/ARCHITECTURE.md:316-330,398-400`
- Modify: `docs/CONFIG.md` — the ports table and the paragraph under it

**Interfaces:**
- Consumes: the constants and behaviour from tasks 1 and 2. Do this last, so
  what is written is what was built.
- Produces: nothing code depends on.

- [ ] **Step 1: Rewrite the listener table in ARCHITECTURE.md**

Section *Two surfaces in one binary, and what keeps them apart*. Replace:

```
:8090   portal   public, reached through Traefik
:8091   healthz  private
:8092   admin    private, never published
```

with the three listeners as they are now — portal and admin both public, each
on its own host, healthz private — and say in a sentence that the admin surface
is public on purpose and what makes that safe. Do not delete the subsection
*Cookies are not scoped by port*: it is now the reason the surfaces could not be
separated by port and are separated by host. Give it that ending.

- [ ] **Step 2: Replace the sentence that is now false**

```
**The admin port is never published.** Not in `ports:`, and not on
`dokploy-network`.
```

That is no longer true. What holds the boundary now is four things, and the
replacement should name all four: three handlers with no shared mux, a live
role read on every request, **a cookie per host**, and the two route tests. Add
the third test from task 1 to the *What must be tested* list beside them.

- [ ] **Step 3: CONFIG.md**

In the ports table, `console | 8092` is no longer "never published". Give it
`admin.srosha.ir`. Add `panel.srosha.ir` and `api.srosha.ir` to their rows while
you are there, since the hostnames are now decided data and this file is where
data lives. Remove the sentence "Its admin port stays on the private network
only."

- [ ] **Step 4: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-admin-host-documents.md` in Persian, then
**stop**.

---

## When this is done

The branch is ready to push and the deployment stack plan
(`2026-08-31-deployment-stack.md`) can start. It cannot start before this, for
the reason in the spec: without the cookie split, part B deploys a panel that is
either unreachable or defended by one boolean.
