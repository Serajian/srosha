package delivery

import (
	"time"

	"github.com/Serajian/srosha/internal/core/shared"
)

// Snapshot is a delivery flattened for storage, so Restore needs no ten-argument
// signature and the guarded fields stay guarded everywhere else.
type Snapshot struct {
	ID             shared.ID
	NotificationID shared.ID
	Recipient      shared.Recipient
	SenderName     string

	Status            Status
	Attempts          int
	LastError         string
	FailureReason     FailureReason
	ProviderMessageID string
	NotifiedAt        *time.Time
	UpdatedAt         time.Time
}
