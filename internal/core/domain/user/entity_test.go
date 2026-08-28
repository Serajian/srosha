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
