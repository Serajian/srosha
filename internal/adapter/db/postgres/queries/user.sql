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
