package usecase

// maxRetentionBatches bounds one retention run.
//
// A backstop, not a setting: the run stops on its own as soon as a batch comes
// back short, and this only exists so a mistake in the age or a clock that moved
// cannot turn one sweep into a job that deletes for hours. Reaching it is worth
// a log line, because it means the table is further behind than one run can fix.
const maxRetentionBatches = 100
