-- +goose Up
-- +goose StatementBegin

-- Which sending identity a source uses on a channel.
--
-- The domain entity carries no secret and no provider settings: it would
-- otherwise have to know what SMTP is. The adapter reads the two columns below
-- by id at send time, which is also why they are here and not in their own
-- table -- one read instead of two.
CREATE TABLE credentials (
    id         ulid        PRIMARY KEY,
    source_id  ulid        NOT NULL REFERENCES sources (id) ON DELETE CASCADE,

    channel    TEXT        NOT NULL
                           CHECK (channel IN ('email', 'telegram', 'bale', 'whatsapp', 'matrix', 'fcm', 'apns', 'gotify')),

    -- What the source calls it: "marketing", "alerts". Chosen per message.
    name       TEXT        NOT NULL,

    -- Everything the provider needs that is not secret: smtp host and port, the
    -- from address, a chat id. Shapeless on purpose -- a second provider is not
    -- a schema change.
    config     JSONB       NOT NULL DEFAULT '{}'::jsonb,

    -- Encrypted, not hashed: a bot token has to be handed to Telegram, so it
    -- must come back out. The value names the key that encrypted it --
    -- v1.<key id>.<nonce>.<ciphertext> -- so the key can be changed without an
    -- outage. See docs/ARCHITECTURE.md.
    secret     TEXT,

    is_default BOOLEAN     NOT NULL DEFAULT FALSE,
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- A source picks a credential by name, so two with one name on one channel
-- would make the choice ambiguous.
CREATE UNIQUE INDEX credentials_source_channel_name_key
    ON credentials (source_id, channel, name);

-- Only one default per channel. This cannot be enforced inside a single entity
-- -- it is a rule about the set -- so the database holds it.
CREATE UNIQUE INDEX credentials_one_default_per_channel
    ON credentials (source_id, channel)
    WHERE is_default;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE credentials;
-- +goose StatementEnd
