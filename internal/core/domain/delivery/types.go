package delivery

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// IDFunc supplies an id per delivery. Injected, so the domain generates nothing
// itself and a test can hand it a fixed sequence.
type IDFunc func() shared.ID

// Snapshot is the whole state of a delivery, flat. It exists so a repository can
// load and store one without a ten-argument function, and so the guarded fields
// stay guarded everywhere else.
type Snapshot struct {
	ID             shared.ID
	NotificationID shared.ID
	Recipient      shared.Recipient

	Status            Status
	Attempts          int
	LastError         string
	FailureReason     FailureReason
	ProviderMessageID string
	NotifiedAt        *time.Time
	UpdatedAt         time.Time
}
