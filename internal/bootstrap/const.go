package bootstrap

import "time"

// Which binary this is. It goes on every log line, because both write to one
// collector and nothing else separates them there.
const (
	binaryGateway    = "gateway"
	binaryDispatcher = "dispatcher"
	binaryConsole    = "console"
	binaryMigrate    = "migrate"
)

// consoleRateLimit is required by source.Service and never consulted: the
// limiter is spent by Admit, which is the sending path, and the console does
// not send. It is a number so the type is satisfied, not a policy.
const consoleRateLimit = 1_000_000

// probeTimeout bounds one health check. Compose gives the command five
// seconds, so this stays under it: a slow answer should be our error rather
// than docker's kill.
const probeTimeout = 3 * time.Second

// gotifyIgnoredAppID is what the alerter passes as a Gotify address.
//
// Gotify picks the application from the token and ignores this, so the value
// is arbitrary -- it exists only because the sender refuses an empty address,
// which is right for a customer's message and meaningless here.
const gotifyIgnoredAppID = "1"
