-- CreateNotification is where the idempotency key is actually enforced, not just
-- checked. The use case reads by key first and returns the original if it finds
-- one, but there is a gap between that read and this write -- and a client that
-- timed out and retried is exactly what puts two requests in that gap at once.
-- Without ON CONFLICT the second one gets a raw unique violation, so the caller
-- is handed a server error for the case the key exists to make graceful, and
-- retries again.
--
-- execrows tells the two apart: one row means this call wrote the message, zero
-- means somebody got there first and the caller should read it back and answer
-- as a duplicate.
--
-- The conflict target repeats the index's WHERE clause because the index is
-- partial: a source that sends no key must not be limited to one message.
--
-- name: CreateNotification :execrows
INSERT INTO notifications (
    id, source_id, idempotency_key, source_name, title, body,
    requested_priority, effective_priority, expire_at, metadata, created_at
) VALUES (
    @id, @source_id, @idempotency_key, @source_name, @title, @body,
    @requested_priority, @effective_priority, @expire_at, @metadata, @created_at
)
ON CONFLICT (source_id, idempotency_key) WHERE idempotency_key IS NOT NULL
DO NOTHING;

-- name: ReadNotification :one
SELECT * FROM notifications WHERE id = @id;

-- ReadNotificationByIdempotencyKey answers "have I seen this request before".
-- Scoped by source, because two customers may pick the same key and neither
-- should be able to reach the other's message by guessing it.
--
-- name: ReadNotificationByIdempotencyKey :one
SELECT * FROM notifications
WHERE source_id = @source_id AND idempotency_key = @idempotency_key;
