# Testing a sending identity — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One button on the portal's senders page that sends a real message
through a registered identity and shows the provider's own answer.

**Architecture:** The console gains a `sender.Registry` built with an **empty**
`Fallback`, so it can send as the customer and structurally cannot send as
srosha. A new use case, `usecase.Trials`, resolves the identity by name — the
same way a real send does — and sends one short message to the source's default
address for that channel.

**Tech Stack:** Go 1.26, gin, `internal/adapter/sender`, `internal/registry`.

**Spec:** `docs/superpowers/specs/2026-08-31-credential-trial-design.md`. Read
it before task 1; the argument for why the console may hold a registry at all is
there and is not repeated here.

## Global Constraints

- **Never `git commit` without a direct, explicit order from the owner.** Each
  task ends with the work in the tree, checks green, report written — then stop.
- Every commit carries a change report under `docs/changes/`, in Persian,
  following `docs/changes/TEMPLATE.md`.
- Branch: `feat/credential-trial`, cut from `master`.
- **The console's `Fallback` stays empty.** Filling it in would let a failed
  trial succeed by sending as srosha — a wrong answer wearing a right one's
  clothes. Task 1's test exists to make that impossible to do quietly.
- Operational knobs live in configuration, never in constants. The trial cap is
  `NOTIF_CONSOLE_TRIAL_PER_MINUTE`.
- Errors carry a sentinel through `pkg/errs` `WithErr`. Never match on message
  text.
- Comments are few and short. Constants live in a package's `const.go`.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/config/settings/console.go` | the trial cap |
| `internal/config/console.go` | the console reads http-client settings now |
| `internal/bootstrap/console.go` | builds the registry, with an empty fallback |
| `internal/bootstrap/console_test.go` | the boundary: this registry cannot send as srosha |
| `internal/core/usecase/trial.go` | `Trials.Run` — resolve by name, send, report |
| `internal/core/usecase/trial_test.go` | refusals, resolution, and the provider's error surviving |
| `internal/core/usecase/const.go` | the audit verb |
| `internal/adapter/api/web/portal_identity.go` | the handler |
| `internal/adapter/api/web/portal_const.go` | the route and the page's field |
| `public/templates/portal/senders.html` | the button and the result |
| `docs/CONFIG.md`, `docs/ARCHITECTURE.md` | what the console is now |

---

### Task 1: The console can send as the customer, and only as the customer

**Files:**
- Modify: `internal/config/console.go` — add `HTTPClient settings.HTTPClient`
- Modify: `internal/bootstrap/console.go:236` — `buildIdentityCore` builds the registry
- Create: `internal/bootstrap/console_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `consoleCore.senderRegistry` of type `*sender.Registry`, satisfying
  `delivery.SenderRegistry`. Task 2 takes it as a constructor argument.

- [ ] **Step 1: Write the failing boundary test**

`internal/bootstrap/console_test.go`. This is the most important test in the
change: it asserts what the console **cannot** do.

```go
package bootstrap_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Serajian/srosha/internal/adapter/sender"
	"github.com/Serajian/srosha/internal/core/shared"
)

// The console's registry has no fallback identity, and that is the whole
// security boundary of the trial feature.
//
// With one, a customer whose own credential is broken would press "test",
// srosha would send as itself, and the page would say it worked. A wrong
// answer that looks exactly like a right one.
//
// Every fallback branch begins by asking configured(), so an empty Fallback
// refuses all eight. This asserts it rather than trusting it.
func TestTheConsoleRegistryCannotSendAsSrosha(t *testing.T) {
	reg := consoleRegistry(t)

	for _, c := range []shared.Channel{
		shared.ChannelEmail, shared.ChannelTelegram, shared.ChannelBale,
		shared.ChannelWhatsApp, shared.ChannelMatrix, shared.ChannelGotify,
		shared.ChannelFCM, shared.ChannelAPNs,
	} {
		t.Run(string(c), func(t *testing.T) {
			// A source with no credential of its own is the path that reaches
			// the fallback. It must not reach a sender.
			_, err := reg.For(context.Background(), "01K0SRC00000000000000000AB", c, "")
			if err == nil {
				t.Fatalf("the console built a sender for %s with no credential: "+
					"it can send as srosha", c)
			}
			if !errors.Is(err, sender.ErrNoSender) {
				t.Errorf("refused %s for the wrong reason: %v", c, err)
			}
		})
	}
}
```

`consoleRegistry` is a helper you write in the same file: it builds a
`sender.Registry` exactly as `buildIdentityCore` does — an empty `Fallback`, a
`credential.Service` over a repository that answers "no credentials", and stub
`Secrets`, `GoogleTokens`, `AppleTokens` and `email.Dialer`. Nothing reaches the
network: every channel is refused before a client is used.

Check the exported name of the "no sender" sentinel in
`internal/adapter/sender/registry.go` before writing `errors.Is` — if it is
unexported, assert on the error being non-nil and add a short comment saying
why the sentinel could not be used.

- [ ] **Step 2: Run it and watch it fail to compile**

```bash
go test -count=1 ./internal/bootstrap/
```

Expected: a build failure naming `consoleRegistry` or the sentinel. Not a red
test.

- [ ] **Step 3: Give the console http-client settings**

`internal/config/console.go`. Add the field, and correct the struct's doc
comment, which currently says the console "has no broker, no sending credentials
and no callback secrets: it serves pages and reads rows". The middle clause is
about to be half wrong:

```go
	// It holds no sending credentials of its own -- Fallback is empty in the
	// registry it builds -- but it does open a source's, to send a trial
	// message. That is why it needs an http client.
	HTTPClient settings.HTTPClient
