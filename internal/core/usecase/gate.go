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
	}
	if err := g.log.Record(ctx, entry); err != nil {
		return err
	}
	return fn(ctx)
}
