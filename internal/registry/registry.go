// Package registry opens this service's infrastructure and owns what it opened.
// It is the only place that reads config and hands back a running dependency,
// which is what keeps internal/infra free of anything srosha knows.
//
// Only bootstrap may import it. Opening a dependency outside the one place that
// closes it is how a process ends up with a pool nobody shuts down.
package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Resources is what is open right now. Things close by tier, highest first, so
// nothing is torn down under something still using it: the listener stops
// before the pool its handlers query.
type Resources struct {
	log   *slog.Logger
	steps []step
}

// step is one opened dependency. ready is nil when it has no health of its own.
type step struct {
	name  string
	tier  tier
	ready func(context.Context) error
	close func(context.Context) error
}

func New(log *slog.Logger) *Resources {
	return &Resources{log: log}
}

// add records something that is already open. Only this package's own openers
// call it, and only once the thing they opened has answered.
func (r *Resources) add(s step) {
	r.steps = append(r.steps, s)
}

// Check is one dependency and what it answered. Err is nil when it is fine.
type Check struct {
	Name string
	Err  error
}

// Ready asks everything that has health of its own and reports each answer
// separately.
//
// Separately, rather than one joined error, because whoever asked needs to know
// WHICH dependency is down, and the only other way to tell would be to read the
// error's text -- which is exactly what this repository forbids.
//
// It asks all of them instead of stopping at the first failure: an operator
// wants every name, not the first one. Tiers do not apply here -- asking a
// question of something changes nothing, so there is no order to get wrong.
func (r *Resources) Ready(ctx context.Context) []Check {
	checks := make([]Check, 0, len(r.steps))

	for _, s := range r.steps {
		if s.ready == nil {
			continue
		}
		checks = append(checks, Check{Name: s.name, Err: s.ready(ctx)})
	}
	return checks
}

// Close shuts everything down from the highest tier to the lowest, and within
// one tier in the reverse of the order it opened. It does not stop at the first
// failure: a broker that refuses to drain must not leave the pool open. Calling
// it twice is safe, because shutdown paths cross.
//
// The log line is deliberate: the order is a decision, and this is how anyone
// reading a production shutdown sees the decision that was actually taken.
func (r *Resources) Close(ctx context.Context) error {
	var failures []error

	for t := tierHighest; t >= tierStore; t-- {
		for i := len(r.steps) - 1; i >= 0; i-- {
			s := r.steps[i]
			if s.tier != t {
				continue
			}

			r.log.InfoContext(ctx, "closing dependency", "name", s.name, "tier", int(t))

			if err := s.close(ctx); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", s.name, err))
			}
		}
	}

	r.steps = nil
	return errors.Join(failures...)
}
