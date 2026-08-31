package usecase

import "errors"

// Sentinel errors of this layer.
//
// A fresh AppError has no identity, and errors.Is needs one -- the same reason
// every domain package keeps its own errors.go. This one is for refusals that
// belong to no single aggregate.
var (
	// ErrNotOperator is mayOperate's refusal. Without it, "not an operator"
	// reaches the caller as the same ErrForbidden as a source's own refusals
	// (ErrSourceInactive, ErrCustomAddressNotAllowed, ...), and nothing above
	// this layer can tell them apart except by reading the message.
	ErrNotOperator = errors.New("actor is not an operator")

	// ErrNotSuperAdmin is mayGovernPeople's refusal. Changing a role, switching
	// an account off, and reading the roster at all are the things that make
	// super_admin mean anything more than admin, and an admin reaching any of
	// them must get an error that says so rather than the generic
	// ErrForbidden every other refusal shares.
	ErrNotSuperAdmin = errors.New("actor is not a super_admin")

	// ErrSelfTarget is SetRole's and SetPersonActive's refusal when the actor
	// names their own account. One sentinel for both: it is the same door,
	// closed the same way, whichever of the two somebody tried.
	ErrSelfTarget = errors.New("actor is the target")
)
