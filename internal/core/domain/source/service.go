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

// Admit answers "may this source send right now": the rate limit and the active
// check together, so no caller can ask one and forget the other. It spends a
// unit of the quota, so it belongs on the sending path and nowhere else.
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
	return s.Load(ctx, id)
}

// Load answers "who is this source", without touching the quota. Managing a
// webhook is not sending, and must not cost a message.
func (s *Service) Load(ctx context.Context, id string) (*Source, error) {
	src, err := s.repo.ReadByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := src.EnsureActive(); err != nil {
		return nil, err
	}
	return src, nil
}

// Manage answers "who is this source" for a caller that is changing its
// configuration rather than sending.
//
// It deliberately does NOT require the source to be active. A source is created
// waiting for approval, and a customer sets it up in that window -- a bot, a
// callback, a key -- so that what an operator approves is a source that is
// ready rather than an empty one. A source an operator later switched off can
// still be reconfigured for the same reason: fixing whatever caused that is the
// only way back.
func (s *Service) Manage(ctx context.Context, id string) (*Source, error) {
	return s.repo.ReadByID(ctx, id)
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
