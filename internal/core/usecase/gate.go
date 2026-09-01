package usecase

import (
	"context"
	"fmt"
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

	// Note is why, when the verb does not say it on its own -- a refusal's
	// reason, or what a role change was for. Empty is ordinary: approving needs
	// no justification.
	Note string
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
	Note       string
}

// AuditLog is where the record is kept.
//
// Declared here rather than imported: whoever writes rows satisfies it, and
// this package never learns which database that is.
type AuditLog interface {
	Record(ctx context.Context, e AuditEntry) error

	// List is the newest rows first, capped at limit. No filter yet -- see
	// Operators.Audit for why.
	List(ctx context.Context, limit int32) ([]AuditEntry, error)

	// ListByTarget is one thing's own history: every row naming it, narrowed
	// to the given verbs, newest first, capped at limit. See
	// Operators.SourceHistory for why the verb set is not optional -- it is
	// what makes this read safe for an audience /audit itself is locked away
	// from.
	ListByTarget(
		ctx context.Context, targetType, targetID string, verbs []string, limit int32,
	) ([]AuditEntry, error)
}

// Alerter carries something an operator should know to a channel that is not
// this service's own.
//
// One method, and it returns nothing: an alert that failed changes nothing the
// caller could do about it, and returning an error only invites somebody to
// check one.
type Alerter interface {
	Notify(ctx context.Context, subject, detail string)
}

// Gate is the one place every change goes through.
//
// Today it records and tells. It exists so that what comes later -- roles,
// two-person approval, per-user limits -- is one file rather than fifty call
// sites.
type Gate struct {
	log    AuditLog
	alerts Alerter
	newID  shared.IDFunc
	now    shared.NowFunc
}

// NewGate takes a nil Alerter to mean silence, so a caller that has no channel
// -- a test, a binary built before this existed -- needs to know nothing about
// alerting.
func NewGate(log AuditLog, alerts Alerter, newID shared.IDFunc, now shared.NowFunc) *Gate {
	return &Gate{log: log, alerts: alerts, newID: newID, now: now}
}

// Do records the act, then runs it.
//
// In that order, deliberately. The log records ATTEMPTS: a change nobody can
// account for is worse than a change refused, so if the row cannot be written
// the change does not happen, and a change that then fails still leaves the
// attempt behind -- which is what an investigation is looking for.
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
		Note:       act.Note,
	}
	if err := g.log.Record(ctx, entry); err != nil {
		return err
	}

	if err := fn(ctx); err != nil {
		return err
	}

	// After it happened, and not before. The audit deliberately records
	// attempts -- a change nobody can account for is worse than a change
	// refused -- but an alert saying a source registered is simply wrong if it
	// did not.
	g.tell(ctx, entry)
	return nil
}

// tell hands one audit row to whoever is listening.
//
// The actor's email is in it. That was a decision, not an oversight: whoever
// holds the alert channel's token sees customer addresses, which is the same
// visibility /audit has and the reason /audit is super_admin only.
func (g *Gate) tell(ctx context.Context, e AuditEntry) {
	if g.alerts == nil {
		return
	}

	detail := fmt.Sprintf("%s by %s", e.TargetID, e.ActorEmail)
	if e.Note != "" {
		detail += " -- " + e.Note
	}
	g.alerts.Notify(ctx, e.Verb, detail)
}
