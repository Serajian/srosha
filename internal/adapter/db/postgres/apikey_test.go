//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/adapter/auth"
	"github.com/Serajian/srosha/internal/adapter/db/postgres"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// The authentication path is defined by this interface, and a repository that
// drifts from it stops the gateway compiling rather than failing at runtime.
var _ source.KeyRepository = (*postgres.APIKeyRepository)(nil)

// Scheme holds nothing, so one is enough for the whole file.
var scheme = auth.NewScheme()

// issued mints a key, stores it, and hands back both halves: the string a
// customer would present, and the row we kept.
func issued(
	t *testing.T, repo *postgres.APIKeyRepository, id, sourceID, label string, expires *time.Time,
) (presented string, k *source.Key) {
	t.Helper()

	presented, hash, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	k, err = source.NewKey(shared.ID(ulid(id)), sourceID, label, expires, keyNow())
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if err := repo.Create(context.Background(), k, hash); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return presented, k
}

func keyNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func TestAKeyAuthenticatesItsSource(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "K0")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	presented, k := issued(t, repo, "K1", sourceID, "production", nil)

	got, keyID, err := repo.ReadSourceByKeyHash(ctx, scheme.Hash(presented), keyNow())
	if err != nil {
		t.Fatalf("ReadSourceByKeyHash: %v", err)
	}
	if got == nil {
		t.Fatal("our own key did not authenticate")
	}
	if got.ID != sourceID || got.Name != "Acme" {
		t.Errorf("the join brought back the wrong source: %+v", got)
	}
	if keyID != k.ID {
		t.Errorf("key id = %q, want %q", keyID, k.ID)
	}
}

// Every way of not being a live key is the same silence here, because the
// statement excludes all three inside its WHERE clause.
func TestNoLiveKeyIsANilSourceRatherThanAnError(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "K2")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	now := keyNow()
	past := now.Add(-time.Hour)

	// Issued two hours ago with an expiry an hour ago: NewKey refuses an expiry
	// that has already passed, so the only honest way to hold an expired key is
	// to have created it before it expired.
	expiredKey, expiredHash, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	expired, err := source.NewKey(
		shared.ID(ulid("K3")), sourceID, "old", &past, past.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if err := repo.Create(ctx, expired, expiredHash); err != nil {
		t.Fatalf("Create: %v", err)
	}

	revokedKey, revoked := issued(t, repo, "K4", sourceID, "leaked", nil)
	if err := repo.Revoke(ctx, revoked.ID, now); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	unknown, _, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	tests := []struct {
		name      string
		presented string
	}{
		{"never issued", unknown},
		{"expired", expiredKey},
		{"revoked", revokedKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, keyID, err := repo.ReadSourceByKeyHash(ctx, scheme.Hash(tt.presented), now)
			if err != nil {
				t.Fatalf("ReadSourceByKeyHash() error = %v, want a nil source", err)
			}
			if got != nil {
				t.Errorf("it authenticated: %+v", got)
			}
			if keyID != "" {
				t.Errorf("key id = %q, want empty", keyID)
			}
		})
	}

	// The expired one is still a row -- it was never deleted.
	if expired.ExpiresAt == nil {
		t.Error("the test did not actually store an expiry")
	}
}

// A suspended source still authenticates. Refusing it here would report
// "invalid credentials" for a perfectly good key, and the domain's EnsureActive
// is what says the account is off.
func TestASuspendedSourceStillAuthenticates(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "K5")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	presented, _ := issued(t, repo, "K6", sourceID, "production", nil)
	if err := postgres.NewSourceRepository(pool).Deactivate(ctx, sourceID, keyNow()); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	got, _, err := repo.ReadSourceByKeyHash(ctx, scheme.Hash(presented), keyNow())
	if err != nil {
		t.Fatalf("ReadSourceByKeyHash: %v", err)
	}
	if got == nil {
		t.Fatal("a suspended source came back as no source at all")
	}
	if got.IsActive {
		t.Error("it came back active")
	}
	if err := got.EnsureActive(); err == nil {
		t.Error("EnsureActive accepted a suspended source")
	}
}

// Two sources holding one hash would make authentication ambiguous, so the
// unique index refuses it rather than letting a key work for two customers.
func TestOneHashCannotReachTwoSources(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	first := withASource(t, pool, "K7")
	second := withASource(t, pool, "K8")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	_, hash, err := scheme.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	for _, tc := range []struct{ id, sourceID string }{{"K9", first}, {"KA", second}} {
		k, err := source.NewKey(shared.ID(ulid(tc.id)), tc.sourceID, "production", nil, keyNow())
		if err != nil {
			t.Fatalf("NewKey: %v", err)
		}
		err = repo.Create(ctx, k, hash)
		if tc.sourceID == first {
			if err != nil {
				t.Fatalf("first Create: %v", err)
			}
			continue
		}
		if !errs.IsType(err, errs.ErrDuplicateEntry) {
			t.Fatalf("second Create() = %v, want a duplicate", err)
		}
	}
}

