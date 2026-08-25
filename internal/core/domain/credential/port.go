package credential

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Repository has no single Save, on purpose. The same row is written by very
// different callers -- the source through the API, and the sender registry when
// a key is rotated -- and one method that saves the whole entity lets each undo
// the other. So each method writes only what its caller meant to change.
//
// Every lookup and every write is scoped by source. The id arrives in a request
// body, so the difference is whether a guessed id finds somebody else's identity
// or finds nothing.
type Repository interface {
	// ListBySourceAndChannel returns the whole set on a channel, switched-off
	// ones included, and lets Pick choose. Filtering here would report a
	// disabled identity as one that does not exist, and the source would go
	// looking for a typo instead of turning it back on.
	ListBySourceAndChannel(
		ctx context.Context, sourceID string, c shared.Channel,
	) ([]Credential, error)

	// ListBySourceID is the same answer across every channel: what this source
	// has registered.
	ListBySourceID(ctx context.Context, sourceID string) ([]Credential, error)

	ReadByID(ctx context.Context, sourceID string, id shared.ID) (*Credential, error)

	// Deactivate and Activate write the flag alone. One already in the asked-for
	// state is not an error: two requests crossing arrive here together and only
	// one of them changes anything.
	Deactivate(ctx context.Context, c *Credential) error
	Activate(ctx context.Context, c *Credential) error

	// SetDefault takes the flag over, and ClearDefault gives it up. They are two
	// halves of one move and must run in one transaction: the index refuses two
	// defaults, so without the clear the set fails instead of taking over -- and
	// with the clear alone, the channel is left with no default at all and every
	// message that names no identity fails.
	SetDefault(ctx context.Context, c *Credential) error
	ClearDefault(ctx context.Context, sourceID string, c shared.Channel, now time.Time) error
}
