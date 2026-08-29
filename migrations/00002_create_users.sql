-- +goose Up
-- +goose StatementBegin

-- A person. Customers and operators are the same row with a different role:
-- two account tables would mean two sign-in flows and two sets of bugs.
--
-- There is no password column and never will be. Sign-in is a one-time code,
-- which is also what lets the first operator be written by hand -- an argon2
-- hash cannot be typed into SQL, and an email can.
CREATE TABLE users (
    id         ulid        PRIMARY KEY,

    -- Lowercased before it is stored, so two spellings of one address are one
    -- account.
    email      TEXT        NOT NULL UNIQUE,

    role       TEXT        NOT NULL
                           CHECK (role IN ('customer', 'admin', 'super_admin')),

    -- Whether this person may SIGN IN. It says nothing about whether their
    -- sources may send: those are opposite questions -- a customer who has not
    -- paid must still be able to sign in, or they cannot pay.
    is_active  BOOLEAN     NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE users;
-- +goose StatementEnd
