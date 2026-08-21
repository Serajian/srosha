package credential

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Repository interface {
	ListBySourceAndChannel(
		ctx context.Context, sourceID string, c shared.Channel,
	) ([]Credential, error)
}
