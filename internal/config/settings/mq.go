package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type MQ struct {
	// URL is a secret: each binary connects as its own user, password included.
	URL env.Secret

	// Stream and DuplicateWindow belong together: republishing is only safe
	// because the broker drops a message id it has seen inside this window.
	Stream          string
	DuplicateWindow time.Duration
}

func LoadMQ(r *env.Reader) MQ {
	return MQ{
		URL:             r.RequiredSecret("MQ_URL"),
		Stream:          r.Str("MQ_STREAM", "NOTIFY"),
		DuplicateWindow: r.Duration("MQ_DUPLICATE_WINDOW", time.Hour),
	}
}
