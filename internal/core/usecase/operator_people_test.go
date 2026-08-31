package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/usecase"
)

// An admin who could change roles could promote anybody, including themselves
// out of whatever bound they are under. This is the only thing super_admin
// means, and without it the value is a string nobody reads.
func TestAnAdminCannotChangeRoles(t *testing.T) {
	rig := newOperatorRig(t)

	err := rig.ops.SetRole(
		context.Background(), rig.admin, rig.customer.ID, user.RoleSuperAdmin, "promoting",
	)
	if err == nil {
		t.Fatal("an admin changed somebody's role")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused role change still wrote an audit row")
	}
}

func TestASuperAdminCanChangeRoles(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.customer.ID, user.RoleAdmin, "joins the team")
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}

	got, err := rig.ops.Person(ctx, rig.superAdmin, rig.customer.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if got.Role != user.RoleAdmin {
		t.Errorf("role = %q", got.Role)
	}
	if len(rig.log.entries) != 1 || rig.log.entries[0].Verb != usecase.ActUserRole {
		t.Errorf("audit = %+v", rig.log.entries)
	}
	if rig.log.entries[0].Note != "joins the team" {
		t.Errorf("note = %q", rig.log.entries[0].Note)
	}
}

// The last way in must not be closable. A super_admin removing their own role,
// or switching off their own account, locks everybody out of the panel with no
// way back except SQL. One sentinel identifies the refusal on both paths --
// it is the same door, closed the same way, whichever of the two was tried.
func TestASuperAdminCannotDemoteThemselves(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.superAdmin.ID, user.RoleCustomer, "oops")
	if err == nil {
		t.Fatal("a super_admin demoted themselves")
	}
	if !errors.Is(err, usecase.ErrSelfTarget) {
		t.Errorf("SetRole err = %v, want it to wrap usecase.ErrSelfTarget", err)
	}

	err = rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.superAdmin.ID, false, "oops")
	if err == nil {
		t.Fatal("a super_admin switched off their own account")
	}
	if !errors.Is(err, usecase.ErrSelfTarget) {
		t.Errorf("SetPersonActive err = %v, want it to wrap usecase.ErrSelfTarget", err)
	}
}

// An admin reaching either write is refused with an identifiable error, the
// same way a customer reaching Approve is -- not the generic ErrForbidden
// every other refusal in this layer also carries.
func TestAnAdminIsRefusedWithAnIdentifiableError(t *testing.T) {
	rig := newOperatorRig(t)

	err := rig.ops.SetRole(
		context.Background(), rig.admin, rig.customer.ID, user.RoleAdmin, "",
	)
	if !errors.Is(err, usecase.ErrNotSuperAdmin) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotSuperAdmin", err)
	}
}

// A customer reaching either write is refused the same as an admin -- being an
// operator at all is not enough, let alone being one of the two roles.
func TestACustomerCannotChangeRolesOrDeactivateAnybody(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.SetRole(ctx, rig.customer, rig.admin.ID, user.RoleCustomer, ""); err == nil {
		t.Error("a customer changed somebody's role")
	}
	if err := rig.ops.SetPersonActive(ctx, rig.customer, rig.admin.ID, false, ""); err == nil {
		t.Error("a customer switched somebody's account off")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused change still wrote an audit row")
	}
}

