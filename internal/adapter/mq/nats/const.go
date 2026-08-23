package nats

// DispatchRoot is the namespace of the stream the gateway publishes deliveries
// to. A second stream picks its own root and builds its subjects from the same
// Subjects type; nothing here is written on the assumption there is only one.
const DispatchRoot = "notify"

const (
	// tokenSeparator is what nats splits a subject on. Everything about
	// wildcards follows from it, which is why a root containing one is refused.
	tokenSeparator = "."

	// tailWildcard matches any number of remaining tokens. A stream captures
	// its whole namespace with it, so adding a level to a subject later does
	// not need the stream reconfigured.
	tailWildcard = ">"

	// tokenWildcard matches exactly one token. Not used to build anything --
	// it is here because a root containing either wildcard would silently
	// change what a stream captures, and both are refused by the same check.
	tokenWildcard = "*"
)
