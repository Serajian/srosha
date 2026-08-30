package usecase

import "time"

// maxRetentionBatches bounds one retention run.
//
// A backstop, not a setting: the run stops on its own as soon as a batch comes
// back short, and this only exists so a mistake in the age or a clock that moved
// cannot turn one sweep into a job that deletes for hours. Reaching it is worth
// a log line, because it means the table is further behind than one run can fix.
const maxRetentionBatches = 100

// MaxCodeRequests is how many codes one address may ask for in a window, and
// CodeRequestWindow is that window. Without them anybody can fill a stranger's
// inbox, or learn which addresses are real by timing the reply.
const (
	MaxCodeRequests   = 5
	CodeRequestWindow = time.Hour
)

// MaxSourcesPerUser is a backstop, not a plan: srosha treats everybody the
// same, and this exists so one account cannot fill the table before anybody
// notices. Reaching it is worth a conversation, not an upgrade.
const MaxSourcesPerUser = 20

// Verbs, spelled once. They end up in audit_log and are read a year later.
const (
	ActSourceCreate = "source.create"
	ActSourceUpdate = "source.update"
	ActKeyIssue     = "key.issue"
	ActKeyRevoke    = "key.revoke"
)
