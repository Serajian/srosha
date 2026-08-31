package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// mayGovernPeople is the one check every method in this file starts with:
// mayOperate plus super_admin. There is no lesser check here, for reads
// either -- an admin has no page that shows the roster and no reason for
// one, their work is other people's sources. A use case that let a read
// through where no page exposes it would leave the route guard as the only
// thing standing between an admin and every customer's email address, and
// one line of routing is not a boundary. Checking here too is what survives
// a route being moved.
//
// Operators.Audit takes this too, from operator_read.go. It is not a people
// page, but its rows carry actor_email and most actors are customers -- see
// its own comment.
func (o *Operators) mayGovernPeople(actor *user.User) error {
	if err := o.mayOperate(actor); err != nil {
		return err
	}
	if actor.Role != user.RoleSuperAdmin {
		return errs.ForbiddenErr("only a super_admin may do this").
			WithErr(ErrNotSuperAdmin).
			WithStr("not a super_admin")
	}
	return nil
}

// People is every account, for the page that manages them.
func (o *Operators) People(ctx context.Context, actor *user.User) ([]user.User, error) {
	if err := o.mayGovernPeople(actor); err != nil {
		return nil, err
	}
	return o.users.List(ctx)
}

// Person is one account, by id.
func (o *Operators) Person(
	ctx context.Context, actor *user.User, id shared.ID,
) (*user.User, error) {
	if err := o.mayGovernPeople(actor); err != nil {
		return nil, err
	}
	return o.users.ReadByID(ctx, id)
}

// SetRole changes what somebody may do.
func (o *Operators) SetRole(
	ctx context.Context, actor *user.User, id shared.ID, role user.Role, note string,
) error {
	if err := o.mayGovernPeople(actor); err != nil {
		return err
	}
	// Nobody closes the last door behind themselves. A super_admin who demoted
	// their own account, or switched it off, would leave the panel reachable
	// only by an UPDATE run by hand -- which is the state this whole surface
	// exists to end.
	if id == actor.ID {
		return errs.InvalidInputErr("you cannot do this to your own account").
			WithErr(ErrSelfTarget).
			WithStr(fmt.Sprintf("actor and target are both %q", id))
	}

	target, err := o.users.ReadByID(ctx, id)
	if err != nil {
		return err
	}

	// ChangeRole never sees the note -- it takes none -- so nothing in the
	// domain bounds it. Trimmed and capped here, before anything is mutated,
	// the same way Suspend's reason is.
	trimmed := strings.TrimSpace(note)
	if len(trimmed) > MaxOperatorNoteLen {
		return errs.InvalidInputErr("that note is too long").
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), MaxOperatorNoteLen))
	}

	if err := target.ChangeRole(role, o.now()); err != nil {
		return err
	}

	act := Act{
		Verb: ActUserRole, TargetType: "user", TargetID: target.ID.String(), Note: trimmed,
	}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.users.UpdateRole(ctx, target)
	})
}

// SetPersonActive switches whether somebody may sign in at all.
func (o *Operators) SetPersonActive(
	ctx context.Context, actor *user.User, id shared.ID, on bool, note string,
) error {
	if err := o.mayGovernPeople(actor); err != nil {
		return err
	}
	// See SetRole: the same door, closed the same way.
	if id == actor.ID {
		return errs.InvalidInputErr("you cannot do this to your own account").
			WithErr(ErrSelfTarget).
			WithStr(fmt.Sprintf("actor and target are both %q", id))
	}

	target, err := o.users.ReadByID(ctx, id)
	if err != nil {
		return err
	}

	// See SetRole: nothing in the domain bounds this note either.
	trimmed := strings.TrimSpace(note)
	if len(trimmed) > MaxOperatorNoteLen {
		return errs.InvalidInputErr("that note is too long").
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), MaxOperatorNoteLen))
	}

	target.IsActive = on
	target.UpdatedAt = o.now()

	verb := ActUserDeactivate
	if on {
		verb = ActUserActivate
	}
	act := Act{Verb: verb, TargetType: "user", TargetID: target.ID.String(), Note: trimmed}
	return o.gate.Do(ctx, actor, act, func(ctx context.Context) error {
		return o.users.SetActive(ctx, target)
	})
}
