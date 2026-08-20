package delivery

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// IDFunc is injected, so the domain generates no id of its own.
type IDFunc func() shared.ID

// Snapshot is a delivery flattened for storage, so Restore needs no ten-argument
// signature and the guarded fields stay guarded everywhere else.
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