```

Load it in `LoadConsole` beside the others, the way `LoadDispatcher` does.

- [ ] **Step 4: Build the registry**

`internal/bootstrap/console.go`. `buildIdentityCore` gains two parameters — the
`*registry.Resources` and the SMTP dialer already built in `Console()` — and
returns the registry on `consoleCore`:

```go
	providers, err := registry.SenderClient(cfg.HTTPClient, res)
	if err != nil {
		return err
	}
	tokens, err := registry.GoogleTokens(providers)
	if err != nil {
		return err
	}
	apple, err := registry.AppleTokens(now)
	if err != nil {
		return err
	}

	creds := credential.NewService(credentialRows, now)

	// Empty, and it must stay empty. With a fallback the console could send as
	// srosha, and a customer's failed trial would come back a success. Every
	// branch of Registry.ours asks configured() first, so this refuses all
	// eight -- see console_test.go.
	core.senderRegistry, err = sender.NewRegistry(
		creds, secrets, providers, dialer, tokens, apple, sender.Fallback{},
	)
	if err != nil {
		return err
	}
```

`creds` replaces the inline `credential.NewService(credentialRows, now)` in the
`usecase.NewCredentials` call below it, so the registry and the use case share
one service rather than two.

The dialer is the one the console already builds for sign-in codes. It is a
connection factory, not an account: `email.New(dialer, cfg, secret)` supplies
the source's own identity per send.

- [ ] **Step 5: Run the boundary test**

```bash
go test -count=1 ./internal/bootstrap/
```

Expected: PASS, all eight channels refused.

- [ ] **Step 6: Prove the test can fail**

Temporarily give the fallback one channel — `sender.Fallback{TelegramToken: "x"}`
— and run the test again. Expected: the `telegram` subtest goes red saying the
console can send as srosha. Then put it back.

A test that has never been seen red is a test nobody has checked, and this one
is the reason the rest of the feature is safe.

- [ ] **Step 7: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-console-sender-registry.md` in Persian. Then
**stop**.

---

### Task 2: `usecase.Trials`

**Files:**
- Create: `internal/core/usecase/trial.go`
- Create: `internal/core/usecase/trial_test.go`
- Modify: `internal/core/usecase/const.go` — the audit verb and the message
- Modify: `internal/config/settings/console.go` — the cap

**Interfaces:**
- Consumes: `*sender.Registry` from task 1, as the port
  `delivery.SenderRegistry`.
- Produces:

  ```go
  func NewTrials(
      sources *source.Service, creds CredentialReader, senders delivery.SenderRegistry,
      gate *Gate, limiter Limiter, newID shared.IDFunc, now shared.NowFunc,
  ) *Trials

  func (t *Trials) Run(ctx context.Context, sourceID string, credentialID shared.ID) (string, error)
  ```

  The string is the provider's own message id, which the page shows. Task 3
  calls exactly this.

- [ ] **Step 1: The cap, in configuration**

`internal/config/settings/console.go`, in `Console` and `LoadConsole`:

```go
	// TrialPerMinute bounds how often one source may send a test message. The
	// button really sends, so without a cap it is a way to make srosha's server
	// send whatever somebody wants as fast as they can click.
	//
	// It is not the source's sending quota and cannot be: the gateway's limiter
	// is a separate bucket in a separate process.
	TrialPerMinute int
```

```go
		TrialPerMinute: r.Int("CONSOLE_TRIAL_PER_MINUTE", 3),
```

```go
	r.Check(c.TrialPerMinute > 0,
		"NOTIF_CONSOLE_TRIAL_PER_MINUTE must be above zero: a cap of zero is "+
			"not a limit, it is the button refused forever")
```

- [ ] **Step 2: The audit verb**

`internal/core/usecase/const.go`, beside `ActKeyIssue`:

```go
	ActCredentialTest = "credential.test"
```

