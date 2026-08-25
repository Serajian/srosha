package nats

import "time"

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

// dispatchConsumer is the name the dispatcher's consumer is known by on the
// broker. It is a constant rather than config for the same reason the subject
// root is: it is this service's own protocol, and a durable consumer that came
// back under a different name would start from the beginning of the stream.
const dispatchConsumer = "dispatcher"

// backoff is how long the broker waits before offering a delivery again.
//
// It is in code rather than config because it is a protocol decision, not an
// operational one: these numbers and MaxDeliver together decide how long a
// message can take to give up, and tuning one without the other is how a row
// ends up retried forever or abandoned in seconds.
//
// The last interval repeats for whatever attempts are left, so a short list
// covers any limit. It is truncated to MaxDeliver before use -- the broker
// refuses a consumer with more intervals than attempts.
//
// The same table is used on both paths, and they are genuinely different: the
// broker applies these itself when a message is never acknowledged, but a
// message that is explicitly nak'ed skips them entirely, so the nak has to
// carry the delay with it.
var backoff = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}
