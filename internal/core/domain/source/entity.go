// Package source holds the caller identity: who may send what, at which
// priority, and to whom.
package source

import (
	"fmt"
	"strings"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Source is the authenticated caller. Configuration loaded from a row, so
// nothing here is derived and nothing needs an accessor.
type Source struct {
	ID   string
	Name string

	// Description is what this source is for, in the customer's words. A name
	// is a label; two sources both called "alerts" are told apart by this.
	Description string

	MaxPriority shared.Priority
	IsActive    bool

	// OwnerUserID is who registered this. A customer sees their own sources and
	// nobody else's.
	OwnerUserID shared.ID

	// ApprovedAt is when an operator first let this source out. A record, never
	// a gate: IsActive is what decides, and this only tells a queue what it has
	// never looked at.
	ApprovedAt *time.Time

	// ReviewedAt is when an operator last decided about this source, whichever
	// way they decided. Nil is the review queue.
	ReviewedAt *time.Time

	// ReviewNote is why, in the operator's words. The customer reads it, which
	// is the whole reason a refusal is not silent.
	ReviewNote string

	// False bounds the damage of a leaked key: the source can then only reach
	// the addresses configured below, never a stranger.
	AllowCustomAddress bool

	// One address per channel. Reaching several people is a group chat or a
	// mailing list, which the customer manages.
	DefaultAddresses map[shared.Channel]string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// New builds a source, switched off. An operator decides when it may send, and
// nothing here can decide that for them.
func New(
	id string, owner shared.ID, name string,
	addresses map[shared.Channel]string, now time.Time,
) (*Source, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, errs.InvalidInputErr("a source needs a name").WithErr(ErrEmptyName)
	}
	if len(trimmed) > maxNameLen {
		return nil, errs.InvalidInputErr("that name is too long").
			WithErr(ErrEmptyName).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), maxNameLen))
	}

	if addresses == nil {
		addresses = map[shared.Channel]string{}
	}
	for channel, address := range addresses {
		if err := channel.ValidateAddress(address); err != nil {
			return nil, err
		}
	}

	return &Source{
		ID:          id,
		OwnerUserID: owner,
		Name:        trimmed,
		MaxPriority: shared.PriorityNormal,

		// Switched off, and never approved. Both are the operator's to change.
		IsActive:           false,
		AllowCustomAddress: false,

		DefaultAddresses: addresses,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

// Reconfigure changes the three things a customer owns, or changes nothing.
//
// Everything is validated before anything is written, so a bad address does not
// leave a rename already applied -- the customer would fix the address and never
// learn the rename had gone through on its own.
//
// What is not here is the point of it: the ceiling, the switch, the owner and
// the id are not parameters, so no caller can pass them and no later edit can
// add them without saying so in this signature.
func (s *Source) Reconfigure(
	name, description string, addresses map[shared.Channel]string, now time.Time,
) error {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return errs.InvalidInputErr("a source needs a name").WithErr(ErrEmptyName)
	}
	if len(trimmedName) > maxNameLen {
		return errs.InvalidInputErr("that name is too long").
			WithErr(ErrEmptyName).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmedName), maxNameLen))
	}

	trimmedDescription := strings.TrimSpace(description)
	if len(trimmedDescription) > maxDescriptionLen {
		return errs.InvalidInputErr("that description is too long").
			WithErr(ErrEmptyName).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmedDescription), maxDescriptionLen))
	}

	if addresses == nil {
		addresses = map[shared.Channel]string{}
	}
	for channel, address := range addresses {
		if err := channel.ValidateAddress(address); err != nil {
			return err
		}
	}

	s.Name = trimmedName
	s.Description = trimmedDescription
	s.DefaultAddresses = addresses
	s.UpdatedAt = now
	return nil
}

func (s *Source) EnsureActive() error {
	if s.IsActive {
		return nil
	}
	return errs.ForbiddenErr("source is not active").
		WithErr(ErrSourceInactive).
		WithStr(fmt.Sprintf("source %q", s.ID))
}

// IsApproved reports whether an operator has ever let this source out. It is
// not the same question as EnsureActive: a source approved in March and
// switched off in August answers yes here and refuses there.
func (s *Source) IsApproved() bool { return s.ApprovedAt != nil }

// Approve lets this source send. It is the only method here that turns
// IsActive on from a fresh queue entry.
//
// Refused when the source has nowhere to send -- see IsReachable -- because
// approving it would make it look like it works when every message it sends
// is going to fail. The message names what is missing and who can fix it: an
// operator cannot add an address on somebody else's behalf, only the
// customer can.
func (s *Source) Approve(now time.Time) error {
	if !s.IsReachable() {
		return errs.InvalidInputErr(
			"this source has nowhere to send: no default address is set for any " +
				"channel, and custom addresses are not allowed. Only the customer can " +
				"fix this, by adding an address.").
			WithErr(ErrNoReachableAddress).
			WithStr(fmt.Sprintf("source %q", s.ID))
	}

	if s.ApprovedAt == nil {
		s.ApprovedAt = &now
	}
	s.ReviewedAt = &now
	s.ReviewNote = ""
	s.IsActive = true
	s.UpdatedAt = now
	return nil
}

