// Package source holds the caller identity: who is allowed to send what, at
// which priority, and to whom.
package source

import (
	"fmt"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Source is the authenticated caller of the service.
//
// Its fields are exported, unlike the notification aggregate's. That is not an
// inconsistency: a notification hides its fields because Status is DERIVED and
// must never be assigned from outside. A Source has no derived state and no
// lifecycle of its own -- it is configuration loaded from a row -- so there is
// nothing for accessors to protect.
type Source struct {
	ID   string
	Name string

	// MaxPriority is the ceiling this source may request. A request above it is
	// clamped rather than rejected; see the notification aggregate.
	MaxPriority shared.Priority

	IsActive bool

	// AllowCustomTarget decides whether this source may address arbitrary
	// recipients. Leave it false for system sources whose alerts should only
	// ever reach the operators listed in DefaultTargets: a leaked API key then
	// cannot be used to message strangers.
	AllowCustomTarget bool

	// DefaultTargets is the per-channel fallback destination, loaded from the
	// source_channels table. A channel absent from this map can only be used by
	// a source that is allowed to supply its own target.
	DefaultTargets map[shared.Channel]string
}

// ResolveTarget decides where a channel should deliver to.
//
//	explicit target + allowed      -> use it, after validating its shape
//	explicit target + not allowed  -> refuse
//	no explicit target             -> fall back to the source's default
//	no explicit target, no default -> refuse
//
// This lives in the domain rather than in a service because it answers a
// business question -- who is this source permitted to reach -- not a
// data-access one. Putting it here also means the rule is enforced wherever a
// notification is built, not only on the path someone remembered to guard.
func (s *Source) ResolveTarget(c shared.Channel, requested string) (string, error) {
	if requested != "" {
		if !s.AllowCustomTarget {
			return "", errs.ForbiddenErr("custom delivery target not allowed").
				WithErr(ErrCustomTargetNotAllowed).
				WithStr(fmt.Sprintf("source %q, channel %q", s.ID, c))
		}
		if err := c.ValidateTarget(requested); err != nil {
			return "", err
		}
		return requested, nil
	}

	fallback, ok := s.DefaultTargets[c]
	if !ok || fallback == "" {
		return "", errs.InvalidInputErr("channel is not configured for this source").
			WithErr(ErrNoTargetForChannel).
			WithStr(fmt.Sprintf("source %q, channel %q", s.ID, c))
	}

	// Defaults are validated when they are written, but a row predating a rule
	// change may never have passed through validation. Re-checking is cheap and
	// turns a silent bad send into a clear failure.
	if err := c.ValidateTarget(fallback); err != nil {
		return "", err
	}
	return fallback, nil
}
