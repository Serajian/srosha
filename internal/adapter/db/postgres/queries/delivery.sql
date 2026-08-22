-- One row per recipient, written in the same transaction as the message.
--
-- status and attempts are not given: a new delivery is always PENDING with none
-- attempted, and the column defaults say so. Passing them would make it possible
-- to create a delivery that was already sent.
--
-- name: CreateDeliveries :copyfrom
INSERT INTO deliveries (
    id, notification_id, channel, address, sender_name, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ReadDelivery :one
SELECT * FROM deliveries WHERE id = @id;

-- name: ListDeliveriesByNotificationID :many
SELECT * FROM deliveries WHERE notification_id = @notification_id ORDER BY id;

-- ListStaleDeliveries is what recovery scans: rows the broker was never told
-- about, or was told about and dropped. Oldest first, so the ones closest to
-- giving up are dealt with first, and limited because this runs on a timer and
-- must not pull a backlog into memory.
--
-- The order is (updated_at, id) rather than updated_at alone. With equal
-- timestamps the order would otherwise be whatever the plan happened to produce,
-- and a caller walking one batch after another could skip a row or see it twice.
--
-- THIS DOES NOT CLAIM THE ROWS. One dispatcher is safe: it reads once and hands
-- the rows to its workers, so no row reaches two of them. Two dispatchers are
-- not: both would read the same PENDING rows, and recovery sends directly rather
-- than republishing, so the broker's duplicate window cannot save it -- somebody
-- gets the message twice.
--
-- FOR UPDATE SKIP LOCKED was here and was removed because it only holds inside a
-- transaction, and the transaction would have to stay open across the sends.
-- Claiming by touching updated_at is worse still: age IS the retry counter, so
-- resetting it means the row never gives up.
--
-- A second dispatcher therefore needs a claimed_at column first. That is a
-- migration, and it is the price of the second replica, not of this query.
--
-- name: ListStaleDeliveries :many
SELECT * FROM deliveries
WHERE status = 'PENDING' AND updated_at < @older_than::timestamptz
ORDER BY updated_at, id
LIMIT @row_limit;

-- PageDeliveriesByNotificationID walks a message's deliveries by id. The id is a
-- ULID, so ordering by it is ordering by time and the cursor needs no second
-- column.
--
-- name: PageDeliveriesByNotificationID :many
SELECT * FROM deliveries
WHERE notification_id = @notification_id
  -- Cast to text, not to ulid: the domain's base type is text, and a cast to
  -- the domain itself is opaque to sqlc, which then types the parameter as any.
  AND (sqlc.narg('after')::text IS NULL OR id > sqlc.narg('after')::text)
ORDER BY id
LIMIT @row_limit;

-- UpdateDelivery records an outcome, and only an outcome: channel, address and
-- sender_name were decided when the message was accepted and never move.
--
-- The status guard is what makes at-least-once safe here. The same delivery can
-- reach two workers; both may send, and both will try to write the result. With
-- the guard the second one changes nothing and gets 0 back, so it knows it lost
-- the race instead of quietly overwriting the first answer. Transient failures
-- write nothing at all, so a row waiting to be retried is still PENDING and this
-- still matches it.
--
-- notified_at is not here. Announcing an outcome is a different act at a
-- different time, and writing it from this statement would mean every result
-- write had to carry the current value or erase it.
--
-- name: UpdateDelivery :execrows
UPDATE deliveries
SET status = @status,
    attempts = @attempts,
    last_error = @last_error,
    failure_reason = @failure_reason,
    provider_message_id = @provider_message_id,
    updated_at = @updated_at::timestamptz
WHERE id = @id AND status = 'PENDING';

-- MarkDeliveryNotified records that the source was told. Separate from the
-- outcome write above so that neither can erase the other, and guarded so a
-- repeated callback does not move the timestamp.
--
-- name: MarkDeliveryNotified :execrows
UPDATE deliveries
SET notified_at = @notified_at::timestamptz
WHERE id = @id AND notified_at IS NULL;
