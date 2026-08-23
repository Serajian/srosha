package source

import "errors"

// Sentinel errors of the source aggregate.
var (
	ErrNotFound                = errors.New("source not found")
	ErrSourceInactive          = errors.New("source is not active")
	ErrNoRoutes                = errors.New("at least one channel is required")
	ErrRateLimited             = errors.New("source has sent too many requests")
	ErrCustomAddressNotAllowed = errors.New("source is not allowed to specify a custom address")
	ErrNoAddressForChannel     = errors.New("no address configured for this channel")
)
