package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/delivery"
	"github.com/Serajian/srosha/internal/core/domain/notification"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Operators is what somebody who works here does to other people's sources.
//
// A separate type from Sources rather than more methods on it. Sources says
// what it is in its own first line -- what a customer does with the things they
// own -- and an operator is not that caller: it checks a role where the other
// checks ownership. One type serving both would mean every method knowing about
// two audiences, and that branch is where the mistake gets written.
type Operators struct {
	repo          source.Repository
	users         user.Repository
	notifications notification.Repository
	deliveries    delivery.Repository
	credentials   credential.Repository
	audit         AuditLog
	gate          *Gate
	now           shared.NowFunc

	// listLimit bounds every list this type reads: the queue, all sources, a
	// source's message log, a source's own decision history, the roster, and
	// the audit feed. One field for all of them -- see settings.Console's
	// AdminListLimit, which this is read from.
	listLimit int32
}

func NewOperators(
	repo source.Repository, users user.Repository,
	notifications notification.Repository, deliveries delivery.Repository,
	credentials credential.Repository,
	audit AuditLog, gate *Gate, now shared.NowFunc, listLimit int32,
) *Operators {
	return &Operators{
		repo: repo, users: users,
		notifications: notifications, deliveries: deliveries, credentials: credentials,
		audit: audit, gate: gate, now: now, listLimit: listLimit,
	}
}

// truncate keeps at most limit items and reports whether more existed.
//
// Every caller here asks its repository for limit+1 rows. Getting more than
// limit back IS the answer to "was this truncated" -- cheaper than a second
// query to count what a screen was never going to read past the first limit
// rows of anyway, and it cannot disagree with what is actually shown, because
// the same slice this trims is the one rendered.
func truncate[T any](rows []T, limit int32) ([]T, bool) {
	if int32(len(rows)) > limit { //nolint:gosec // rows is limit+1 at most, bounded by config
		return rows[:limit], true
	}
	return rows, false
}

// mayOperate is the check every method here begins with.
//
// The route group has a guard too. This is not the same check twice: the guard
// keeps a page off somebody's screen, and this keeps the operation off the
// database whichever route reaches it.
func (o *Operators) mayOperate(actor *user.User) error {
	if actor == nil || !actor.Role.IsOperator() {
		return errs.ForbiddenErr("this is not yours to do").
			WithErr(ErrNotOperator).
			WithStr("not an operator")
	}
	return nil
}

// Queue is every source waiting for a first decision, capped at listLimit.
// The bool says whether more were waiting than fit -- see truncate.
func (o *Operators) Queue(
	ctx context.Context, actor *user.User,
) ([]source.Source, bool, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, false, err
	}
	rows, err := o.repo.ListForReview(ctx, o.listLimit+1)
	if err != nil {
		return nil, false, err
	}
	rows, truncated := truncate(rows, o.listLimit)
	return rows, truncated, nil
}

// AllSources is every source, for the page that filters, capped at listLimit.
func (o *Operators) AllSources(
	ctx context.Context, actor *user.User,
) ([]source.Source, bool, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, false, err
	}
	rows, err := o.repo.ListAll(ctx, o.listLimit+1)
	if err != nil {
		return nil, false, err
	}
	rows, truncated := truncate(rows, o.listLimit)
	return rows, truncated, nil
}

// Source is one source, by id, for an operator rather than its owner -- no
// ownership check, because an operator's whole job is other people's sources.
func (o *Operators) Source(
	ctx context.Context, actor *user.User, id string,
) (*source.Source, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}
	return o.repo.ReadByID(ctx, id)
}

// Approve lets a source send.
func (o *Operators) Approve(ctx context.Context, actor *user.User, id string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	// Validated before the gate, like every other decision here: a source
	// with nowhere to send must write no audit row for an approval that did
	// not happen.
	if err := src.Approve(o.now()); err != nil {
		return err
	}

	act := Act{Verb: ActSourceApprove, TargetType: "source", TargetID: src.ID}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

// Refuse turns a source away at the door, with a reason the customer will read.
func (o *Operators) Refuse(ctx context.Context, actor *user.User, id, note string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	// Validated before the gate, so a refusal with no reason -- or of a source
	// already approved -- writes no audit row for something that did not
	// happen.
	if err := src.Refuse(note, o.now()); err != nil {
		return err
	}

	act := Act{
		Verb: ActSourceRefuse, TargetType: "source", TargetID: src.ID, Note: src.ReviewNote,
	}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

// Suspend stops a source that already got through. The note goes on the act,
// not on the source: a suspension is not a refusal, and the customer's page
// says something different for each.
func (o *Operators) Suspend(ctx context.Context, actor *user.User, id, note string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	// The note never reaches the source -- Suspend the domain method takes
	// none -- so nothing in the domain bounds it. Trimmed and capped here,
	// before anything is mutated, the same way Refuse's reason is.
	trimmed := strings.TrimSpace(note)
	if len(trimmed) > MaxOperatorNoteLen {
		return errs.InvalidInputErr("that reason is too long").
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), MaxOperatorNoteLen))
	}

	// Before the gate, like Refuse's own guard: suspending something nobody
	// has approved must not write an audit row for a thing that did not
	// happen.
	if err := src.Suspend(o.now()); err != nil {
		return err
	}

	act := Act{Verb: ActSourceSuspend, TargetType: "source", TargetID: src.ID, Note: trimmed}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

// Restore is the way back from Suspend or Refuse.
func (o *Operators) Restore(ctx context.Context, actor *user.User, id string) error {
	src, err := o.forDecision(ctx, actor, id)
	if err != nil {
		return err
	}

	// Same as Suspend: validated before the gate, so nothing is recorded for a
	// decision the domain refuses.
	if err := src.Restore(o.now()); err != nil {
		return err
	}

	act := Act{Verb: ActSourceRestore, TargetType: "source", TargetID: src.ID}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.repo.UpdateReview(ctx, src)
	})
}

// forDecision is the role check and the read, together, so no method here can
// do one and forget the other.
func (o *Operators) forDecision(
	ctx context.Context, actor *user.User, id string,
) (*source.Source, error) {
	if err := o.mayOperate(actor); err != nil {
		return nil, err
	}
	return o.repo.ReadByID(ctx, id)
}
