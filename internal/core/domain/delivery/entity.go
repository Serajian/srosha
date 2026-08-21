package delivery

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/pkg/errs"
)

// Delivery is one message to one recipient. The exported fields never change;
// the rest move together through MarkSent and MarkFailed, because writing one
// alone would produce a row that contradicts itself.
type Delivery struct {
	ID             shared.ID
	NotificationID shared.ID
	Recipient      shared.Recipient

	// Which of the source's sending identities to use. Empty means its default
	// for this channel. Stamped at construction, so changing the source's setup
	// later cannot move a delivery that is already waiting.
	SenderName string

	status            Status
	attempts          int
	lastError         string
	failureReason     FailureReason
	providerMessageID string
	notifiedAt        *time.Time
	updatedAt         time.Time
}

// NewSet opens one delivery per recipient. It takes the whole set because both
// rules are about the set: at least one, and no repeats. Duplicates are refused
// on the whole recipient, so one channel with two addresses stays valid.
func NewSet(
	notificationID shared.ID, recipients []shared.Recipient,
	senders map[shared.Channel]string, nextID shared.IDFunc, now time.Time,
) ([]Delivery, error) {
	if notificationID.IsZero() {
		return nil, errs.InternalErr("notification id is missing").
			WithErr(ErrMissingNotification)
	}
	if nextID == nil {
		return nil, errs.InternalErr("delivery id generator is missing").
			WithErr(ErrMissingIDFunc)
	}
	if now.IsZero() {
		return nil, errs.InternalErr("creation timestamp is missing").
			WithErr(shared.ErrInvalidID)
	}
	if len(recipients) == 0 {
		return nil, errs.InvalidInputErr("at least one recipient is required").
			WithErr(ErrNoRecipients)
	}

	out := make([]Delivery, 0, len(recipients))
	seen := make(map[shared.Recipient]struct{}, len(recipients))

	for _, r := range recipients {
		if err := r.Validate(); err != nil {
			return nil, err
		}
		if _, dup := seen[r]; dup {
			return nil, errs.InvalidInputErr("recipient listed more than once").
				WithErr(ErrDuplicateRecipient).
				WithStr(fmt.Sprintf("recipient %s", r))
		}
		seen[r] = struct{}{}

		id := nextID()
		if id.IsZero() {
			return nil, errs.InternalErr("delivery id is missing").
				WithErr(shared.ErrInvalidID)
		}
		out = append(out, Delivery{
			ID:             id,
			NotificationID: notificationID,
			Recipient:      r,
			SenderName:     senders[r.Channel],
			status:         StatusPending,
			updatedAt:      now,
		})
	}
	return out, nil
}

// Restore rebuilds from storage WITHOUT validation: a row valid when written
// must stay loadable when a rule tightens. Repository only.
func Restore(s Snapshot) *Delivery {
	return &Delivery{
		ID:                s.ID,
		NotificationID:    s.NotificationID,
		Recipient:         s.Recipient,
		SenderName:        s.SenderName,
		status:            s.Status,
		attempts:          s.Attempts,
		lastError:         s.LastError,
		failureReason:     s.FailureReason,
		providerMessageID: s.ProviderMessageID,
		notifiedAt:        s.NotifiedAt,
		updatedAt:         s.UpdatedAt,
	}
}

// --- guarded state ---------------------------------------------------------

func (d *Delivery) Status() Status               { return d.status }
func (d *Delivery) Attempts() int                { return d.attempts }
func (d *Delivery) LastError() string            { return d.lastError }
func (d *Delivery) FailureReason() FailureReason { return d.failureReason }
func (d *Delivery) ProviderMessageID() string    { return d.providerMessageID }
func (d *Delivery) UpdatedAt() time.Time         { return d.updatedAt }
func (d *Delivery) NotifiedAt() *time.Time       { return d.notifiedAt }

// IsSettled reports whether this delivery has stopped moving.
func (d *Delivery) IsSettled() bool { return d.status.IsSettled() }

// --- state transitions -----------------------------------------------------

// MarkSent means the provider accepted it, not that anyone received it.
// attempts comes from the broker, so an attempt that died mid-send still counts.
func (d *Delivery) MarkSent(providerMessageID string, attempts int, now time.Time) error {
	if err := d.moveTo(StatusSent, now); err != nil {
		return err
	}
	d.providerMessageID = providerMessageID
	d.attempts = attempts
	d.lastError = ""
	d.failureReason = ""
	return nil
}

// MarkFailed is final. Transient failures are not recorded; the broker retries.
func (d *Delivery) MarkFailed(
	reason FailureReason, detail string, attempts int, now time.Time,
) error {
	if !reason.Valid() {
		return errs.InternalErr("failure reason is missing").
			WithErr(ErrMissingFailureReason).
			WithStr(fmt.Sprintf("got %q", reason))
	}
	if err := d.moveTo(StatusFailed, now); err != nil {
		return err
	}
	d.failureReason = reason
	d.lastError = detail
	d.attempts = attempts
	return nil
}

func (d *Delivery) MarkNotified(now time.Time) {
	d.notifiedAt = &now
}

func (d *Delivery) moveTo(next Status, now time.Time) error {
	if !d.status.CanTransitionTo(next) {
		return errs.InternalErr("invalid delivery status transition").
			WithErr(ErrInvalidTransition).
			WithStr(fmt.Sprintf("delivery %s: %s -> %s", d.ID, d.status, next))
	}
	d.status = next
	d.updatedAt = now
	return nil
}
