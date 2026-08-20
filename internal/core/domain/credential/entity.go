// Package credential holds which sending identity a source uses on a channel.
// The token and the provider settings behind it are the adapter's business:
// this package would otherwise have to know what SMTP is.
package credential

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

const maxNameLen = 32

// Credential names one sending identity. It carries no secret and no provider
// settings; the adapter resolves those by ID at send time.
type Credential struct {
	ID       shared.ID
	SourceID string
	Channel  shared.Channel
	Name     string

	CreatedAt time.Time
	UpdatedAt time.Time

	isDefault bool
	isActive  bool
}

// New opens an active credential. Only one per channel may be the default,
// which is a rule about the set and belongs to the caller holding it.
func New(
	id shared.ID, sourceID string, c shared.Channel, name string, isDefault bool, now time.Time,
) (*Credential, error) {
	if id.IsZero() {
		return nil, errs.InternalErr("credential id is missing").WithErr(shared.ErrInvalidID)
	}
	if sourceID == "" {
		return nil, errs.InternalErr("source is missing").WithErr(ErrMissingSource)
	}
	if now.IsZero() {
		return nil, errs.InternalErr("creation timestamp is missing").WithErr(shared.ErrInvalidID)
	}
	if !c.Valid() {
		return nil, errs.InvalidInputErr("unknown channel").
			WithErr(shared.ErrUnknownChannel).
			WithStr(fmt.Sprintf("got %q", c))
	}
	if err := validateName(name); err != nil {
		return nil, err
	}

	return &Credential{
		ID:        id,
		SourceID:  sourceID,
		Channel:   c,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
		isDefault: isDefault,
		isActive:  true,
	}, nil
}

// Snapshot is a credential flattened for storage.
type Snapshot struct {
	ID        shared.ID
	SourceID  string
	Channel   shared.Channel
	Name      string
	IsDefault bool
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Restore rebuilds from storage WITHOUT validation. Repository only.
func Restore(s Snapshot) *Credential {
	return &Credential{
		ID:        s.ID,
		SourceID:  s.SourceID,
		Channel:   s.Channel,
		Name:      s.Name,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		isDefault: s.IsDefault,
		isActive:  s.IsActive,
	}
}

func (c *Credential) IsDefault() bool { return c.isDefault }
func (c *Credential) IsActive() bool  { return c.isActive }

// MakeDefault refuses an inactive credential: a default that cannot be used
// leaves every send on that channel failing with nothing configured to fix.
func (c *Credential) MakeDefault(now time.Time) error {
	if !c.isActive {
		return errs.InvalidInputErr("an inactive credential cannot be the default").
			WithErr(ErrDefaultUnusable).
			WithStr(fmt.Sprintf("credential %q", c.Name))
	}
	c.isDefault = true
	c.UpdatedAt = now
	return nil
}

// Deactivate also clears the default flag, so the two can never contradict.
func (c *Credential) Deactivate(now time.Time) {
	c.isActive = false
	c.isDefault = false
	c.UpdatedAt = now
}

func (c *Credential) Activate(now time.Time) {
	c.isActive = true
	c.UpdatedAt = now
}

// Pick chooses the identity to send with: the one named, or the default when no
// name was given. This is the whole reason the aggregate exists.
func Pick(creds []Credential, name string) (Credential, error) {
	if name != "" {
		for _, c := range creds {
			if c.Name != name {
				continue
			}
			if !c.isActive {
				return Credential{}, errs.InvalidInputErr("credential is not active").
					WithErr(ErrInactive).
					WithStr(fmt.Sprintf("credential %q", name))
			}
			return c, nil
		}
		return Credential{}, errs.InvalidInputErr("no credential with that name").
			WithErr(ErrNotFound).
			WithStr(fmt.Sprintf("name %q", name))
	}

	for _, c := range creds {
		if c.isDefault && c.isActive {
			return c, nil
		}
	}
	return Credential{}, errs.InvalidInputErr("no sender configured for this channel").
		WithErr(ErrNoDefault)
}

// validateName keeps names usable in a URL and a config key: lowercase letters,
// digits and hyphens.
func validateName(name string) error {
	if name == "" {
		return errs.InvalidInputErr("credential name is required").WithErr(ErrEmptyName)
	}
	if len(name) > maxNameLen {
		return errs.InvalidInputErr("credential name is too long").
			WithErr(ErrInvalidName).
			WithStr(fmt.Sprintf("%d chars, max %d", len(name), maxNameLen))
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return errs.InvalidInputErr("credential name has the wrong format").
			WithErr(ErrInvalidName).
			WithStr(fmt.Sprintf("name %q", name))
	}
	return nil
}
