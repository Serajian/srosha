package session

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Repository is where signed-in browsers are kept.
type Repository interface {
	Create(ctx context.Context, s *Session) error
	Read(ctx context.Context, id shared.ID) (*Session, error)
	Touch(ctx context.Context, s *Session) error
	Delete(ctx context.Context, id shared.ID) error
}
