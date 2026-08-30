-- +goose Up
-- +goose StatementBegin

-- One accepted message. What it says, and to whom is in deliveries.
CREATE TABLE notifications (
    id                 ulid        PRIMARY KEY,
    -- No ON DELETE CASCADE, unlike the source's keys and settings. A source's
    -- message history is a record: cascading would let removing one customer
    -- row silently take everything ever sent for them. Deleting a source that
    -- has ever sent anything is refused, and deactivating is what to do
    -- instead.
    source_id          ulid        NOT NULL REFERENCES sources (id),

    -- The source's own key for this message, so a retried request does not send
    -- twice. Optional: a source that does not care may leave it out.
    idempotency_key    TEXT,

    -- A copy, not a join. A callback sent later must describe the source as it
    -- was when the message was accepted, even if it has since been renamed.
    source_name        TEXT        NOT NULL,

    title              TEXT        NOT NULL,
    body               TEXT        NOT NULL,

    -- Both kept, so the gateway can report a downgrade instead of hiding it:
    -- what was asked for, and what the source's ceiling allowed.
    requested_priority TEXT        NOT NULL
                                   CHECK (requested_priority IN ('NORMAL', 'HIGH', 'CRITICAL')),
    effective_priority TEXT        NOT NULL
                                   CHECK (effective_priority IN ('NORMAL', 'HIGH', 'CRITICAL')),

    -- Null means no deadline. Past it, a delivery that has not gone yet fails
    -- as EXPIRED rather than arriving too late to be worth anything.
    expire_at          TIMESTAMPTZ,

    -- The source's own, returned untouched on the callback. We never read it.
    metadata           JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at         TIMESTAMPTZ NOT NULL
);

-- The idempotency guard. Partial, because a source that sends no key must not
-- be limited to one message.
CREATE UNIQUE INDEX notifications_source_idempotency_key
    ON notifications (source_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Listing a source's messages, newest first. The id is a ULID, so ordering by
-- it is ordering by time and needs no second column.
CREATE INDEX notifications_source_id_id_idx ON notifications (source_id, id DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE notifications;
-- +goose StatementEnd
