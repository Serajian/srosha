-- ReadSourceByKeyHash is the whole of authentication: the key is both the
-- identifier and the secret, so its hash is what the row is found by.
--
-- The join is what keeps this one round trip. The key row holds only source_id,
-- and this runs on every request, so looking the source up separately would
-- double the cost of the hottest path in the service. sqlc.embed keeps the
-- source's columns in a nested struct rather than flattened in beside the key's,
-- so the mapper is the same one every other source query uses.
--
-- Expiry and revocation are in the WHERE rather than checked afterwards: a
-- revoked key must be indistinguishable from one that never existed, or the
-- answer tells whoever is guessing that it once was real.
--
-- now comes from the caller, not from now(): the service has a clock port so
-- that time is injectable, and the one place that could not be tested without
-- moving the database's clock should not be this one.
--
-- name: ReadSourceByKeyHash :one
SELECT sqlc.embed(s), k.id AS api_key_id
FROM api_keys k
JOIN sources s ON s.id = k.source_id
WHERE k.key_hash = @key_hash
  AND k.revoked_at IS NULL
  -- Cast so sqlc knows the parameter is a non-null timestamp; a bare @now is
  -- assumed nullable and comes out as *time.Time.
  AND (k.expires_at IS NULL OR k.expires_at > @now::timestamptz);

-- exec, not one: the ports return only an error and nothing here is computed by
-- the database -- no trigger, no default we rely on -- so a returned row would
-- be read by nobody.
--
-- name: CreateAPIKey :exec
INSERT INTO api_keys (id, source_id, key_hash, label, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- TouchAPIKey records that a key is in use, at most once per stale_before
-- window. Writing on every request would put an UPDATE on the hottest path;
-- never writing at all would leave last_used_at permanently null and the
-- question it exists to answer -- when was this key last used -- unanswerable.
--
-- Accuracy to within the window is enough for that question.
--
-- name: TouchAPIKey :execrows
UPDATE api_keys
SET last_used_at = @now::timestamptz
WHERE id = @id
  AND (last_used_at IS NULL OR last_used_at < @stale_before::timestamptz);

-- RevokeAPIKey stops a key working from the moment revoked_at is set: the
-- authentication query above excludes it. The row itself stays, and key_hash is
-- not cleared -- we never held the key, only its hash, and keeping it is what
-- lets us answer later whether a leaked key was ever ours.
--
-- execrows rather than exec so the caller can tell three cases apart that
-- otherwise all look like success: revoked now, revoked already, never existed.
--
-- name: RevokeAPIKey :execrows
UPDATE api_keys
SET revoked_at = @revoked_at::timestamptz
WHERE id = @id AND revoked_at IS NULL;
