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

	// ActCredentialTest is the customer testing their own identity, so its
	// actor is the customer -- which is why it is not in sourceDecisionVerbs
	// below.
	ActCredentialTest = "credential.test" //nolint:gosec // an audit verb, not a secret

	ActUserRole       = "user.role"
	ActUserDeactivate = "user.deactivate"
	ActUserActivate   = "user.activate"
)

// sourceDecisionVerbs is what Operators.SourceHistory reads a source's own
// audit rows through, and it is a privacy boundary, not a convenience.
//
// /audit is super_admin-only because actor_email on a source.create or
// source.update row is the CUSTOMER's address -- see Operators.Audit. These
// four verbs are different: their actor is always an OPERATOR, a colleague
// deciding a source, never the customer who owns it. That is the whole
// reason SourceHistory is allowed to exist under mayOperate, on a page an
// admin may reach.
//
// Widen this to "every row for this source" -- or build it by excluding
// source.create and source.update instead of naming what is included -- and
// the next verb ever added to this file (key.issue's actor is the customer
// too) is admitted by default, and an admin gains the customer's address
// through a page nobody re-examined. Naming the four operator verbs directly,
// from the constants above rather than as new strings, is what keeps this
// list from drifting out of step with them.
var sourceDecisionVerbs = []string{
	ActSourceApprove, ActSourceRefuse, ActSourceSuspend, ActSourceRestore,
}

// What a trial actually says. Somebody who receives one unexpectedly has to be
// able to tell what it is without asking anybody.
const (
	trialTitle = "srosha test"
	trialBody  = "This is a test message sent from srosha to check a sending identity."
)
