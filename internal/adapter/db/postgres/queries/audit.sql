-- There is no update and no delete, and there will not be. See the migration.
-- name: RecordAudit :exec
INSERT INTO audit_log (id, at, actor_id, actor_email, verb, target_type, target_id, note)
VALUES (@id, @at, @actor_id, @actor_email, @verb, @target_type, @target_id, @note);

-- ListAudit is the newest rows first, with no filter. See
-- usecase.Operators.Audit for why there is no filter yet.
-- name: ListAudit :many
SELECT * FROM audit_log ORDER BY at DESC LIMIT @row_limit;