// Refuse turns a source away, with a reason the customer will read.
//
// The reason is required and the refusal does not happen without one: a source
// that silently never works is the failure this whole state exists to prevent.
//
// A source already approved cannot be refused: refusing is a decision at the
// door, and approved_at set alongside a refusal would be indistinguishable
// from suspended. Suspend is the way to stop one that already got through.
func (s *Source) Refuse(note string, now time.Time) error {
	if s.IsApproved() {
		return errs.InvalidInputErr("an approved source cannot be refused, only suspended").
			WithErr(ErrAlreadyApproved).
			WithStr(fmt.Sprintf("source %q", s.ID))
	}

	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return errs.InvalidInputErr("a refusal needs a reason").WithErr(ErrNoReason)
	}
	if len(trimmed) > maxReviewNoteLen {
		return errs.InvalidInputErr("that reason is too long").
			WithErr(ErrNoReason).
			WithStr(fmt.Sprintf("%d chars, max %d", len(trimmed), maxReviewNoteLen))
	}

	s.ReviewedAt = &now
	s.ReviewNote = trimmed
	s.IsActive = false
	s.UpdatedAt = now
	return nil
}

// Suspend stops a source that was working. ApprovedAt is left alone, so this
// stays distinguishable from a source that was turned away at the door.
//
// A source that was never approved cannot be suspended, which is Refuse's
// guard read the other way round. Without it, suspending something still in
// the queue left is_active=f, approved_at=null, reviewed_at=set and
// review_note="" -- byte for byte a refusal with no reason, which is the exact
// state review_note was added to make impossible. The customer's page then
// reads "This source was not approved." followed by an empty sentence.
func (s *Source) Suspend(now time.Time) error {
	if !s.IsApproved() {
		return errs.InvalidInputErr(
			"a source that was never approved cannot be suspended, only refused").
			WithErr(ErrNotApproved).
			WithStr(fmt.Sprintf("source %q", s.ID))
	}

	s.ReviewedAt = &now
	s.IsActive = false
	s.UpdatedAt = now
	return nil
}

// Restore is the way back from Suspend. Also the way back from Refuse: a
// source restored straight from a refusal is being let out for the first
// time, and approved_at is what records that -- without it, a restored
// refusal would read as active and never approved, a state the table this
// column is built on does not have.
//
// "The way back" is the whole meaning, so there has to be somewhere to come
// back from. A source nobody has decided about yet is not suspended and not
// refused, and restoring it would approve it while the audit row said
// source.restore -- a first decision recorded under a verb that says it was
// the second. Approve is what a queued source takes, and it says so.
func (s *Source) Restore(now time.Time) error {
	if !s.IsReviewed() {
		return errs.InvalidInputErr(
			"this source has never been decided about, so approve it rather than restore it").
			WithErr(ErrNotReviewed).
			WithStr(fmt.Sprintf("source %q", s.ID))
	}

	// Same guard as Approve, and for the same reason: a source switched off
	// has exactly as little to send to as one still in the queue.
	if !s.IsReachable() {
		return errs.InvalidInputErr(
			"this source has nowhere to send: no default address is set for any " +
				"channel, and custom addresses are not allowed. Only the customer can " +
				"fix this, by adding an address.").
			WithErr(ErrNoReachableAddress).
			WithStr(fmt.Sprintf("source %q", s.ID))
	}

	if s.ApprovedAt == nil {
		s.ApprovedAt = &now
	}
	s.ReviewedAt = &now
	s.ReviewNote = ""
	s.IsActive = true
	s.UpdatedAt = now
	return nil
}

// IsReviewed reports whether an operator has ever decided about this source.
// It is the queue's question, and not the same one as IsApproved.
func (s *Source) IsReviewed() bool { return s.ReviewedAt != nil }

// IsReachable reports whether this source has anywhere to send a message: a
// configured default address on at least one channel, or permission to be
// given one per request. Either alone is enough -- a default can be sent to,
// and a custom address can be named -- so this is false only for the
// combination of neither.
//
// Approve and Restore refuse to turn a source on when this is false, and the
// portal's own source page asks the same question rather than re-deriving
// the rule: a source registered with nothing set up yet is the ordinary
// case, not an error, right up until somebody tries to let it out.
func (s *Source) IsReachable() bool {
	return len(s.DefaultAddresses) > 0 || s.AllowCustomAddress
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
