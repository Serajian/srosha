package port

import (
	"context"

	"github.com/Serajian/srosha/internal/core/shared"
)

type Sender interface {
	Channel() shared.Channel

	// Send returns the provider's own id for the message. We do not track
	// delivery ourselves, so that id is the handle a source needs to do it.
	Send(ctx context.Context, m shared.Message) (providerMessageID string, err error)
}

// SenderRegistry hands back a sender already configured with the right identity.
// The core asks for one by name and never sees a token: which credential, how it
// is decrypted, and what client is built are all the adapter's business.
type SenderRegistry interface {
	For(ctx context.Context, sourceID string, c shared.Channel, name string) (Sender, error)
}
