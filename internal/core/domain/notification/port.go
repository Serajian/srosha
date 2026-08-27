package notification

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	Create(ctx context.Context, n *Notification) error
	ReadByID(ctx context.Context, id shared.ID) (*Notification, error)

	// ReadByIdempotencyKey returns nil when the key has not been used, so the
	// caller can tell "seen before" from "failed to look".
	ReadByIdempotencyKey(ctx context.Context, sourceID, key string) (*Notification, error)

	// PageBySource answers "what did I send", newest first -- which is what a
	// source asking the question wants, and the opposite of every other listing
	// here.
	PageBySource(
		ctx context.Context, sourceID string, since time.Time, c shared.Cursor,
	) (shared.Pagination[Notification], error)

	// DeleteOlderThan drops one batch of messages written before a moment and
	// reports how many went. Their deliveries follow, by the foreign key.
	//
	// One batch rather than all of them, so a table that has been collecting for
	// a year is not cleared inside a single transaction holding locks on it.
	DeleteOlderThan(ctx context.Context, before time.Time, limit int) (int, error)
}
