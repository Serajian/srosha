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

// MaxOperatorNoteLen bounds an operator's free-text note -- a suspension's
// reason, a role change's, a deactivation's -- matching source.maxReviewNoteLen,
// the length the domain itself bounds a refusal's reason to. None of these
// three notes reach an entity the domain validates (they land only on the
// audit row), so nothing in the domain enforces this on its own; declared
// here, unexported on the other side, so this layer isn't the one reaching
// across the boundary for it. One constant for all three rather than one
// per call site: the value is the same rule -- an operator's sentence or two
// -- applied wherever a note has no domain method to pass through.
const MaxOperatorNoteLen = 500

// MaxOperatorMessages bounds one call to Messages. An operator's log is read a
// screen at a time by a person, not paged through by a machine, so this is a
// backstop against a source that has sent a great many messages -- not a page
// size somebody tunes.
const MaxOperatorMessages = 200

// MaxOperatorAudit bounds one call to Audit, the same way and for the same
// reason: a person reading a screen, not a machine paging through one.
const MaxOperatorAudit = 200

// Verbs, spelled once. They end up in audit_log and are read a year later.
const (
	ActSourceCreate  = "source.create"
	ActSourceUpdate  = "source.update"
	ActSourceApprove = "source.approve"
	ActSourceRefuse  = "source.refuse"
	ActSourceSuspend = "source.suspend"
	ActSourceRestore = "source.restore"
	ActKeyIssue      = "key.issue"
	ActKeyRevoke     = "key.revoke"

	ActUserRole       = "user.role"
	ActUserDeactivate = "user.deactivate"
	ActUserActivate   = "user.activate"
)
