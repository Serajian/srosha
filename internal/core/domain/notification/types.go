package notification

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Origin is what this package needs of a source, so it need not import it.
type Origin struct {
	ID          string
	Name        string
	MaxPriority shared.Priority
}

// Request is what the client asked for about the message. Recipients belong to
// the delivery aggregate, which validates them as a set.
type Request struct {
	IdempotencyKey string // optional
	Title          string // optional
	Body           string
	Priority       shared.Priority
	ExpireAt       *time.Time        // optional
	Metadata       map[string]string // optional
}

// Window bounds a listing in time. Both halves are optional and separate:
// "since yesterday" and "that week in March" are both real questions, and
// neither should have to invent the other's bound.
//
// Until is exclusive, so two windows that meet cannot both return the same
// message.
type Window struct {
	From  *time.Time
	Until *time.Time
}

// Valid refuses a window that cannot contain anything, which is a question
// nobody meant to ask.
func (w Window) Valid() bool {
	return w.From == nil || w.Until == nil || w.From.Before(*w.Until)
}
