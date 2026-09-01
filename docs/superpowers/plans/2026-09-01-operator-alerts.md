# Operator alerts — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One Gotify channel carrying what an operator needs to act on: the
eleven audited changes, and whether each binary is up.

**Architecture:** A one-method port. `internal/adapter/alert` holds a bounded
queue and one worker and formats the message; bootstrap hands it a
`gotify.Sender`, because one adapter may not import another. Two callers feed
it: `usecase.Gate` after a change succeeds, and `bootstrap` for lifecycle. It
reaches Gotify directly — never through srosha's own pipeline, which is the
path it reports on.

**Tech Stack:** Go 1.26, `internal/adapter/sender/gotify`, `internal/registry`.

**Spec:** `docs/superpowers/specs/2026-09-01-operator-alerts-design.md`. Read it
before task 1; the argument for going direct rather than through the pipeline is
there and is not repeated here.

## Global Constraints

- **Never `git commit` without a direct, explicit order from the owner.** Each
  task ends with the work in the tree, `make prepush` green, its change report
  written — then stop.
- `make precommit` is not enough before saying a task is done. `misspell`,
  `unused` and `gci` live in `golangci-lint`, which runs only in `prepush`, and
  they have blocked a push twice.
- Every commit carries a change report under `docs/changes/`, in Persian.
- Branch: `feat/operator-alerts`, cut from `master`.
- **An alert must never block, slow, or fail what it reports.** Any code that
  could is wrong however correct it looks.
- Errors carry a sentinel through `pkg/errs`. Constants live in `const.go`.
- Operational knobs live in configuration, never in constants.

---

## What the code already tells us

Facts checked before this plan, which decide its shape:

| | |
| --- | --- |
| `usecase.NewGate` is built in **one** place | `internal/bootstrap/console.go:192` |
| `Sources`, `Keys`, `Operators` are built in **one** place | the console, same file |
| So all eleven audited verbs happen in the console | the gateway and dispatcher audit nothing |
| `Gate.Do` records the attempt, **then** runs it | `internal/core/usecase/gate.go:101` |
| `gotify.New(client, token, cfg) (*Sender, error)` | `internal/adapter/sender/gotify/sender.go:42` |
| `Sender.Send(ctx, shared.Message) (string, error)` | title and body only; Gotify priority is not exposed |
| Readiness is asked, never polled | `checks(res)` runs only when `/readyz` is requested |

The last one is why task 3 adds a loop: the binary already knows when a
dependency falls over and tells nobody.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/adapter/alert/alert.go` | the queue, the one worker, the drop |
| `internal/adapter/alert/message.go` | what an alert reads like |
| `internal/adapter/alert/alert_test.go` | that it drops rather than blocks, and never returns an error |
| `internal/core/usecase/port.go` | the `Alerter` port, one method |
| `internal/core/usecase/gate.go` | notify after the change succeeds |
| `internal/bootstrap/alert.go` | build it, or build the no-op |
| `internal/bootstrap/watch.go` | the readiness loop |
| `internal/config/settings/alert.go` | server url, token, app id, queue, interval |
| `docs/CONFIG.md` | the new keys |

---

### Task 1: An alerter that cannot hurt anything

**Files:**
- Create: `internal/adapter/alert/alert.go`, `message.go`, `alert_test.go`

**Interfaces:**
- Consumes: nothing. It declares what it needs:

  ```go
  // Pusher is the one thing this package needs from a channel. Declared here
  // rather than imported: one adapter may not import another, and bootstrap
  // passes a gotify.Sender in.
  type Pusher interface {
      Send(ctx context.Context, m shared.Message) (string, error)
  }
  ```

- Produces:

  ```go
  func New(p Pusher, address string, cfg Config, log *slog.Logger) *Alerter
  func (a *Alerter) Notify(ctx context.Context, subject, detail string)
  func (a *Alerter) Close(context.Context) error
  ```

  `Notify` returns nothing. There is no error a caller could act on, and
  returning one invites somebody to check it.

- [ ] **Step 1: Write the failing test, starting with the one that matters**

`internal/adapter/alert/alert_test.go`:

```go
package alert_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/alert"
	"github.com/Serajian/srosha/internal/core/shared"
)

// blocked is a pusher that never returns until it is let go.
type blocked struct{ release chan struct{} }

func (b *blocked) Send(context.Context, shared.Message) (string, error) {
	<-b.release
	return "", nil
}

