-- name: CreateUser :exec
INSERT INTO users (id, email, role, is_active, created_at, updated_at)
VALUES (@id, @email, @role, @is_active, @created_at, @updated_at);

-- ReadUserByEmail is the sign-in lookup. The address is already lowercased by
-- the caller, so this matches exactly and uses the unique index.
-- name: ReadUserByEmail :one
SELECT id, email, role, is_active, created_at, updated_at
FROM users
WHERE email = @email;

-- name: ReadUserByID :one
SELECT id, email, role, is_active, created_at, updated_at
FROM users
WHERE id = @id;

-- ListUsers is every account, newest first, for the page that manages them.
-- No filter anywhere, unlike ListAllSources: /sources has four states an
-- operator flips between and /people has none to flip between -- a role and
-- an is_active flag are shown on the row, and the page is short enough to
-- read. When it stops being short the answer is a page cursor, not a filter.
-- Capped at row_limit -- see usecase.Operators.People.
-- name: ListUsers :many
SELECT id, email, role, is_active, created_at, updated_at
FROM users
ORDER BY created_at DESC
LIMIT @row_limit;

-- UpdateUserRole writes the one field a person cannot set for themselves, and
-- nothing else -- a role change must not be able to carry a reactivation along
-- with it.
--
-- execrows so the caller can tell "updated" from "no such user".
-- name: UpdateUserRole :execrows
UPDATE users
SET role = @role, updated_at = @updated_at::timestamptz
WHERE id = @id;

-- SetUserActive writes whether somebody may sign in, and nothing a role change
-- touches.
--
-- execrows so the caller can tell "updated" from "no such user".
-- name: SetUserActive :execrows
UPDATE users
SET is_active = @is_active, updated_at = @updated_at::timestamptz
WHERE id = @id;
