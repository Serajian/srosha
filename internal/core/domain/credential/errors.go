package credential

import "errors"

// Sentinel errors of the credential aggregate.
var (
	ErrMissingSource = errors.New("source id is required")
	ErrEmptyName     = errors.New("credential name is required")
	ErrInvalidName   = errors.New("credential name has the wrong format")

	ErrNotFound        = errors.New("no credential with that name")
	ErrInactive        = errors.New("credential is not active")
	ErrNoDefault       = errors.New("no default credential for this channel")
	ErrDefaultUnusable = errors.New("an inactive credential cannot be the default")
)
