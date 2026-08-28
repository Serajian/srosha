package logincode

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Repository is where one-time codes are kept.
type Repository interface {
	Create(ctx context.Context, c *LoginCode) error

	// ReadNewest is this person's most recent code, or ErrNotFound. Only the
	// newest is ever checked: asking for another is what invalidates the one
	// before it.
	ReadNewest(ctx context.Context, userID shared.ID) (*LoginCode, error)

	// Spend writes back the attempt count and the moment it was used.
	Spend(ctx context.Context, c *LoginCode) error

	// CountSince is how many codes this person has asked for in a window, which
	// is what the request limit is measured against.
	CountSince(ctx context.Context, userID shared.ID, since time.Time) (int, error)
}
