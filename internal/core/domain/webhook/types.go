package webhook

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Registration is what a source asked for when registering its callback.
type Registration struct {
	CallbackURL string

	// BatchInterval bounds how often we call. Zero means send each outcome as
	// it settles, which is only sensible when the fan-out is small.
	BatchInterval time.Duration

	// MaxBatchSize caps one call, so a thousand deliveries settling at once do
	// not become one request that times out.
	MaxBatchSize int
}

// URLPolicy is how strict the URL check is. Production forbids plain http and
// anything inside our own network; a developer testing against localhost needs
// both, so the choice comes from config while the rule stays here.
type URLPolicy struct {
	AllowInsecure bool
	AllowPrivate  bool
}

// Strict is the production policy.
var Strict = URLPolicy{}

// Snapshot is a webhook flattened for storage.
type Snapshot struct {
	ID       shared.ID
	SourceID string

	CallbackURL   string
	BatchInterval time.Duration
	MaxBatchSize  int

	IsActive            bool
	ConsecutiveFailures int

	CreatedAt time.Time
	UpdatedAt time.Time
}
