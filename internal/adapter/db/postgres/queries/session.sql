-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, expires_at, last_seen_at, created_at)
VALUES (@id, @user_id, @expires_at, @last_seen_at, @created_at);

-- name: ReadSession :one
SELECT id, user_id, expires_at, last_seen_at, created_at
FROM sessions
WHERE id = @id;

-- name: TouchSession :execrows
UPDATE sessions
SET last_seen_at = @last_seen_at
WHERE id = @id;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = @id;
