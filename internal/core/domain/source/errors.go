package source

import "errors"

// Sentinel errors of the source aggregate. This list grows one entry at a
// time, as the code that returns it is written.
var (
	// ErrCustomTargetNotAllowed is returned when a source supplies an explicit
	// target but is not permitted to address arbitrary recipients.
	ErrCustomTargetNotAllowed = errors.New("source is not allowed to specify a custom target")

	// ErrNoTargetForChannel is returned when a source requests a channel it has
	// no configured default for and supplies no explicit target.
	ErrNoTargetForChannel = errors.New("no target configured for this channel")
)