// Rotation is "issue the second, let them move, revoke the first", and this is
// what lets a customer tell which is which.
func TestListShowsEveryKeyIncludingRevokedOnes(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "KB")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	_, live := issued(t, repo, "KC", sourceID, "production", nil)
	_, dead := issued(t, repo, "KD", sourceID, "ci", nil)
	if err := repo.Revoke(ctx, dead.ID, keyNow()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	got, err := repo.ListBySourceID(ctx, sourceID)
	if err != nil {
		t.Fatalf("ListBySourceID: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want both", len(got))
	}

	byID := map[shared.ID]source.Key{}
	for _, k := range got {
		byID[k.ID] = k
	}
	if byID[live.ID].Label != "production" || byID[dead.ID].Label != "ci" {
		t.Errorf("labels did not survive: %+v", got)
	}
	if !byID[live.ID].IsLive(keyNow()) {
		t.Error("the live key came back not live")
	}
	if byID[dead.ID].RevokedAt == nil {
		t.Error(
			"the revoked key came back with no revocation time -- rotation is a guess without it",
		)
	}
}

// Revoking twice is not the same as revoking once, and an operator has to be
// able to tell. Unlike Touch, zero rows here is a real answer.
func TestRevokingTwiceIsReported(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "KE")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	_, k := issued(t, repo, "KF", sourceID, "leaked", nil)

	if err := repo.Revoke(ctx, k.ID, keyNow()); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if err := repo.Revoke(ctx, k.ID, keyNow()); !errs.IsType(err, errs.ErrNotFound) {
		t.Errorf("second Revoke() = %v, want a not-found", err)
	}
	if err := repo.Revoke(ctx, shared.ID(ulid("ZZ")), keyNow()); !errs.IsType(
		err,
		errs.ErrNotFound,
	) {
		t.Errorf("Revoke() on a key that never existed = %v, want a not-found", err)
	}
}

// last_used_at is written once per window, not once per request: the whole
// point is to keep an UPDATE off the hottest path in the service.
func TestTouchWritesOncePerWindow(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	sourceID := withASource(t, pool, "KG")
	repo := postgres.NewAPIKeyRepository(pool)
	ctx := context.Background()

	_, k := issued(t, repo, "KH", sourceID, "production", nil)

	lastUsed := func() *time.Time {
		t.Helper()
		keys, err := repo.ListBySourceID(ctx, sourceID)
		if err != nil {
			t.Fatalf("ListBySourceID: %v", err)
		}
		return keys[0].LastUsedAt
	}

	if lastUsed() != nil {
		t.Fatal("a key that has never been used should say so with nil")
	}

	first := keyNow()
	if err := repo.Touch(ctx, k.ID, first, time.Hour); err != nil {
		t.Fatalf("first Touch: %v", err)
	}
	if got := lastUsed(); got == nil || !got.Equal(first) {
		t.Fatalf("LastUsedAt = %v, want %v", got, first)
	}

	// Inside the window: nothing is written, and no error either.
	if err := repo.Touch(ctx, k.ID, first.Add(time.Minute), time.Hour); err != nil {
		t.Fatalf("Touch inside the window: %v", err)
	}
	if got := lastUsed(); !got.Equal(first) {
		t.Errorf("LastUsedAt moved to %v inside the window", got)
	}

	// Past it: written again.
	later := first.Add(2 * time.Hour)
	if err := repo.Touch(ctx, k.ID, later, time.Hour); err != nil {
		t.Fatalf("Touch past the window: %v", err)
	}
	if got := lastUsed(); !got.Equal(later) {
		t.Errorf("LastUsedAt = %v, want %v", got, later)
	}
}

// Touching a key that is gone changes nothing and says nothing. It is
// bookkeeping, and the caller has been told to let the request through.
func TestTouchingAKeyThatIsNotThereIsNotAFailure(t *testing.T) {
	pool := connect(t)
	truncate(t, pool)

	err := postgres.NewAPIKeyRepository(pool).
		Touch(context.Background(), shared.ID(ulid("ZZ")), keyNow(), time.Hour)
	if err != nil {
		t.Fatalf("Touch() = %v, want nil", err)
	}
}
