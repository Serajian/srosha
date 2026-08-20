// Package port declares what the core needs from the outside. What the outside
// needs from the core is declared by whichever adapter consumes it.
package port

import (
	"context"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Clock and IDGenerator exist so the domain never reads ambient state: New
// takes the time and the id as arguments, and a test can make both fixed.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() shared.ID
}

// RateLimiter answers whether this source may send another request now. It is
// per source, so the gateway asks after authentication and never before.
type RateLimiter interface {
	Allow(ctx context.Context, sourceID string) (bool, error)
}
