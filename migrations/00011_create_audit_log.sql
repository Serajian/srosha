-- +goose Up
-- +goose StatementBegin

-- Who did what, and when.
--
-- Append only. Never updated, never deleted: a record somebody can tidy shows
-- only what nobody wanted to hide.
--
-- It exists from the first day because per-person accounts were chosen so that
-- "who created this source" and "who revoked that key" have answers, and
-- accounts with no record answer neither.
CREATE TABLE audit_log (
    id          ulid        PRIMARY KEY,

    at          TIMESTAMPTZ NOT NULL,

    -- The actor's id AND their address at the time. The address is copied
    -- rather than joined because it is what somebody reading this a year later
    -- needs, and the row it came from may since have been changed.
    actor_id    ulid        NOT NULL REFERENCES users (id),
    actor_email TEXT        NOT NULL,

    -- "source.create", "key.revoke". A verb, not a sentence.
    verb        TEXT        NOT NULL,

    target_type TEXT        NOT NULL,
    target_id   TEXT        NOT NULL,

    -- Why, when the verb does not say it on its own. A copy rather than a
    -- join, for the same reason actor_email is one: sources.review_note is
    -- overwritten by the next decision, so a year later the reason for the
    -- first refusal would be gone.
    note        TEXT        NOT NULL DEFAULT ''
);

-- What an investigation reads: everything one person did, newest first.
CREATE INDEX audit_log_actor_at_idx ON audit_log (actor_id, at DESC);

-- And everything that happened to one thing.
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, at DESC);

-- And the only read the panel actually makes: the newest N rows, whoever did
-- them. Neither index above answers it -- both lead with a column this query
-- does not filter on, so it is a full scan and a sort of a table that is
-- append-only, never swept, and grows by a row per customer action.
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE audit_log;
-- +goose StatementEnd
