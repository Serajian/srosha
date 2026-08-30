package source

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	Create(ctx context.Context, s *Source) error
	ReadByID(ctx context.Context, id string) (*Source, error)
	ListByOwner(ctx context.Context, ownerID shared.ID) ([]Source, error)

	// UpdateSettings writes what the customer owns -- name, description and
	// default addresses -- and nothing else.
	//
	// Deliberately not called Update. The adapter has one of those, it writes
	// max_priority and allow_custom_address as well, and the whole difference
	// between the two is which columns a caller can reach. A name that hid that
	// difference would be the bug.
	UpdateSettings(ctx context.Context, s *Source) error
}

// KeyRepository is the authentication path, and only that. Issuing, listing and
// revoking keys are the administrator's, and get their own port when there is
// an administrator to call it.
type KeyRepository interface {
	// ReadSourceByKeyHash answers with the Source itself rather than an id,
	// because this runs on every request and a second lookup would double the
	// cost of the hottest path in the service. It also returns the key's own
	// id, which is the handle RecordUse needs.
	//
	// A hash arrives here, not a key. What we store and how it is derived is
	// the adapter's business; this layer only knows a key is looked up rather
	// than compared.
	//
	// A key that is unknown, revoked or expired arrives back as a nil Source
	// rather than an error. The statement cannot tell the three apart -- all
	// three are simply no row -- and neither may anything above it.
	ReadSourceByKeyHash(
		ctx context.Context, keyHash string, now time.Time,
	) (*Source, shared.ID, error)

	// Touch records that a key is in use, and writes nothing when it was
	// already touched within notUsedFor. Doing it on every request would put an
	// UPDATE on the hottest path; never doing it leaves last_used_at null and
	// the question it exists to answer unanswerable.
	Touch(ctx context.Context, keyID shared.ID, now time.Time, notUsedFor time.Duration) error
}

// RateLimiter answers whether this source may act again now. The quota is per
// source, so the port belongs here rather than to whoever happens to ask.
type RateLimiter interface {
	Allow(ctx context.Context, sourceID string) (bool, error)
}

// KeyIssuer is the other half of a key's life: making one, listing them and
// revoking one.
//
// Separate from KeyRepository because that one runs on every request and this
// one runs when a person clicks something -- a port that grew both would be
// faked in tests that care about neither.
type KeyIssuer interface {
	Create(ctx context.Context, k *Key, keyHash string) error
	ListBySourceID(ctx context.Context, sourceID string) ([]Key, error)
	Revoke(ctx context.Context, keyID shared.ID, now time.Time) error
}
