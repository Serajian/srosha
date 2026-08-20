package source

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

type Service struct {
	repo    Repository
	limiter RateLimiter
}

func NewService(repo Repository, limiter RateLimiter) *Service {
	return &Service{repo: repo, limiter: limiter}
}

// Admit answers one question: may this source act right now. It is the rate
// limit and the active check together, so no caller can pass one and forget
// the other.
func (s *Service) Admit(ctx context.Context, id string) (*Source, error) {
	allowed, err := s.limiter.Allow(ctx, id)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errs.TooManyErr("too many requests").
			WithErr(ErrRateLimited).
			WithStr(fmt.Sprintf("source %q", id))
	}

	src, err := s.repo.ReadByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := src.EnsureActive(); err != nil {
		return nil, err
	}
	return src, nil
}

// Resolve turns the requested channels into the recipients to deliver to.
func (s *Service) Resolve(src *Source, routes []Route) ([]shared.Recipient, error) {
	if len(routes) == 0 {
		return nil, errs.InvalidInputErr("at least one channel is required").
			WithErr(ErrNoRoutes)
	}

	out := make([]shared.Recipient, 0, len(routes))
	for _, r := range routes {
		rs, err := src.Resolve(r.Channel, r.Address)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}
