package registry

import "time"

// tier is when a dependency closes, declared here rather than inferred from the
// order bootstrap happens to open things in. Shutdown runs from the highest
// down: listeners stop accepting before what they were using goes away.
//
// A dependency picks the tier that says what it is, not where it was opened.
type tier int

const (
	// tierStore is what everything else queries, so it is the last thing left.
	tierStore tier = iota

	// tierBroker still needs the store while it drains what it is holding.
	tierBroker

	// tierClient is outbound: nothing inside the process depends on it.
	tierClient

	// tierServer is inbound. It stops accepting first, so a request in flight
	// still finds everything it needs underneath it.
	tierServer

	// tierHighest is the top of the range Close walks down from. Adding a tier
	// above tierServer means moving this too.
	tierHighest = tierServer
)

// The alert client's shape. Not configuration: one process sends a handful of
// alerts an hour to one host, so there is nothing here an operator would ever
// want to tune, and the one thing they might -- how long to wait -- is
// NOTIF_ALERT_TIMEOUT.
const (
	alertDialTimeout = 5 * time.Second
	alertTLSTimeout  = 5 * time.Second
	alertIdleConns   = 2
	alertIdleTimeout = 90 * time.Second
)
