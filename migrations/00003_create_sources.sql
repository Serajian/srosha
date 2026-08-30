-- +goose Up
-- +goose StatementBegin

-- A source is whoever is allowed to send through us. Ids are ULIDs generated in
-- Go: char(26) rather than uuid because that is what shared.ID is, and because
-- a ULID sorts by creation time, which makes it its own cursor for pagination.
CREATE TABLE sources (
    id                   ulid        PRIMARY KEY,
    name                 TEXT        NOT NULL,

    -- What this source is for, in the customer's words. Distinct from name,
    -- which is a label: two sources both called "alerts" are told apart by
    -- this.
    --
    -- NOT NULL DEFAULT '' rather than nullable. An empty description and an
    -- absent one are the same thing to everybody who reads one, and a nullable
    -- column would make every reader handle a difference carrying no meaning.
    description          TEXT        NOT NULL DEFAULT '',

    -- The ceiling on what this source may ask for. A request above it is
    -- downgraded rather than refused, and both values are kept on the message.
    max_priority         TEXT        NOT NULL
                                     CHECK (max_priority IN ('NORMAL', 'HIGH', 'CRITICAL')),

    -- Who registered this. A customer sees their own sources and nobody
    -- else's, and this is the whole of how that is decided.
    owner_user_id        ulid        NOT NULL REFERENCES users (id),

    -- Whether this source may send. FALSE on creation: anybody may register a
    -- source, and nothing it registers reaches anybody until an operator says
    -- so. It is also the switch an operator uses later, which is why there is
    -- no second column for approval.
    is_active            BOOLEAN     NOT NULL DEFAULT FALSE,

    -- When it was first approved. A record, never a gate -- nothing reads this
    -- to decide anything. It exists so a review queue can ask for what has
    -- never been approved without also listing what somebody switched off last
    -- month.
    approved_at          TIMESTAMPTZ,

    -- False bounds the damage of a leaked key: the source can then only reach
    -- the addresses below, never a stranger.
    allow_custom_address BOOLEAN     NOT NULL DEFAULT FALSE,

    -- channel -> address. One per channel; reaching several people is a group
    -- chat or a mailing list, which the customer manages.
    default_addresses    JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

-- What a customer's own page reads, and what an operator's queue reads.
CREATE INDEX sources_owner_idx ON sources (owner_user_id, created_at DESC);
CREATE INDEX sources_unapproved_idx ON sources (created_at) WHERE approved_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sources;
-- +goose StatementEnd
