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

-- PageNotificationsBySource answers "what did I send", newest first.
--
-- DESC and not ASC, unlike every other listing here: a source asking this wants
-- what it just sent, not what it sent when it signed up. The cursor follows --
-- id < after rather than id > after -- because a ULID orders by time and this
-- walks backwards through it.
--
-- The window is optional and both halves are separate: "since yesterday" and
-- "that week in March" are both real questions, and neither should need the
-- other's bound invented.
--
-- One row more than asked for is fetched, which is the whole answer to "is there
-- another page" and costs nothing next to a second query that counts.
--
-- name: PageNotificationsBySource :many
SELECT * FROM notifications
WHERE source_id = @source_id
  -- Cast to text, not to ulid: the domain's base type is text, and a cast to
  -- the domain itself is opaque to sqlc, which then types the parameter as any.
  AND (sqlc.narg('after')::text IS NULL OR id < sqlc.narg('after')::text)
  AND (sqlc.narg('from')::timestamptz IS NULL OR created_at >= sqlc.narg('from')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR created_at < sqlc.narg('until')::timestamptz)
ORDER BY id DESC
LIMIT @row_limit;

-- DeleteNotificationsBefore drops what nobody is going to ask about again.
--
-- Deliveries go with them: the foreign key is ON DELETE CASCADE, so this one
-- statement clears both and there is no second one to keep in step with it.
--
-- In batches, because an unbounded DELETE over a table that has been collecting
-- for a year is one transaction holding locks on all of it. The caller runs this
-- until it stops finding rows.
--
-- Age alone, deliberately -- no check that the deliveries settled. A delivery
-- gives up at RECONCILE_GIVE_UP, which is minutes, so one still PENDING a month
-- later is not work waiting to happen: it is a row recovery never saw. Config
-- refuses a retention age close enough to give-up for that reasoning to fail.
--
-- name: DeleteNotificationsBefore :execrows
DELETE FROM notifications
WHERE id IN (
    SELECT id FROM notifications
    WHERE created_at < @before::timestamptz
    ORDER BY id
    LIMIT @row_limit
);
