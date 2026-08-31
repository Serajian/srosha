-- There is no update and no delete, and there will not be. See the migration.
-- name: RecordAudit :exec
INSERT INTO audit_log (id, at, actor_id, actor_email, verb, target_type, target_id, note)
VALUES (@id, @at, @actor_id, @actor_email, @verb, @target_type, @target_id, @note);

-- ListAudit is the newest rows first, with no filter. See
-- usecase.Operators.Audit for why there is no filter yet.
-- name: ListAudit :many
SELECT * FROM audit_log ORDER BY at DESC LIMIT @row_limit;

-- ListAuditByTarget is one thing's own history, narrowed to a caller-chosen
-- verb set, newest first, capped at row_limit. Filters on target_type and
-- target_id, which audit_log_target_idx already covers, plus the verb list --
-- see usecase.sourceDecisionVerbs for why that verb list is a privacy
-- boundary and not a convenience: this statement enforces whatever verbs it
-- is asked for, and it is the CALLER's job to never ask for more than it may
-- show.
-- name: ListAuditByTarget :many
SELECT * FROM audit_log
WHERE target_type = @target_type
  AND target_id = @target_id
  AND verb = ANY(@verbs::text[])
ORDER BY at DESC
LIMIT @row_limit;
