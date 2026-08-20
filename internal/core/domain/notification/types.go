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