Do **not** add it to `sourceDecisionVerbs`. That list is the filter behind the
per-source history an operator reads, and the actor here is the customer —
widening it leaks the customer's identity into a page a plain `admin` opens, for
the same reason `/audit` moved to `super_admin`.

- [ ] **Step 3: Write the failing tests**

`internal/core/usecase/trial_test.go`. Each case is a refusal the spec named,
plus the one success. Use the existing fakes in `fakes_test.go` where they fit
and add a `fakeSenders` that records what it was asked for and returns a
canned answer.

```go
// A trial is refused for a source that has not been approved: srosha would be
// sending on behalf of something it has not let out.
func TestATrialNeedsAnActiveSource(t *testing.T)

// The address is the channel's default and nothing else. Without one the trial
// has nowhere to go, and the message says which channel is missing one.
func TestATrialNeedsADefaultAddressForThatChannel(t *testing.T)

// Somebody else's credential id is not found, rather than forbidden: whether
// an id exists is not a customer's business.
func TestAStrangersCredentialIsNotFound(t *testing.T)

// The registry is asked for the credential's NAME, not its id. A trial that
// resolved differently from a real send would prove something else -- and a
// switched-off identity has to be refused here exactly as it is in production.
func TestATrialResolvesByNameLikeARealSend(t *testing.T)

// The provider's own words reach the caller. "Telegram: 401 Unauthorized" is
// something a customer can act on; "test failed" is not.
func TestTheProvidersErrorSurvives(t *testing.T)

// The cap refuses the fourth press in a minute, with the default of three.
func TestTheFourthTrialInAMinuteIsRefused(t *testing.T)

// A trial writes one audit row naming the customer.
func TestATrialIsAudited(t *testing.T)
```

Write each body against the constructor signature above. Every refusal asserts
its **sentinel** through `errors.Is`, never the message text.

- [ ] **Step 4: Run them and watch them fail to compile**

```bash
go test -count=1 ./internal/core/usecase/
```

Expected: `undefined: usecase.NewTrials`.

- [ ] **Step 5: Write `Trials`**

`internal/core/usecase/trial.go`. The order of the checks is the order in the
spec's diagram, and it matters: the cheapest refusals come first, and nothing is
spent before the source is known to be allowed.

```go
func (t *Trials) Run(
	ctx context.Context, sourceID string, credentialID shared.ID,
) (string, error) {
	src, err := t.sources.Load(ctx, sourceID)
	if err != nil {
		return "", err
	}
	if err := src.EnsureActive(); err != nil {
		return "", err
	}

	cred, err := t.creds.ReadByID(ctx, credentialID)
	if err != nil || cred == nil || cred.SourceID != sourceID {
		return "", errs.NotFoundErr("no such sender").WithErr(ErrTrialNoCredential)
	}

	address, ok := src.DefaultAddresses[cred.Channel]
	if !ok || address == "" {
		return "", errs.InvalidInputErr(
			"this channel has no default address, so a test has nowhere to go").
			WithErr(ErrTrialNoAddress).
			WithStr(string(cred.Channel))
	}

	if !t.limiter.Allow(sourceID) {
		return "", errs.TooManyErr("too many tests just now").WithErr(ErrTrialTooMany)
	}

	// By name, exactly as a real send resolves. A switched-off identity is
	// refused here for the same reason it is refused in production.
	out, err := t.senders.For(ctx, sourceID, cred.Channel, cred.Name)
	if err != nil {
		return "", err
	}

	id := t.newID()
	act := Act{Verb: ActCredentialTest, TargetType: "credential", TargetID: credentialID.String()}

	var providerID string
	err = t.gate.Do(ctx, actorOf(ctx), act, func(ctx context.Context) error {
		providerID, err = out.Send(ctx, shared.Message{
			DeliveryID: id,
			Recipient:  shared.Recipient{Channel: cred.Channel, Address: address},
			Title:      trialTitle,
			Body:       trialBody,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return providerID, nil
}
```

`trialTitle` and `trialBody` go in `const.go`. Keep the body one plain sentence
saying this is a test from srosha and naming the identity, so somebody who
receives it unexpectedly knows what it is.

How the actor reaches the gate is the one thing to settle while writing this:
every other use case here takes `actor *user.User` as an argument. Follow that —
`Run(ctx, actor *user.User, sourceID string, credentialID shared.ID)` — rather
than inventing a context value. Update the Interfaces block above if you do, and
tell task 3.

- [ ] **Step 6: Run the tests**

```bash
go test -count=1 ./internal/core/usecase/
```

Expected: PASS.

