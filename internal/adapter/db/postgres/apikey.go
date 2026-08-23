package postgres

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/adapter/db/postgres/gen"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKeyRepository implements source.KeyRepository.
//
// It never sees a key, only the hash of one. Minting and hashing are
// adapter/auth's, which is the single place that knows what a key looks like.
type APIKeyRepository struct{ base }

func NewAPIKeyRepository(pool *pgxpool.Pool) *APIKeyRepository {
	return &APIKeyRepository{base{pool: pool}}
}

// Create stores a key that adapter/auth has just minted. The hash is passed
// beside the entity because source.Key deliberately has no field for it.
func (r *APIKeyRepository) Create(ctx context.Context, k *source.Key, keyHash string) error {
	err := r.q(ctx).CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		ID:        k.ID.String(),
		SourceID:  k.SourceID,
		KeyHash:   keyHash,
		Label:     k.Label,
		CreatedAt: k.CreatedAt,
		ExpiresAt: k.ExpiresAt,
	})
	switch {
	// Two sources holding one hash would make authentication ambiguous, so the
	// index refuses it. Reaching this is a bug in whoever generated the key,
	// not a customer's mistake -- but it is reported rather than swallowed,
	// because the alternative is a key that silently was not issued.
	case violates(err, "api_keys_key_hash_key"):
		return errs.DuplicateErr("that key already exists").WithStr(k.SourceID)
	case violates(err, "api_keys_pkey"):
		return errs.DuplicateErr("key already exists").WithStr(k.ID.String())
	case err != nil:
		return failed("create api key", err)
	}
	return nil
}

// ReadSourceByKeyHash is the whole of authentication, and one round trip: the
// join brings the source back with the key, because this runs on every request.
//
// No row means no live key, and the caller is told nothing more. Revoked,
// expired and never-existed are all excluded inside the statement, so they
// arrive here as the same silence -- which is the point. Answering them
// differently would tell whoever is guessing which of their strings was real.
func (r *APIKeyRepository) ReadSourceByKeyHash(
	ctx context.Context, keyHash string, now time.Time,
) (*source.Source, shared.ID, error) {
	row, err := r.q(ctx).ReadSourceByKeyHash(ctx, gen.ReadSourceByKeyHashParams{
		KeyHash: keyHash,
		Now:     now,
	})
	switch {
	case noRows(err):
		return nil, "", nil
	case err != nil:
		return nil, "", failed("read source by key hash", err)
	}

	src, err := toSource(row.Source)
	if err != nil {
		return nil, "", err
	}
	return src, shared.ID(row.ApiKeyID), nil
}

// ListBySourceID is what makes rotation possible: issue the second key, let the
// callers move, revoke the first. Revoked keys come back too, because after an
// incident the row being asked about is exactly the revoked one.
func (r *APIKeyRepository) ListBySourceID(
	ctx context.Context, sourceID string,
) ([]source.Key, error) {
	rows, err := r.q(ctx).ListAPIKeysBySource(ctx, sourceID)
	if err != nil {
		return nil, failed("list api keys", err)
	}

	out := make([]source.Key, 0, len(rows))
	for _, row := range rows {
		out = append(out, toKey(row))
	}
	return out, nil
}

// Touch records that a key is in use, at most once per notUsedFor window.
//
// Zero rows is success, and is in fact the usual outcome: it means the key was
// already touched inside the window. This is bookkeeping, and the caller has
// been told to log a failure and let the request through.
func (r *APIKeyRepository) Touch(
	ctx context.Context, keyID shared.ID, now time.Time, notUsedFor time.Duration,
) error {
	_, err := r.q(ctx).TouchAPIKey(ctx, gen.TouchAPIKeyParams{
		ID:          keyID.String(),
		Now:         now,
		StaleBefore: now.Add(-notUsedFor),
	})
	if err != nil {
		return failed("touch api key", err)
	}
	return nil
}

// Revoke stops a key working from this moment: the authentication statement
// excludes it. The row stays and key_hash is not cleared -- we never held the
// key, only its hash, and keeping it is what lets us answer later whether a
// leaked string was ever ours.
//
// Zero rows here is NOT success, unlike Touch. Revoking is something an
// operator does once, deliberately, and the three cases it distinguishes --
// revoked now, revoked already, never existed -- are exactly what they need to
// see.
func (r *APIKeyRepository) Revoke(ctx context.Context, keyID shared.ID, now time.Time) error {
	rows, err := r.q(ctx).RevokeAPIKey(ctx, gen.RevokeAPIKeyParams{
		ID:        keyID.String(),
		RevokedAt: now,
	})
	if err != nil {
		return failed("revoke api key", err)
	}
	if rows == 0 {
		return errs.NotFoundErr("no key to revoke").WithStr(keyID.String())
	}
	return nil
}

// --- mapping -----------------------------------------------------------------

// toKey needs no error path: the row carries no enum and no json, and the hash
// -- the one column that would need care -- is not selected.
func toKey(row gen.ListAPIKeysBySourceRow) source.Key {
	return source.Key{
		ID:         shared.ID(row.ID),
		SourceID:   row.SourceID,
		Label:      row.Label,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt,
		ExpiresAt:  row.ExpiresAt,
		RevokedAt:  row.RevokedAt,
	}
}
