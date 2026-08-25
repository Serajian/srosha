-- exec, not one: the ports return only an error and nothing here is computed by
-- the database -- no trigger, no default we rely on -- so a returned row would
-- be read by nobody.
--
-- name: CreateCredential :exec
INSERT INTO credentials (
    id, source_id, channel, name, config, secret,
    is_default, is_active, created_at, updated_at
) VALUES (
    @id, @source_id, @channel, @name, @config, @secret,
    @is_default, TRUE, @created_at, @created_at
);

-- The core asks for the whole set on a channel and picks from it itself, so that
-- "which one is the default" stays a rule about the set rather than a query.
--
-- Inactive ones are returned too, and that is the point. Filtering them here
-- would turn "that identity is switched off" into "no identity by that name" --
-- Pick has a branch for the first and could never reach it, and the source would
-- be told to check its spelling for a credential it had deliberately disabled.
--
-- config and secret are NOT selected. The domain type holds neither, so they
-- would be read from disk and thrown away on the path that only chooses which
-- identity to send with. ReadCredentialSecret below fetches them at the moment
-- they are used, which is the only moment a secret should be in memory.
--
-- name: ListCredentialsBySourceAndChannel :many
SELECT id, source_id, channel, name, is_default, is_active, created_at, updated_at
FROM credentials
WHERE source_id = @source_id AND channel = @channel
ORDER BY id;

-- ReadCredentialSecret is the sender registry's, and nothing else's: it is
-- called once a credential has already been chosen and a message is about to
-- go out. is_active is checked again here rather than trusted from the earlier
-- read, because the two happen at different times.
--
-- name: ReadCredentialSecret :one
SELECT config, secret
FROM credentials
WHERE id = @id AND is_active;

-- ClearDefaultCredential is half of moving the default, and must run in the same
-- transaction as the other half. The partial unique index refuses two defaults,
-- so without this the new one fails to write instead of taking over -- but if
-- this runs and the other half does not, the channel is left with no default at
-- all and every message that names no credential fails.
--
-- name: ClearDefaultCredential :execrows
UPDATE credentials
SET is_default = FALSE, updated_at = @updated_at::timestamptz
WHERE source_id = @source_id AND channel = @channel AND is_default;

-- ResealCredentialSecret writes the secret alone, and is the only statement that
-- ever rewrites one. It exists for key rotation: the stored value names the key
-- that sealed it, so a value naming an old key is resealed with the current one
-- the next time it is read.
--
-- The old value is in the WHERE clause, so a reseal that lost a race writes
-- nothing rather than overwriting whatever the winner put there. Both would have
-- sealed the same plaintext, but only one of them may be the row.
--
-- Nothing else is touched -- not config, not the name, not the flags. The same
-- rule the webhook statements keep: a method writes what its caller meant to
-- change and nothing more.
--
-- name: ResealCredentialSecret :execrows
UPDATE credentials
SET secret = @secret, updated_at = @updated_at::timestamptz
WHERE id = @id AND secret = @previous;
