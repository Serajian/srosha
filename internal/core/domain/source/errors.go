package source

import "errors"

// Sentinel errors of the source aggregate.
var (
	ErrSourceInactive          = errors.New("source is not active")
	ErrCustomAddressNotAllowed = errors.New("source is not allowed to specify a custom address")
	ErrNoAddressForChannel     = errors.New("no address configured for this channel")
)
