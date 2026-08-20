package notification

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	Create(ctx context.Context, n *Notification) error
	ReadByID(ctx context.Context, id shared.ID) (*Notification, error)

	// ReadByIdempotencyKey returns nil when the key has not been used, so the
	// caller can tell "seen before" from "failed to look".
	ReadByIdempotencyKey(ctx context.Context, sourceID, key string) (*Notification, error)
}
