package user

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
