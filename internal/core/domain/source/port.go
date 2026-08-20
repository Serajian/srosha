package source

import "context"

type Repository interface {
	ReadByID(ctx context.Context, id string) (*Source, error)
}

// RateLimiter answers whether this source may act again now. The quota is per
// source, so the port belongs here rather than to whoever happens to ask.
type RateLimiter interface {
	Allow(ctx context.Context, sourceID string) (bool, error)
}
