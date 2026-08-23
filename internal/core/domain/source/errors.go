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

	ErrMissingSource = errors.New("source id is required")
)

// Sentinel errors of the API keys a source authenticates with.
//
// ErrUnknownKey is the only one authentication ever reports, and it must stay
// that way: a key that was revoked, one that expired and one that never existed
// have to be indistinguishable, or the answer tells whoever is guessing which
// of their strings was once real.
var (
	ErrUnknownKey = errors.New("no live key with that hash")

	ErrKeyLabelRequired  = errors.New("key label is required")
	ErrKeyLabelTooLong   = errors.New("key label is too long")
	ErrKeyAlreadyExpired = errors.New("key expiry is not in the future")
)
