-- +goose Up
-- +goose StatementBegin

-- Where a source wants delivery outcomes pushed to. One per source.
--
-- The signing secret is NOT here. It never touches the database and is never
-- returned by the API: it is handed to the source out of band and lives in
-- NOTIF_WEBHOOK_SECRETS.
CREATE TABLE webhooks (
    id                   ulid        PRIMARY KEY,

    -- Unique, because one per source is the rule rather than a coincidence.
    source_id            ulid        NOT NULL UNIQUE
                                     REFERENCES sources (id) ON DELETE CASCADE,

    callback_url         TEXT        NOT NULL,

    is_active            BOOLEAN     NOT NULL DEFAULT TRUE,

    -- Reset by any success. Once it reaches the configured limit the webhook is
    -- switched off, so a dead endpoint is not called once per message forever.
    consecutive_failures INTEGER     NOT NULL DEFAULT 0,

    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE webhooks;
-- +goose StatementEnd
