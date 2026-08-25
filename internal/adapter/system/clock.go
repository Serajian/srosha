// Package system is where the service reads what the machine knows: the time,
// and the randomness behind an identifier.
//
// The core never reads either directly. Both arrive as shared.NowFunc and
// shared.IDFunc, so a test hands the domain a fixed instant and a known
// sequence of ids instead of whatever the machine happened to say.
package system

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Clock is the real time, in UTC.
//
// UTC and not the machine's zone, and it is not a preference. Two binaries
// deployed in two places would otherwise stamp the same event differently, an
// expiry written in one zone would be compared against a now in another, and
// the hour a country moves its clocks would move rows with it.
//
// The service converts to a person's zone when it shows a time, which is
// nowhere in this service -- nothing here is read by a person.
func Clock() shared.NowFunc {
	return func() time.Time { return time.Now().UTC() }
}
