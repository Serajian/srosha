package credential

import "errors"

// Sentinel errors of the credential aggregate.
var (
	ErrMissingSource = errors.New("source id is required")
	ErrEmptyName     = errors.New("credential name is required")
	ErrInvalidName   = errors.New("credential name has the wrong format")

	ErrNotFound  = errors.New("no credential with that name")
	ErrInactive  = errors.New("credential is not active")
	ErrNoDefault = errors.New("no default credential for this channel")

	// ErrNoCredentials is the empty case, and is deliberately not ErrNoDefault:
	// a source that registered nothing has not chosen anything, while one whose
	// only identity is switched off has. Whoever falls back to a service-wide
	// identity must be able to tell those apart.
	ErrNoCredentials   = errors.New("source has no credential on this channel")
	ErrDefaultUnusable = errors.New("an inactive credential cannot be the default")
)