// A super_admin switching somebody else off is the ordinary case, and it
// writes one audit row naming what happened.
func TestASuperAdminCanSwitchSomebodyElseOff(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.admin.ID, false, "left the company")
	if err != nil {
		t.Fatalf("SetPersonActive: %v", err)
	}

	got, err := rig.ops.Person(ctx, rig.superAdmin, rig.admin.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if got.IsActive {
		t.Error("the account is still active")
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Verb != usecase.ActUserDeactivate {
		t.Errorf("verb = %q", last.Verb)
	}
	if last.Note != "left the company" {
		t.Errorf("note = %q", last.Note)
	}
}

// Switching somebody back on is a different verb, so a year-old audit log can
// tell the two apart without reading the note.
func TestSwitchingSomebodyBackOnWritesADifferentVerb(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if err := rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.admin.ID, false, ""); err != nil {
		t.Fatalf("SetPersonActive(off): %v", err)
	}
	if err := rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.admin.ID, true, "back"); err != nil {
		t.Fatalf("SetPersonActive(on): %v", err)
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Verb != usecase.ActUserActivate {
		t.Errorf("verb = %q, want %q", last.Verb, usecase.ActUserActivate)
	}

	got, err := rig.ops.Person(ctx, rig.superAdmin, rig.admin.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if !got.IsActive {
		t.Error("the account is still off")
	}
}

// An admin has no page that shows the roster and no reason for one -- their
// work is other people's sources, not other people. Reading it is refused the
// same as changing it, with the same identifiable error, or a route moved
// under this use case would leave an admin able to read every customer's
// email address.
func TestAnAdminCannotListPeople(t *testing.T) {
	rig := newOperatorRig(t)

	_, err := rig.ops.People(context.Background(), rig.admin)
	if err == nil {
		t.Fatal("an admin read the roster")
	}
	if !errors.Is(err, usecase.ErrNotSuperAdmin) {
		t.Errorf("err = %v, want it to wrap usecase.ErrNotSuperAdmin", err)
	}
}

// The one role this whole file exists for can read what it can also change.
func TestASuperAdminMayListPeople(t *testing.T) {
	rig := newOperatorRig(t)

	got, err := rig.ops.People(context.Background(), rig.superAdmin)
	if err != nil {
		t.Fatalf("People: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("People returned %d, want 3", len(got))
	}
}

// A customer reaching the roster is refused, the same as every other operator
// path.
func TestACustomerCannotListPeople(t *testing.T) {
	rig := newOperatorRig(t)

	if _, err := rig.ops.People(context.Background(), rig.customer); err == nil {
		t.Error("a customer read the roster")
	}
}

// Person, the single-account read, is gated the same way People is -- an
// admin refused, a super_admin let through.
func TestPersonIsGatedTheSameWayPeopleIs(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	if _, err := rig.ops.Person(ctx, rig.admin, rig.customer.ID); err == nil {
		t.Error("an admin read one account through Person")
	}

	got, err := rig.ops.Person(ctx, rig.superAdmin, rig.customer.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if got.ID != rig.customer.ID {
		t.Errorf("Person returned %q, want %q", got.ID, rig.customer.ID)
	}
}

// An unknown role is refused before anything is written -- ChangeRole's own
// validation, carried through untouched.
func TestSetRoleRefusesAnUnknownRole(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.customer.ID, user.Role("owner"), "")
	if !errors.Is(err, user.ErrUnknownRole) {
		t.Errorf("err = %v, want it to wrap user.ErrUnknownRole", err)
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused role change still wrote an audit row")
	}
}

// ChangeRole never sees the note -- it takes none -- so nothing in the
// domain bounds it, and the use case has to trim it the same way Suspend's
// reason is trimmed.
func TestARoleChangeNoteIsTrimmed(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.customer.ID, user.RoleAdmin, "  joins  ")
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Note != "joins" {
		t.Errorf("audit note = %q, want it trimmed", last.Note)
	}
}

// Nothing in the domain bounds a role change's note, so an operator's
// arbitrarily long string must not reach audit_log.note unchecked.
func TestARoleChangeNoteThatIsTooLongIsRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	tooLong := strings.Repeat("a", usecase.MaxOperatorNoteLen+1)
	err := rig.ops.SetRole(ctx, rig.superAdmin, rig.customer.ID, user.RoleAdmin, tooLong)
	if err == nil {
		t.Fatal("a role was changed with an over-long note")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused role change still wrote an audit row")
	}
}

// See TestARoleChangeNoteIsTrimmed: SetPersonActive's note has the same gap,
// for the same reason -- nothing it mutates takes a note either.
func TestADeactivationNoteIsTrimmed(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	err := rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.admin.ID, false, "  left  ")
	if err != nil {
		t.Fatalf("SetPersonActive: %v", err)
	}

	last := rig.log.entries[len(rig.log.entries)-1]
	if last.Note != "left" {
		t.Errorf("audit note = %q, want it trimmed", last.Note)
	}
}

// See TestARoleChangeNoteThatIsTooLongIsRefused: the same bound, the same
// reason, on the other of the two writes this file gates.
func TestADeactivationNoteThatIsTooLongIsRefused(t *testing.T) {
	rig := newOperatorRig(t)
	ctx := context.Background()

	tooLong := strings.Repeat("a", usecase.MaxOperatorNoteLen+1)
	err := rig.ops.SetPersonActive(ctx, rig.superAdmin, rig.admin.ID, false, tooLong)
	if err == nil {
		t.Fatal("an account was switched off with an over-long note")
	}
	if len(rig.log.entries) != 0 {
		t.Error("a refused deactivation still wrote an audit row")
	}
}
