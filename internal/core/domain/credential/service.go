package credential

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Service struct {
	repo Repository
	now  shared.NowFunc
}

func NewService(repo Repository, now shared.NowFunc) *Service {
	return &Service{repo: repo, now: now}
}

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

// List is what a source has registered, on every channel.
func (s *Service) List(ctx context.Context, sourceID string) ([]Credential, error) {
	return s.repo.ListBySourceID(ctx, sourceID)
}

func (s *Service) Get(ctx context.Context, sourceID string, id shared.ID) (*Credential, error) {
	return s.repo.ReadByID(ctx, sourceID, id)
}

// Deactivate switches an identity off without forgetting it, so turning it back
// on is not a re-registration.
//
// The entity clears the default flag as it goes, because a default that cannot
// be used leaves every message naming no identity failing with nothing to fix.
// The channel is then left with no default until the source names one, which is
// the honest state rather than a guess at which should take over.
func (s *Service) Deactivate(ctx context.Context, c *Credential) error {
	c.Deactivate(s.now())
	return s.repo.Deactivate(ctx, c)
}

// Activate is the way back. It does not restore the default flag: which one is
// the default is a decision, and guessing it back would move it silently.
func (s *Service) Activate(ctx context.Context, c *Credential) error {
	c.Activate(s.now())
	return s.repo.Activate(ctx, c)
}

// MakeDefault is only half of moving the default. The caller clears the old one
// first, in the same transaction -- see Repository.
func (s *Service) MakeDefault(ctx context.Context, c *Credential) error {
	if err := c.MakeDefault(s.now()); err != nil {
		return err
	}
	return s.repo.SetDefault(ctx, c)
}

// ClearDefault takes the flag off whichever identity holds it on a channel.
func (s *Service) ClearDefault(ctx context.Context, sourceID string, c shared.Channel) error {
	return s.repo.ClearDefault(ctx, sourceID, c, s.now())
}
