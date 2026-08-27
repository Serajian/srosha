-- +goose Up
-- +goose StatementBegin

-- One message to one recipient on one channel. This table IS the outbox: a row
-- in PENDING says exactly what an outbox row would -- this must be sent and has
-- not been -- so a second table would only be able to disagree with it. See
-- docs/ARCHITECTURE.md.
CREATE TABLE deliveries (
    id                  ulid        PRIMARY KEY,
    notification_id     ulid        NOT NULL
                                    REFERENCES notifications (id) ON DELETE CASCADE,

    channel             TEXT        NOT NULL
                                    CHECK (channel IN ('email', 'telegram', 'bale', 'whatsapp', 'matrix', 'fcm', 'apns')),
    address             TEXT        NOT NULL,

    -- Which credential to send with, resolved when the message was accepted so
    -- that changing a default later does not rewrite history.
    sender_name         TEXT        NOT NULL,

    status              TEXT        NOT NULL DEFAULT 'PENDING'
                                    CHECK (status IN ('PENDING', 'SENT', 'FAILED')),

    attempts            INTEGER     NOT NULL DEFAULT 0,

    -- Kept for us, not for the source: it can name internal hosts, so it is
    -- sanitised before it reaches anyone outside.
    last_error          TEXT,

    -- NOT_REACHABLE is the provider refusing the RECIPIENT rather than the
    -- message: somebody who blocked us, a conversation window that closed, a
    -- device token no longer registered. Apart from PERMANENT because the source
    -- can act on it and cannot act on the other.
    failure_reason      TEXT        CHECK (failure_reason IS NULL OR failure_reason IN
                                    ('EXPIRED', 'MAX_ATTEMPTS', 'PERMANENT', 'NO_SENDER',
                                     'NOT_REACHABLE')),

    -- What the provider called it, so a support question can be traced into
    -- their system rather than stopping at ours.
    provider_message_id TEXT,

    notified_at         TIMESTAMPTZ,

    -- Which dispatcher is dealing with this row, and since when.
    --
    -- Recovery SENDS rather than republishing, so the broker's duplicate window
    -- never sees these rows: two dispatchers sweeping at once would both send,
    -- and somebody would get the message twice. Taking a row exclusively is what
    -- stops that, and this is where the claim lives once the lock is gone.
    --
    -- Deliberately NOT updated_at. Age is the retry counter -- every failed
    -- attempt leaves the row a little older, and RECONCILE_GIVE_UP reads that
    -- age -- so claiming by touching it would mean the row never gives up.
    claimed_at          TIMESTAMPTZ,

    -- created_at answers how long a delivery waited, which updated_at stops
    -- being able to answer the moment the row moves.
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);

-- The duplicate guard. Asking for the same person on the same channel twice in
-- one message is one delivery, not two.
CREATE UNIQUE INDEX deliveries_notification_channel_address_key
    ON deliveries (notification_id, channel, address);

-- What recovery scans: rows that have been PENDING too long. Without it that is
-- a sequential scan of every delivery ever sent, on a timer.
-- The sweep asks for pending rows past a cutoff whose claim is absent or
-- expired, so all three are in one index. Partial, because everything else in
-- this table is settled and will never match.
CREATE INDEX deliveries_pending_sweep_idx
    ON deliveries (updated_at, claimed_at)
    WHERE status = 'PENDING';

CREATE INDEX deliveries_notification_id_idx ON deliveries (notification_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE deliveries;
-- +goose StatementEnd
