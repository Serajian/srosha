-- +goose Up
-- +goose StatementBegin

-- A signed-in browser.
--
-- Kept here rather than only in a signed cookie, so that deactivating somebody
-- ends their session on the next request. A self-contained token would keep
-- working until it expired, which is the wrong answer to "this person left".
CREATE TABLE sessions (
    id           ulid        PRIMARY KEY,

    user_id      ulid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    -- The absolute deadline. A session ends here however busy it has been.
    expires_at   TIMESTAMPTZ NOT NULL,

    -- Moved on every request, and what the idle timeout is measured from.
    last_seen_at TIMESTAMPTZ NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
-- +goose StatementEnd
