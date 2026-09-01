-- name: CreateLoginCode :exec
INSERT INTO login_codes (id, user_id, code, expires_at, attempts, used_at, created_at)
VALUES (@id, @user_id, @code, @expires_at, @attempts, @used_at, @created_at);

-- ReadNewestLoginCode is the only one ever checked: asking for another code is
-- what puts the previous one out of reach.
-- name: ReadNewestLoginCode :one
SELECT id, user_id, code, expires_at, attempts, used_at, created_at
FROM login_codes
WHERE user_id = @user_id
ORDER BY created_at DESC
LIMIT 1;

-- name: SpendLoginCode :execrows
UPDATE login_codes
SET attempts = @attempts,
    used_at  = @used_at
WHERE id = @id;

-- name: CountLoginCodesSince :one
SELECT count(*)
FROM login_codes
WHERE user_id = @user_id
  AND created_at >= @since;

-- name: ForgetLoginCode :exec
-- Removes a code that was stored and then never sent. Deleted rather than
-- marked: a code that reached nobody is not history, and the request limit
-- counts rows.
DELETE FROM login_codes
WHERE id = @id;
