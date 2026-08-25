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

-- ListCredentialsBySource is what a source asks to see what it has registered.
-- Every channel, switched-off ones included: the answer to "what do I have" must
-- include the one somebody disabled, or nobody can turn it back on.
--
-- config and secret are NOT selected, for the same reason as above.
--
-- name: ListCredentialsBySource :many
SELECT id, source_id, channel, name, is_default, is_active, created_at, updated_at
FROM credentials
WHERE source_id = @source_id
ORDER BY channel, name;

-- ReadCredential finds one by id, scoped to its source.
--
-- The source_id is in the WHERE clause and that is not belt and braces: the id
-- arrives in a request body, so without it a source could name somebody else's
-- credential and read, disable or rotate it. Scoping here means the worst a
-- guessed id can do is find nothing.
--
-- name: ReadCredential :one
SELECT id, source_id, channel, name, is_default, is_active, created_at, updated_at
FROM credentials
WHERE id = @id AND source_id = @source_id;

-- SetCredentialActive switches one off or on.
--
-- Switching OFF also clears the default flag, because a default that cannot be
-- used leaves every message naming no identity failing with nothing to fix.
-- Switching ON does not restore it: which one is the default is a decision, and
-- guessing it back would silently move it.
--
-- name: SetCredentialActive :execrows
UPDATE credentials
SET is_active  = @is_active,
    is_default = CASE WHEN @is_active::boolean THEN is_default ELSE FALSE END,
    updated_at = @updated_at::timestamptz
WHERE id = @id AND source_id = @source_id AND is_active <> @is_active::boolean;

-- SetCredentialDefault is the other half of moving the default, and must run in
-- the same transaction as ClearDefaultCredential. The partial unique index
-- refuses two defaults, so without the clear this fails instead of taking over.
--
-- is_active is checked here rather than trusted from the read: the two happen at
-- different times, and a default that is switched off is the one state the
-- channel must never be left in.
--
-- name: SetCredentialDefault :execrows
UPDATE credentials
SET is_default = TRUE,
    updated_at = @updated_at::timestamptz
WHERE id = @id AND source_id = @source_id AND is_active;

-- RotateCredentialSecret replaces the secret and nothing else.
--
-- Distinct from ResealCredentialSecret above, which rewrites the same secret
-- under a newer key and matches on the old value to lose a race safely. This one
-- writes a DIFFERENT secret on purpose, so matching on the old value would make
-- it fail whenever a reseal had just run.
--
-- name: RotateCredentialSecret :execrows
UPDATE credentials
SET secret = @secret, updated_at = @updated_at::timestamptz
WHERE id = @id AND source_id = @source_id AND is_active;
