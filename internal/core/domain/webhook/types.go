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
// The field names are fixed here rather than in an adapter, because they are
// our public contract: a new adapter must not be able to rename them and break
// every client.
//
// ID is for tracing only. A client telling duplicates apart must use
// DeliveryID: a delivery settles once and never changes, while a batch is
// whatever had finished at the moment it was built.
type Batch struct {
	ID             shared.ID `json:"batch_id"`
	NotificationID shared.ID `json:"notification_id"`
	SentAt         time.Time `json:"sent_at"`
	Results        []Result  `json:"results"`
}

// Result is what happened to one recipient.
//
// There is no field for the provider's own error text. That is written for
// operators and can name hosts, limits and internals; Reason says what happened
// without any of it.
type Result struct {
	DeliveryID shared.ID `json:"delivery_id"`
	Channel    string    `json:"channel"`
	Address    string    `json:"address"`
	Status     string    `json:"status"`
	SettledAt  time.Time `json:"settled_at"`

	// Only one of these is ever set: a reason when it failed, a provider id
	// when it did not.
	Reason            string `json:"reason,omitempty"`
	ProviderMessageID string `json:"provider_message_id,omitempty"`
}
