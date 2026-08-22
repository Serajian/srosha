-- +goose Up
-- +goose StatementBegin

-- Authentication is a lookup, not a comparison: there is no username to find
-- the row by, so the hash of the key is what the row is found by. See
-- docs/ARCHITECTURE.md.
--
-- Their own table rather than a column on sources, so a source can hold two
-- keys at once and rotate without a window where its messages are refused.
CREATE TABLE api_keys (
    id           ulid        PRIMARY KEY,
    source_id    ulid        NOT NULL REFERENCES sources (id) ON DELETE CASCADE,

    -- SHA-256 of the key we issued. The key itself is shown once and never
    -- stored: bcrypt and argon2 are for low-entropy human passwords, and their
    -- per-row salt would turn this lookup into a full scan.
    key_hash     TEXT        NOT NULL,

    -- Which key this is, for whoever has to revoke one: "production", "ci".
    label        TEXT        NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,

    -- Marked, never deleted. After an incident the questions are when it was
    -- revoked and when it was last used, and a deleted row answers neither.
    revoked_at   TIMESTAMPTZ
);

-- Unique rather than a plain index: the same hash reaching two sources would
-- make authentication ambiguous, and this refuses it at write time.
CREATE UNIQUE INDEX api_keys_key_hash_key ON api_keys (key_hash);

CREATE INDEX api_keys_source_id_idx ON api_keys (source_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE api_keys;
-- +goose StatementEnd
