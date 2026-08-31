-- is_active is not given: a source is created switched OFF, and the column
-- default says so. Anybody may register one; nothing it registers reaches
-- anybody until an operator approves it. allow_custom_address is a parameter because it genuinely is a
-- per-customer decision taken at registration -- with it off, a leaked key can
-- only reach that customer's own addresses.
--
-- exec, not one: the ports return only an error and nothing here is computed by
-- the database, so a returned row would be read by nobody.
--
-- name: CreateSource :exec
INSERT INTO sources (
    id, owner_user_id, name, max_priority, allow_custom_address,
    default_addresses, created_at, updated_at
) VALUES (
    @id, @owner_user_id, @name, @max_priority, @allow_custom_address,
    @default_addresses, @created_at, @created_at
);

-- ListSourcesByOwner is a customer's own page. Newest first, because the one
-- they just registered is the one they are looking for.
-- name: ListSourcesByOwner :many
SELECT * FROM sources WHERE owner_user_id = @owner_user_id ORDER BY created_at DESC;

-- ReadSource deliberately does not filter on is_active. A suspended source must
-- come back as a row so the domain's EnsureActive can say "source is not
-- active"; filtering here would turn that into "no such source" and send the
-- customer looking for a typo in an id that is perfectly correct.
--
-- name: ReadSource :one
SELECT * FROM sources WHERE id = @id;

-- UpdateSourceSettings writes the three columns a customer owns, and cannot
-- name the others.
--
-- There is no statement that writes them all. There was one -- UpdateSource,
-- which carried max_priority and allow_custom_address too -- deleted with
-- DeactivateSource and ActivateSource once nothing in production called any of
-- the three. Keeping those columns out of this statement is a cheaper
-- guarantee than a use case that remembers to re-read and re-send them, because
-- the statement cannot be broken by an edit somewhere else. A source moves in
-- exactly two ways now: this, and UpdateReview.
--
-- default_addresses is written whole, with the same caveat ListSourcesByOwner
-- carries: two edits to two different channels at once will have one overwrite
-- the other.
--
-- execrows so the caller can tell "updated" from "no such source".
--
-- name: UpdateSourceSettings :execrows
UPDATE sources
SET name              = @name,
    description       = @description,
    default_addresses = @default_addresses,
    updated_at        = @updated_at::timestamptz
WHERE id = @id;

-- UpdateReview writes an operator's decision and nothing a customer owns. The
-- mirror of UpdateSourceSettings: that one cannot touch the switch, this one
-- cannot touch the name.
--
-- name: UpdateReview :execrows
UPDATE sources
SET is_active   = @is_active,
    approved_at = @approved_at,
    reviewed_at = @reviewed_at,
    review_note = @review_note,
    updated_at  = @updated_at::timestamptz
WHERE id = @id;

-- ListForReview is the queue: what nobody has decided about, oldest first,
-- because the person who has waited longest is the one to answer next.
-- Capped at row_limit -- see usecase.Operators.Queue, which asks for one more
-- than it means to show so it can tell "that is everything" from "that is
-- all that fit".
--
-- name: ListForReview :many
SELECT * FROM sources WHERE reviewed_at IS NULL ORDER BY created_at LIMIT @row_limit;

-- ListAllSources is every source, newest first. No filter: the operator's page
-- filters in the handler, because the states are four and the counts are small.
-- The handler is web.reviewHandler.list, and the four states are inState
-- beside it -- an operator flips between them on one screen, and a round trip
-- per flip buys nothing on a set one person reads by eye. Capped at row_limit,
-- the same way and for the same reason as ListForReview above.
--
-- name: ListAllSources :many
SELECT * FROM sources ORDER BY created_at DESC LIMIT @row_limit;
