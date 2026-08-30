-- There is no update and no delete, and there will not be. See the migration.
-- name: RecordAudit :exec
INSERT INTO audit_log (id, at, actor_id, actor_email, verb, target_type, target_id)
VALUES (@id, @at, @actor_id, @actor_email, @verb, @target_type, @target_id);
