-- +goose Up
-- +goose StatementBegin

-- A code somebody was sent, and what has happened to it since.
--
-- The code is stored as it was sent, not hashed. For six digits that would be
-- theater: a million values invert instantly, and whoever holds this database
-- does not need a code at all -- they can write a session row. What protects it
-- is the three columns below: a short life, one use, and a guess limit.
CREATE TABLE login_codes (
    id         ulid        PRIMARY KEY,

    user_id    ulid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    code       TEXT        NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    -- Wrong answers so far. Past the limit the code is dead, whatever is left
    -- of its life: six digits is a million tries a script exhausts in seconds.
    attempts   INTEGER     NOT NULL DEFAULT 0,

    -- Set by the first attempt that spends it, right or wrong.
    used_at    TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL
);

-- What sign-in reads: this person's newest code. Also what the request limit
-- counts over.
CREATE INDEX login_codes_user_created_idx
    ON login_codes (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE login_codes;
-- +goose StatementEnd
