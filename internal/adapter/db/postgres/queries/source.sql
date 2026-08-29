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

-- UpdateSource writes what changes over a customer's life. is_active is not
-- among them: switching a source off is its own act, and folding it in here
-- would mean every rename had to carry the current state or flip it by accident.
--
-- default_addresses is written whole, because it is one jsonb value. Two
-- requests changing two different channels at the same time will therefore have
-- one overwrite the other. That is tolerable while it is a map of one value per
-- channel and edited by hand; the day an entry needs a second field it becomes
-- its own table, and this stops being true. See the migration change report.
--
-- execrows so the caller can tell "updated" from "no such source".
--
-- name: UpdateSource :execrows
UPDATE sources
SET name                 = @name,
    max_priority         = @max_priority,
    allow_custom_address = @allow_custom_address,
    default_addresses    = @default_addresses,
    updated_at           = @updated_at::timestamptz
WHERE id = @id;

-- Suspending a source stops it sending without deleting anything: its messages,
-- keys and credentials all stay, and turning it back on is one statement rather
-- than a re-registration.
--
-- Guarded, so suspending an already suspended source reports zero rows instead
-- of moving updated_at and looking like something happened.
--
-- name: DeactivateSource :execrows
UPDATE sources
SET is_active = FALSE, updated_at = @updated_at::timestamptz
WHERE id = @id AND is_active;

-- name: ActivateSource :execrows
UPDATE sources
SET is_active = TRUE, updated_at = @updated_at::timestamptz
WHERE id = @id AND NOT is_active;
