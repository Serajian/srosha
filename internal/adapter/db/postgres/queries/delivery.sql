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
-- The claim is what makes a second dispatcher possible, and it is one statement
-- rather than a transaction held open across the sends.
--
-- SKIP LOCKED settles the instant of contention -- two sweeps arriving together
-- get disjoint sets -- and claimed_at holds the claim for the minutes after it,
-- once the lock is gone. Neither replaces the other: a lock cannot outlive its
-- statement, and a column cannot break a tie.
--
-- The claim expires, because a dispatcher that dies mid-send would otherwise
-- strand the row for ever. It is also released explicitly when a send fails
-- transiently, so the lease covers only the case it was invented for -- see
-- ReleaseDeliveryClaim.
--
-- updated_at is NOT touched. Age is the retry counter, and moving it would mean
-- the row never reaches RECONCILE_GIVE_UP.
--
-- name: ClaimStaleDeliveries :many
UPDATE deliveries
SET claimed_at = @now::timestamptz
WHERE id IN (
    SELECT id FROM deliveries
    WHERE status = 'PENDING'
      AND updated_at < @older_than::timestamptz
      AND (claimed_at IS NULL OR claimed_at < @claim_expired_before::timestamptz)
    ORDER BY updated_at, id
    LIMIT @row_limit
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- ReleaseDeliveryClaim hands a row back before its lease is up.
--
-- A transient failure writes nothing -- the row stays PENDING and only gets
-- older -- so without this it would sit unclaimable until the lease expired, and
-- the lease would silently become the retry interval. With reconcile every five
-- minutes, give-up at thirty and a ten minute lease, a row would get three
-- attempts where the configuration says six.
--
-- So the lease means one thing only: the dispatcher holding this row is gone.
--
-- name: ReleaseDeliveryClaim :execrows
UPDATE deliveries
SET claimed_at = NULL
WHERE id = @id AND status = 'PENDING';

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

-- ClaimNotificationAnnouncement decides who tells the source, and it is a claim
-- rather than a record.
--
-- The callback goes out when the LAST delivery of a message settles, and two
-- workers settling the last two at the same moment both see a finished message:
-- without this, both would send the same batch, and the contract says a callback
-- is sent once and never retried.
--
-- One statement over the whole message, not one row at a time. Two of these
-- serialise: the first stamps every row and reports how many, the second
-- re-evaluates against what the first committed and reports none. A per-row
-- version would let two callers each win a subset and both believe they had it.
--
-- So notified_at means "an announcement was made for this outcome", not "the
-- source received it". That is the honest reading: the callback is best effort
-- and never retried by design, so the attempt is the event -- and whether it
-- landed is recorded on the webhook, in consecutive_failures.
--
-- name: ClaimNotificationAnnouncement :execrows
UPDATE deliveries
SET notified_at = @notified_at::timestamptz
WHERE notification_id = @notification_id AND notified_at IS NULL;
