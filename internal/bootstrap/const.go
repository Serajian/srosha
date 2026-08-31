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

// lockProbeSeconds is how often the migration lock is retried while another
// migration holds it. goose takes a period and a count rather than a duration,
// and their product is the wait -- so this is the divisor that turns
// NOTIF_MIGRATION_LOCK_TIMEOUT into a count.
const lockProbeSeconds = 5

// consoleRateLimit is required by source.Service and never consulted: the
// limiter is spent by Admit, which is the sending path, and the console does
// not send. It is a number so the type is satisfied, not a policy.
const consoleRateLimit = 1_000_000

// probeTimeout bounds one health check. Compose gives the command five
// seconds, so this stays under it: a slow answer should be our error rather
// than docker's kill.
const probeTimeout = 3 * time.Second
