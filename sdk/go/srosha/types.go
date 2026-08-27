package srosha

import "time"

// Priority is how far ahead of other traffic a message goes.
//
// Each source has a ceiling. Asking above it is not an error: the message is
// accepted at the ceiling and Receipt.Downgraded says so, so a customer learns
// about their limit rather than losing a message to it.
type Priority string

const (
	// PriorityDefault is the zero value and means "I did not choose", which the
	// service reads as normal. It is separate from PriorityNormal on purpose:
	// "I did not say" and "I chose the lowest" are different requests.
	PriorityDefault  Priority = ""
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// Window is how far back a listing reaches.
//
// A closed set rather than two timestamps, because srosha is not an archive:
// past the retention age a message is deleted, and a range reaching beyond it
// would come back short with nothing saying so. An empty answer would then mean
// two things at once -- "you sent nothing" and "you sent something we no longer
// have" -- and there would be no way to tell which.
type Window string

const (
	// Everything is the zero value: as far back as the service keeps. It is the
	// only value that is right whatever that age is set to, because it names no
	// number of its own.
	Everything Window = ""
	LastHour   Window = "last_hour"
	LastDay    Window = "last_day"
	LastWeek   Window = "last_week"
	LastMonth  Window = "last_month"
)

// Status is what happened to one recipient.
//
// There is no "retrying". A delivery that failed transiently stays Pending and
// is tried again; only an outcome that will not change is written down.
type Status string

const (
	StatusPending Status = "pending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
)

// FailureReason says why a delivery will not be retried. It is a fixed set
// rather than the provider's own error text, which is written for operators and
// names hosts, limits and internals.
type FailureReason string

const (
	FailureNone FailureReason = ""

	// FailureExpired: the message's own expiry passed before it went out.
	FailureExpired FailureReason = "expired"

	// FailureMaxAttempts: tried as often as the service will try.
	FailureMaxAttempts FailureReason = "max_attempts"

	// FailurePermanent: the provider refused the message, and would again.
	FailurePermanent FailureReason = "permanent"

	// FailureNoSender: no identity is configured for this channel. A
	// configuration answer, not a provider one, and never retried.
	FailureNoSender FailureReason = "no_sender"

	// FailureNotReachable: the provider refused the *recipient* rather than the
	// message. This is the one a source can act on -- nothing they wrote
	// differently would have helped, so the address itself is the problem.
	FailureNotReachable FailureReason = "not_reachable"
)

// Message is what a source sends.
type Message struct {
	// IdempotencyKey makes retrying safe: the same key twice returns the
	// original message rather than creating a second one. Unique per source, so
	// two customers picking "order-42" are not talking about the same thing.
	//
	// Leave it empty and one is generated per Submit call. That is what makes a
	// retry inside Submit safe, and it is why two separate Submit calls with no
	// key still produce two messages -- which is correct.
	IdempotencyKey string

	Title string
	Body  string

	Priority Priority

	// ExpireAt is when the message stops being worth sending. Zero means never.
	ExpireAt time.Time

	// Metadata is carried through untouched: an order number, a trace id. Some
	// channels look in it for what their API needs -- a WhatsApp template, an
	// FCM data payload -- and it is returned with the message unchanged.
	Metadata map[string]string

	// Routes is at least one. Each is a separate delivery with its own outcome.
	Routes []Route
}

// Receipt is what Submit answers with.
type Receipt struct {
	// ID names the message. On a duplicate idempotency key this is the
	// original's.
	ID string

	// Priority is what the message will actually be sent at.
	Priority Priority

	// Downgraded says the requested priority was above this source's ceiling
	// and was lowered. Reported rather than hidden, so a customer can see their
	// limit.
	Downgraded bool

	// Duplicate says this idempotency key had been used and nothing new was
	// created.
	Duplicate bool
}

// Notification is a message as the service holds it.
type Notification struct {
	ID             string
	IdempotencyKey string
	Title          string
	Body           string

	// Requested is what was asked for; Priority is what the ceiling allowed.
	// Both, because they can differ.
	Requested Priority
	Priority  Priority

	ExpireAt  time.Time
	Metadata  map[string]string
	CreatedAt time.Time
}

// Delivery is one recipient's outcome.
type Delivery struct {
	ID      string
	Channel Channel
	Address string

	// Sender is which registered identity this went out from.
	Sender string

	Status Status

	// Reason is set only when Status is StatusFailed.
	Reason FailureReason

	// ProviderMessageID is the provider's own id. srosha does not track
	// delivery past handing a message over, so this is the handle a source
	// needs to do it themselves.
	ProviderMessageID string

	// UpdatedAt is when this delivery last moved. There is no created time: a
	// delivery is created with its message and never on its own.
	UpdatedAt time.Time
}

// Result is a message and what happened to each of its recipients.
type Result struct {
	Notification Notification
	Deliveries   []Delivery
}