- [ ] **Step 7: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-credential-trial-usecase.md` in Persian, then
**stop**.

---

### Task 3: The button

**Files:**
- Modify: `internal/adapter/api/web/portal_const.go` — the route
- Modify: `internal/adapter/api/web/portal_identity.go` — the handler
- Modify: `internal/adapter/api/web/portal.go` — mount it, and pass `Trials`
- Modify: `public/templates/portal/senders.html`
- Modify: `internal/adapter/api/web/portal_test.go`

**Interfaces:**
- Consumes: `Trials.Run` from task 2.
- Produces: `POST /sources/:id/senders/:senderID/test`.

- [ ] **Step 1: The route**

`portal_const.go`, beside `pathSenderOff` and `pathSenderOn`:

```go
	pathSenderTest = "/sources/:id/senders/:senderID/test"
```

- [ ] **Step 2: Write the failing tests**

In `portal_test.go`:

```go
// The button sends, and the page says what came back.
func TestATrialShowsTheProvidersMessageID(t *testing.T)

// The provider's own words reach the screen. This is the whole feature: a
// customer can act on "401 Unauthorized" and cannot act on "test failed".
func TestAFailedTrialShowsWhatTheProviderSaid(t *testing.T)

// The senders page is rendered again either way, whole -- use the `whole`
// helper, which checks for </html> and </main>. A page that stops mid-tag is
// how a render error hides.
func TestTheSendersPageIsWholeAfterATrial(t *testing.T)
```

The portal's fake for `SenderPages` gains whatever `Trials` is behind; follow
the shape of the existing fakes in that file.

- [ ] **Step 3: The handler**

`portal_identity.go`, beside `switchSender`. It renders the senders list again
with a result rather than redirecting, for the same reason `keyHandler.issue`
does: a redirect needs somewhere to keep the message in the meantime, and every
such place outlives the page it was meant for.

```go
func (h *identityHandler) testSender(c *gin.Context) {
	id, ok := h.mine(c)
	if !ok {
		return
	}

	providerID, err := h.trials.Run(
		c.Request.Context(), signedInUser(c), id, shared.ID(c.Param("senderID")),
	)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "trial refused", "error", err)
		h.listSendersWith(c, id, message(err))
		return
	}
	h.listSendersOK(c, id, "Sent. The provider called it "+providerID+".")
}
```

`listSendersWith` already exists for the problem case; add `listSendersOK`
beside it, or give the page struct both a `Problem` and a `Result` field — one
of the two, not both mechanisms.

- [ ] **Step 4: The template**

`public/templates/portal/senders.html`. Add the button inside each card's
`<li>`, and the result above the list:

```html
    <form method="post" action="/sources/{{$.SourceID}}/senders/{{.ID}}/test">
      <button type="submit" class="quiet">Send a test</button>
    </form>
```

```html
{{if .Result}}<p class="ok" role="status">{{.Result}}</p>{{end}}
```

Add a one-line hint under the list saying a test goes to the channel's default
address, so nobody has to guess where it went. Check `portal.css` for an
existing success style before inventing `.ok`.

- [ ] **Step 5: Run the tests**

```bash
go test -count=1 ./internal/adapter/api/web/
```

Expected: PASS, including `TestNoAdminRouteAnswersOnThePortal`, which reads the
route tables back and must still be green with a route added.

- [ ] **Step 6: Whole-repo check, then the report**

```bash
go build ./... && go test -count=1 ./... && make precommit
```

Write `docs/changes/2026-08-31-credential-trial-portal.md` in Persian, then
**stop**.

---

### Task 4: What the console is now

**Files:**
- Modify: `docs/CONFIG.md`
- Modify: `docs/ARCHITECTURE.md`

- [ ] **Step 1: CONFIG.md**

Add `NOTIF_CONSOLE_TRIAL_PER_MINUTE` to the console's row in the
application-configuration table, and mark the `http client` group as read by the
console as well as the dispatcher — it is a `✅` in a column that currently says
`—`.

- [ ] **Step 2: ARCHITECTURE.md**

The console is no longer a binary that only reads rows and renders pages. Say
what it can now do and, more importantly, what it cannot: it opens a source's
own credential to send one trial message, and it holds no fallback identity, so
it can never send as srosha. Name the test that holds that.

- [ ] **Step 3: Check and report**

```bash
make precommit
```

Write `docs/changes/2026-08-31-credential-trial-documents.md` in Persian. Show
the `docs/CONFIG.md` diff and wait: entries there are confirmed with the owner
before they are written. Then **stop**.

---

## What this plan does not do

| | |
| --- | --- |
| A credential check that sends nothing | rejected in the spec: a new port method and eight implementations, and no clean endpoint for APNs or WhatsApp |
| Trials from the SDK or gRPC | a portal affordance. The gateway gains no sender adapters |
| A free-text recipient | a different feature with a different argument |
| Recording a trial as a notification | it is a diagnostic, and the message log must not claim the source sent it |