// A push server that has stopped answering must not stop srosha.
//
// This is the whole point of the package. With the worker stuck and the queue
// full, Notify has to return anyway -- a source registering cannot be made to
// wait on Gotify, and must not be able to be.
func TestNotifyDoesNotBlockWhenTheQueueIsFull(t *testing.T) {
	p := &blocked{release: make(chan struct{})}
	defer close(p.release)

	a := alert.New(p, "42", alert.Config{Queue: 1}, quiet())
	defer func() { _ = a.Close(context.Background()) }()

	done := make(chan struct{})
	go func() {
		for range 50 {
			a.Notify(context.Background(), "subject", "detail")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked: a stuck push server would stall whatever called it")
	}
}

// A pusher that always fails changes nothing for the caller.
func TestAFailingPusherIsInvisibleToTheCaller(t *testing.T) {
	a := alert.New(failing{}, "42", alert.Config{Queue: 4}, quiet())
	defer func() { _ = a.Close(context.Background()) }()

	// Notify returns nothing at all, which is the assertion: there is no error
	// for a caller to mishandle. This runs it to prove it does not panic.
	a.Notify(context.Background(), "subject", "detail")
}

type failing struct{}

func (failing) Send(context.Context, shared.Message) (string, error) {
	return "", errors.New("gotify is down")
}

// What was actually sent, so the wording is checked rather than assumed.
type captor struct{ got chan shared.Message }

func (c captor) Send(_ context.Context, m shared.Message) (string, error) {
	c.got <- m
	return "1", nil
}

func TestTheMessageCarriesTheSubjectAndTheDetail(t *testing.T) {
	c := captor{got: make(chan shared.Message, 1)}
	a := alert.New(c, "42", alert.Config{Queue: 4}, quiet())
	defer func() { _ = a.Close(context.Background()) }()

	a.Notify(context.Background(), "source.create", "someone@acme.test registered 01K0")

	select {
	case m := <-c.got:
		if m.Recipient.Address != "42" {
			t.Errorf("address = %q, want the configured application", m.Recipient.Address)
		}
		if m.Title == "" || m.Body == "" {
			t.Errorf("title = %q, body = %q -- both are read by a person", m.Title, m.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was sent")
	}
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

- [ ] **Step 2: Run it and watch it fail to compile**

```bash
go test -count=1 ./internal/adapter/alert/
```

Expected: `no required module provides package .../internal/adapter/alert`. A
build failure, not a red test.

- [ ] **Step 3: Write the alerter**

`internal/adapter/alert/alert.go`. The shape that matters is the non-blocking
send:

```go
// Notify queues an alert, or drops it.
//
// Dropping is the correct behaviour and not a compromise. Whatever called this
// is in the middle of doing something real -- registering a source, starting a
// process -- and an alert that made that wait would be worse than no alert at
// all. The queue is the whole budget.
func (a *Alerter) Notify(ctx context.Context, subject, detail string) {
	select {
	case a.queue <- item{subject: subject, detail: detail}:
	default:
		a.log.WarnContext(ctx, "alert dropped: the queue is full",
			"subject", subject)
	}
}
```

One worker, reading until the queue closes, each send bounded by its own
timeout taken from `Config`. A failed send is logged and forgotten; see the
spec on why it is not retried.

`Close` closes the queue and waits for the worker, so a shutdown does not cut
an alert in half. It registers with `registry.Resources` in task 2, at a tier
above the store.

`Config` carries `Queue` and `Timeout`. `const.go` carries nothing that varies:
these are operational.

- [ ] **Step 4: Run the tests**

```bash
go test -count=1 ./internal/adapter/alert/
```

Expected: PASS, all three.

- [ ] **Step 5: Prove the blocking test can fail**

Change `Notify` to a plain blocking send — `a.queue <- item{...}` with no
`select` — and run again. `TestNotifyDoesNotBlockWhenTheQueueIsFull` must go red
with its message about a stuck push server. Then put it back.

An alerter that blocks under load looks identical to a working one until the
day it matters. Seeing this test red is the only proof it is watching.

- [ ] **Step 6: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make prepush
```

Write `docs/changes/2026-09-01-alerter.md` in Persian, then **stop**.

---

### Task 2: The binaries say they are up

**Files:**
- Create: `internal/bootstrap/alert.go`
- Create: `internal/config/settings/alert.go`
- Modify: `internal/config/gateway.go`, `dispatcher.go`, `console.go`
- Modify: `internal/bootstrap/gateway.go`, `dispatcher.go`, `console.go`

**Interfaces:**
- Consumes: `alert.New` from task 1.
- Produces: `func alerts(cfg settings.Alert, res *registry.Resources, log *slog.Logger) *alert.Alerter`,
  returning a no-op alerter when nothing is configured. Tasks 3 and 4 take it.

- [ ] **Step 1: The settings**

`internal/config/settings/alert.go`:

```go
// Alert is where operator alerts go. All three Gotify values empty means alerts
// are off, which is every laptop.
type Alert struct {
	ServerURL string
	Token     Secret
	AppID     string

	// Queue is how many alerts may wait before one is dropped. Small on
	// purpose: a backlog of alerts is a backlog of stale news.
	Queue int

	// Timeout bounds one push. Nothing waits on it, so this only decides how
	// long a dead server ties up the single worker.
	Timeout time.Duration

	// ReadyEvery is how often each binary asks itself whether its dependencies
	// are still there. Nothing polls readiness today -- see task 3.
	ReadyEvery time.Duration
}

func (a Alert) Configured() bool {
	return a.ServerURL != "" && a.Token.Reveal() != "" && a.AppID != ""
}
```

Loaded with `ALERT_GOTIFY_SERVER_URL`, `ALERT_GOTIFY_TOKEN`,
`ALERT_GOTIFY_APP_ID`, `ALERT_QUEUE` (default 64), `ALERT_TIMEOUT` (default
10s), `ALERT_READY_EVERY` (default 30s). Add the field to all three config
structs and their loaders.

- [ ] **Step 2: Write the failing test**

`internal/config/config_test.go`:

```go
// Alerts are off unless all three Gotify values are set. A half-configured
// alerter that silently sends nowhere is worse than one that is plainly off.
func TestAlertsNeedEveryGotifyValue(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"nothing": {},
		"no token": {
			"NOTIF_ALERT_GOTIFY_SERVER_URL": "https://push.example",
			"NOTIF_ALERT_GOTIFY_APP_ID":     "42",
		},
		"no app id": {
			"NOTIF_ALERT_GOTIFY_SERVER_URL": "https://push.example",
			"NOTIF_ALERT_GOTIFY_TOKEN":      "t",
		},
	} {
		t.Run(name, func(t *testing.T) {
			setMinimum(t)
			for k, v := range env {
				t.Setenv(k, v)
			}

			cfg, err := config.LoadGateway()
			if err != nil {
				t.Fatalf("LoadGateway: %v", err)
			}
			if cfg.Alert.Configured() {
				t.Error("alerts report themselves configured while incomplete")
			}
		})
	}
}
```

- [ ] **Step 3: Run it**

```bash
go test -count=1 ./internal/config/...
```

Expected: a build failure on `cfg.Alert`, then PASS once step 1 is in.

- [ ] **Step 4: Build the alerter in bootstrap**

`internal/bootstrap/alert.go`:

```go
// alerts builds the operator's channel, or a no-op.
//
// The gotify sender is constructed here rather than inside the alert package
// because one adapter may not import another -- bootstrap is the one place
// that sees both. See make arch-check.
//
// Unconfigured returns an alerter with no queue and no worker: on a laptop
// nobody has a Gotify, and this must cost nothing there.
func alerts(
	cfg settings.Alert, client *http.Client, res *registry.Resources, log *slog.Logger,
) (*alert.Alerter, error)
```

It registers with `res` so shutdown drains it.

Then each of the three `bootstrap.Gateway/Dispatcher/Console` builds one and
announces itself once everything else is open:

```go
	a.Notify(ctx, binaryGateway+" started", cfg.App.Env)
```

A failure to start already returns an error from these functions; alert on that
path too, before returning.

- [ ] **Step 5: Prove it end to end against a real Gotify**

This is the first time srosha has sent anything through any channel, so it is
worth doing by hand rather than trusting a fake:

```bash
NOTIF_ALERT_GOTIFY_SERVER_URL=... NOTIF_ALERT_GOTIFY_TOKEN=... \
NOTIF_ALERT_GOTIFY_APP_ID=... make run-gateway
```

Expected: a push arrives. If it does not, the `appid` assumption in
`(*Sender).endpoint` is where to look first — it has never been tested against
a real server, and this is the test.

- [ ] **Step 6: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make prepush
```

Write `docs/changes/2026-09-01-lifecycle-alerts.md` in Persian, recording
whether step 5 actually delivered. Then **stop**.

---

### Task 3: A binary that loses a dependency says so

**Files:**
- Create: `internal/bootstrap/watch.go`
- Modify: `internal/bootstrap/app.go`

**Interfaces:**
- Consumes: the alerter from task 2, `registry.Resources.Ready`.
- Produces: a goroutine started by `App.Run` and stopped with it.

- [ ] **Step 1: Write the failing test**

`internal/bootstrap/watch_test.go`. The transitions are the whole behaviour, so
the test drives a check from up to down to up and asserts exactly two alerts:

```go
// Alerts fire on the change, not on the state. A database that is down for ten
// minutes is one message, not one every thirty seconds -- the second kind
// trains an operator to ignore the first.
func TestOnlyTransitionsAlert(t *testing.T)
```

- [ ] **Step 2: Run it, watch it fail to compile**

```bash
go test -count=1 ./internal/bootstrap/
```

- [ ] **Step 3: Write the watcher**

A ticker at `cfg.Alert.ReadyEvery`, holding the previous result per check name.
On a name going from nil to non-nil, alert that it is down with the error; on
the reverse, alert that it recovered. Nothing on an unchanged state.

`App.Run` starts it and it stops when `ctx` is done, like everything else there.

- [ ] **Step 4: Run the test**

```bash
go test -count=1 ./internal/bootstrap/
```

- [ ] **Step 5: Prove it by breaking a dependency**

```bash
make dev-up && make run-console   # with alerts configured
make dev-down                     # in another terminal
```

Expected: one alert naming `postgres`, and one naming `schema`, and nothing
further while it stays down. Then `make dev-up` and expect the recoveries.

- [ ] **Step 6: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make prepush
```

Write `docs/changes/2026-09-01-readiness-alerts.md` in Persian, then **stop**.

---

### Task 4: Every audited change reaches the operator

**Files:**
- Modify: `internal/core/usecase/gate.go`, `port.go`
- Modify: `internal/bootstrap/console.go`
- Modify: `internal/core/usecase/gate_test.go`

**Interfaces:**
- Consumes: the alerter, behind a port the core declares.
- Produces: `usecase.NewGate(log AuditLog, alerts Alerter, newID, now) *Gate`.

  **This changes an exported signature.** `NewGate` is built in exactly one
  place (`console.go:192`) and used in tests. A nil `Alerter` must be accepted
  and mean silence, so no existing test has to learn about alerts.

- [ ] **Step 1: The port**

In `internal/core/usecase/port.go`:

```go
// Alerter carries something an operator should know to a channel that is not
// this service's own. One method, and it returns nothing: an alert that failed
// changes nothing the caller can do.
type Alerter interface {
	Notify(ctx context.Context, subject, detail string)
}
```

- [ ] **Step 2: Write the failing tests**

In `internal/core/usecase/gate_test.go`:

```go
// The audit records attempts and the alert records what happened. A source
// that failed to register must not reach the operator as one that did.
func TestTheGateAlertsOnlyAfterTheChangeSucceeds(t *testing.T)

// A gate without an alerter is silent rather than broken, so every existing
// caller and test keeps working.
func TestAGateWithNoAlerterIsSilent(t *testing.T)

// What the operator reads names the verb, the target and who did it.
func TestTheAlertNamesTheVerbTheTargetAndTheActor(t *testing.T)
```

- [ ] **Step 3: Run them, watch them fail**

```bash
go test -count=1 ./internal/core/usecase/
```

- [ ] **Step 4: Notify after success**

```go
	if err := fn(ctx); err != nil {
		return err
	}
	g.notify(ctx, entry)
	return nil
```

`notify` is a small method that returns immediately when `g.alerts` is nil, and
otherwise formats the entry — verb, target, and the actor's email. The email is
included deliberately; see the spec on what that means for whoever holds the
Gotify token.

Do **not** filter by verb. The owner chose all eleven, and a filter is a
configuration change later rather than a shape now.

- [ ] **Step 5: Run the tests, then the package**

```bash
go test -count=1 ./internal/core/usecase/
```

Expected: PASS, including every existing gate and audit test, unchanged.

- [ ] **Step 6: Wire the console and try it for real**

Pass the alerter into `usecase.NewGate` in `console.go:192`. Then, with alerts
configured, register a source through the portal and expect a push naming
`source.create`.

- [ ] **Step 7: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make prepush
```

Write `docs/changes/2026-09-01-audit-alerts.md` in Persian, then **stop**.

---

### Task 5: The keys are written down

**Files:**
- Modify: `docs/CONFIG.md`

- [ ] **Step 1: The new keys**

Add `NOTIF_ALERT_GOTIFY_SERVER_URL`, `NOTIF_ALERT_GOTIFY_TOKEN`,
`NOTIF_ALERT_GOTIFY_APP_ID`, `NOTIF_ALERT_QUEUE`, `NOTIF_ALERT_TIMEOUT` and
`NOTIF_ALERT_READY_EVERY` to the application-configuration table, marked as read
by all three binaries. Say that all three Gotify values empty means off.

Record that the token is a secret and belongs in Dokploy's Environment tab, and
that whoever holds it sees customer email addresses — the same visibility
`/audit` has.

- [ ] **Step 2: Check and report**

```bash
make prepush
```

Write `docs/changes/2026-09-01-alert-config-entries.md` in Persian. Show the
`docs/CONFIG.md` diff and wait: entries there are confirmed with the owner
before they are written. Then **stop**.

---

## What this plan does not do

| | |
| --- | --- |
| Alerting through srosha's own pipeline | argued against in the spec: the alarm must not depend on what it watches |
| A second alert channel | the port takes one pusher; a second is a wiring change, not a redesign |
| Retries, batching, digests | an alert is only useful fresh |
| Filtering which verbs alert | all eleven, by the owner's choice. A filter is configuration later |
| Alerting on individual ERROR log lines | too noisy to be read, and noise is how alerting dies |
