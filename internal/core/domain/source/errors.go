package source

import "errors"

// Sentinel errors of the source aggregate.
var (
	ErrEmptyName               = errors.New("a source needs a name")
	ErrKeyNotFound             = errors.New("key not found")
	ErrNotFound                = errors.New("source not found")
	ErrSourceInactive          = errors.New("source is not active")
	ErrNoRoutes                = errors.New("at least one channel is required")
	ErrRateLimited             = errors.New("source has sent too many requests")
	ErrCustomAddressNotAllowed = errors.New("source is not allowed to specify a custom address")
	ErrNoAddressForChannel     = errors.New("no address configured for this channel")

	ErrMissingSource   = errors.New("source id is required")
	ErrNoReason        = errors.New("source: a refusal needs a reason")
	ErrAlreadyApproved = errors.New(
		"source: an approved source cannot be refused, only suspended",
	)
	ErrNotApproved = errors.New(
		"source: a source that was never approved cannot be suspended",
	)
	ErrNotReviewed = errors.New(
		"source: a source nobody has decided about cannot be restored",
	)
	ErrNoReachableAddress = errors.New(
		"source: has no default address and does not allow a custom one, so it cannot be activated")
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
