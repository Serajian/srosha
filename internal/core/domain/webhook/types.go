package webhook

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Registration is what a source asked for when registering its callback.
type Registration struct {
	CallbackURL string
}

// URLPolicy is how strict the URL check is. Production forbids plain http and
// anything inside our own network; a developer testing against localhost needs
// both, so the choice comes from config while the rule stays here.
type URLPolicy struct {
	AllowInsecure bool
	AllowPrivate  bool
}

// Strict is the production policy.
var Strict = URLPolicy{}

// Snapshot is a webhook flattened for storage.
type Snapshot struct {
	ID       shared.ID
	SourceID string

	CallbackURL string

	IsActive            bool
	ConsecutiveFailures int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Batch is one message's outcome, sent when every recipient has settled.
//
// ID is for tracing only. A client telling duplicates apart must use
// DeliveryID: a delivery settles once and never changes, while a batch is
// whatever had finished at the moment it was built.
type Batch struct {
	ID             shared.ID
	NotificationID shared.ID
	SentAt         time.Time
	Results        []Result
}

// Result is what happened to one recipient.
//
// There is no field for the provider's own error text. That is written for
// operators and can name hosts, limits and internals; Reason says what happened
// without any of it.
type Result struct {
	DeliveryID        shared.ID
	Channel           string
	Address           string
	Status            string
	Reason            string
	ProviderMessageID string
	SettledAt         time.Time
}
