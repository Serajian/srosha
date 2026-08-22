-- +goose Up
-- +goose StatementBegin

-- A source is whoever is allowed to send through us. Ids are ULIDs generated in
-- Go: char(26) rather than uuid because that is what shared.ID is, and because
-- a ULID sorts by creation time, which makes it its own cursor for pagination.
CREATE TABLE sources (
    id                   ulid        PRIMARY KEY,
    name                 TEXT        NOT NULL,

    -- The ceiling on what this source may ask for. A request above it is
    -- downgraded rather than refused, and both values are kept on the message.
    max_priority         TEXT        NOT NULL
                                     CHECK (max_priority IN ('NORMAL', 'HIGH', 'CRITICAL')),

    is_active            BOOLEAN     NOT NULL DEFAULT TRUE,

    -- False bounds the damage of a leaked key: the source can then only reach
    -- the addresses below, never a stranger.
    allow_custom_address BOOLEAN     NOT NULL DEFAULT FALSE,

    -- channel -> address. One per channel; reaching several people is a group
    -- chat or a mailing list, which the customer manages.
    default_addresses    JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sources;
-- +goose StatementEnd
