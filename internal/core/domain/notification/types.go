package notification

import (
	"fmt"
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

// Window is how far back a listing reaches. A closed set rather than two
// timestamps a caller chooses, because this service is not an archive: past
// the retention age a message is deleted, and a range that reaches beyond it
// comes back short with nothing saying so.
//
// An empty answer would then mean two things at once -- "you sent nothing" and
// "you sent something we no longer have" -- and the caller cannot tell which.
// A vocabulary they cannot overshoot removes the question instead of answering
// it.
//
// The cost is real and worth naming: "that week in March" is no longer a thing
// anyone can ask. Inside a month of retention it was rarely worth asking, but
// it was a question, and it is gone.
type Window int8

const (
	// WindowAll reaches as far back as this deployment keeps. It is the zero
	// value, and it is the only one that is right whatever the retention age is
	// set to -- because it names no number of its own.
	WindowAll Window = iota

	WindowLastHour
	WindowLastDay
	WindowLastWeek
	WindowLastMonth
)

// windowLengths is the single place a window's reach is defined. Length and
// Valid both read it, so a value can never be listed in one and missing in the
// other.
//
// WindowAll is absent on purpose: its reach is the retention age, which is
// configuration and not a constant here.
var windowLengths = map[Window]time.Duration{
	WindowLastHour:  time.Hour,
	WindowLastDay:   24 * time.Hour,
	WindowLastWeek:  7 * 24 * time.Hour,
	WindowLastMonth: 30 * 24 * time.Hour,
}

func (w Window) Valid() bool {
	if w == WindowAll {
		return true
	}
	_, ok := windowLengths[w]
	return ok
}

// String renders a name for a log. An out-of-range value shows as Window(7)
// rather than vanishing into an empty string.
func (w Window) String() string {
	if name, ok := windowNames[w]; ok {
		return name
	}
	return fmt.Sprintf("Window(%d)", int8(w))
}

var windowNames = map[Window]string{
	WindowAll:       "ALL",
	WindowLastHour:  "LAST_HOUR",
	WindowLastDay:   "LAST_DAY",
	WindowLastWeek:  "LAST_WEEK",
	WindowLastMonth: "LAST_MONTH",
}

// Length is how far back this window reaches, given how long the service keeps
// messages. WindowAll is exactly that age; the rest are their own.
func (w Window) Length(keeps time.Duration) time.Duration {
	if w == WindowAll {
		return keeps
	}
	return windowLengths[w]
}

// Since is the earliest moment this window covers.
func (w Window) Since(now time.Time, keeps time.Duration) time.Time {
	return now.Add(-w.Length(keeps))
}
