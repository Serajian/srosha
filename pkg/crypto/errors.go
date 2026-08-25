package crypto

import "errors"

// Sentinel errors of the keyring.
var (
	ErrNoKeys      = errors.New("keyring is empty")
	ErrNoActiveKey = errors.New("keyring has no active key")
	ErrKeySize     = errors.New("key is not 32 bytes")
	ErrKeyID       = errors.New("key id is empty or contains a separator")

	ErrMalformed  = errors.New("sealed value is malformed")
	ErrOldVersion = errors.New("sealed value was written by a version this build cannot read")
	ErrUnknownKey = errors.New("sealed value names a key the keyring does not hold")
	ErrCannotOpen = errors.New("sealed value could not be opened")
)
