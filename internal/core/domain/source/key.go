package source

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Key is one API key of a source -- everything about it except the key.
//
// It lives in this package rather than its own because the whole value a key
// has to the core is which source is calling, and the statement that answers
// that returns a Source. A separate package would have to import this one, and
// no domain here imports another.
//
// There is no hash field either. What is stored and how it is derived is the
// adapter's, and nothing the core does needs it.
type Key struct {
	ID       shared.ID
	SourceID string
	Label    string

	CreatedAt time.Time

	// Null until the key is first used, and then only accurate to within the
	// touch window -- writing it on every request would put an UPDATE on the
	// hottest path in the service.
	LastUsedAt *time.Time

	// Null means it does not expire. That is a deliberate choice a customer
	// makes, not an oversight.
	ExpiresAt *time.Time

	// Marked, never deleted: after an incident the questions are when it was
	// revoked and when it was last used, and a deleted row answers neither.
	RevokedAt *time.Time
}

// NewKey describes a key about to be issued. The key itself is minted by the
// adapter that knows what one looks like; this is only what we keep.
func NewKey(
	id shared.ID, sourceID, label string, expiresAt *time.Time, now time.Time,
) (*Key, error) {
	if id.IsZero() {
		return nil, errs.InternalErr("key id is missing").WithErr(shared.ErrInvalidID)
	}
	if sourceID == "" {
		return nil, errs.InternalErr("source is missing").WithErr(ErrMissingSource)
	}
	if err := validateLabel(label); err != nil {
		return nil, err
	}
	// An already-expired key is refused rather than stored. Storing one would
	// hand a customer something that never worked and no error saying why.
	if expiresAt != nil && !expiresAt.After(now) {
		return nil, errs.InvalidInputErr("expiry is not in the future").
			WithErr(ErrKeyAlreadyExpired)
	}

	return &Key{
		ID:        id,
		SourceID:  sourceID,
		Label:     label,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

// IsLive answers what the authentication statement asks in its WHERE clause.
// It exists for whoever reads a key back -- the statement does not call it,
// because a check done after the row is chosen is a check done too late.
func (k Key) IsLive(now time.Time) bool {
	return k.RevokedAt == nil && (k.ExpiresAt == nil || k.ExpiresAt.After(now))
}

// validateLabel keeps the one thing a person uses to tell two keys apart from
// being empty. Rotation is "issue the second, revoke the first", and an
// unlabelled pair makes that a guess.
func validateLabel(label string) error {
	if label == "" {
		return errs.InvalidInputErr("key label is required").WithErr(ErrKeyLabelRequired)
	}
	if len(label) > maxKeyLabelLen {
		return errs.InvalidInputErr("key label is too long").
			WithErr(ErrKeyLabelTooLong).
			WithStr(fmt.Sprintf("%d chars, max %d", len(label), maxKeyLabelLen))
	}
	return nil
}
