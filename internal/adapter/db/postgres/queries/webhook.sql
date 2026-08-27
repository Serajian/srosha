-- is_active and consecutive_failures are not given: a new callback is switched
-- on and has failed at nothing, and the column defaults say so.
--
-- name: CreateWebhook :exec
INSERT INTO webhooks (id, source_id, callback_url, created_at, updated_at)
VALUES (@id, @source_id, @callback_url, @created_at, @created_at);

-- name: ReadWebhookBySourceID :one
SELECT * FROM webhooks WHERE source_id = @source_id;

-- There is no single UpdateWebhook, on purpose. Two very different callers write
-- this row -- the dispatcher after every callback, and the source through the
-- API -- and a statement that writes every column lets each undo the other:
--
--   the source redirects to a new url
--   a callback that was already in flight succeeds
--   it writes the whole row back, old url included
--
-- Each statement below writes only what its caller meant to change.

-- RecordWebhookFailure counts in SQL rather than in Go. Read-modify-write loses
-- increments when several callbacks for the same source settle at once, and this
-- is the counter that decides when a dead endpoint is switched off -- losing them
-- means it is switched off far later than configured, or never.
--
-- The new count comes back so the caller can compare it against the configured
-- limit and switch the callback off if it has been reached. That comparison
-- stays in the domain, which owns the rule and has tests for it; putting it in
-- this statement would make the limit a second source of truth that nothing
-- keeps in step.
--
-- name: RecordWebhookFailure :one
UPDATE webhooks
SET consecutive_failures = consecutive_failures + 1,
    updated_at           = @updated_at::timestamptz
WHERE id = @id
RETURNING consecutive_failures;

-- One success clears the run, so an endpoint that fails now and then is never
-- switched off.
--
-- name: RecordWebhookSuccess :execrows
UPDATE webhooks
SET consecutive_failures = 0, updated_at = @updated_at::timestamptz
WHERE id = @id;

-- Redirect clears the failure run and switches the callback back on: a new
-- address has not failed at anything yet, and leaving it off would mean a source
-- fixing a broken endpoint saw nothing change.
--
-- name: RedirectWebhook :execrows
UPDATE webhooks
SET callback_url         = @callback_url,
    is_active            = TRUE,
    consecutive_failures = 0,
    updated_at           = @updated_at::timestamptz
WHERE id = @id;

-- Switching it on clears the run too. Without that it would be switched off
-- again by the first hiccup, having never been given a fresh start.
--
-- name: SetWebhookActive :execrows
UPDATE webhooks
SET is_active            = @is_active,
    consecutive_failures = CASE WHEN @is_active::boolean THEN 0 ELSE consecutive_failures END,
    updated_at           = @updated_at::timestamptz
WHERE id = @id AND is_active <> @is_active::boolean;

-- WriteWebhookSecret replaces the sealed signing secret.
--
-- Scoped by source as well as by id, so an id belonging to somebody else writes
-- nothing rather than overwriting their secret.
-- name: WriteWebhookSecret :execrows
UPDATE webhooks
SET secret     = @secret,
    updated_at = @updated_at
WHERE id = @id
  AND source_id = @source_id;

-- ReadWebhookSecret hands back the sealed secret and the row it belongs to.
--
-- The id comes with it because the seal is bound to both, so whoever opens it
-- needs the pair and must not have to make a second query for the half it is
-- missing.
-- name: ReadWebhookSecret :one
SELECT id, secret
FROM webhooks
WHERE source_id = @source_id;
