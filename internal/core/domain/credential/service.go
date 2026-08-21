package credential

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service { return &Service{repo: repo} }

// Resolve names the identity to send with, refusing one that does not exist or
// has been switched off.
func (s *Service) Resolve(
	ctx context.Context, sourceID string, c shared.Channel, name string,
) (Credential, error) {
	credsList, err := s.repo.ListBySourceAndChannel(ctx, sourceID, c)
	if err != nil {
		return Credential{}, err
	}
	return Pick(credsList, name)
}
