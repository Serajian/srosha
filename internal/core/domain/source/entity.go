// Package source holds the caller identity: who may send what, at which
// priority, and to whom.
package source

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Source is the authenticated caller. Configuration loaded from a row, so
// nothing here is derived and nothing needs an accessor.
type Source struct {
	ID          string
	Name        string
	MaxPriority shared.Priority
	IsActive    bool

	// False bounds the damage of a leaked key: the source can then only reach
	// the addresses configured below, never a stranger.
	AllowCustomAddress bool

	// One address per channel. Reaching several people is a group chat or a
	// mailing list, which the customer manages.
	DefaultAddresses map[shared.Channel]string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *Source) EnsureActive() error {
	if s.IsActive {
		return nil
	}
	return errs.ForbiddenErr("source is not active").
		WithErr(ErrSourceInactive).
		WithStr(fmt.Sprintf("source %q", s.ID))
}

// Resolve turns one requested channel into the recipients to deliver to: the
// given address if this source may name one, otherwise its configured default.
// Returns a slice so that one channel can resolve to several later.
func (s *Source) Resolve(c shared.Channel, address string) ([]shared.Recipient, error) {
	if address != "" {
		if !s.AllowCustomAddress {
			return nil, errs.ForbiddenErr("custom delivery address not allowed").
				WithErr(ErrCustomAddressNotAllowed).
				WithStr(fmt.Sprintf("source %q, channel %q", s.ID, c))
		}
		return s.one(c, address)
	}

	fallback, ok := s.DefaultAddresses[c]
	if !ok || fallback == "" {
		return nil, errs.InvalidInputErr("channel is not configured for this source").
			WithErr(ErrNoAddressForChannel).
			WithStr(fmt.Sprintf("source %q, channel %q", s.ID, c))
	}

	// Re-checked: a default written before a rule tightened never passed it.
	return s.one(c, fallback)
}

func (s *Source) one(c shared.Channel, address string) ([]shared.Recipient, error) {
	r := shared.Recipient{Channel: c, Address: address}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return []shared.Recipient{r}, nil
}
