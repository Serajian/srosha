package port

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Publisher interface {
	Publish(ctx context.Context, e shared.DispatchEvent) error
}
