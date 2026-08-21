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
	// They are the adapter's, not the connection's -- the stream is this
	// service's vocabulary and nats knows nothing about it.
	Stream          string
	DuplicateWindow time.Duration

	// ConnectTimeout bounds one dial and the wait at startup.
	ConnectTimeout time.Duration

	// MaxReconnects and ReconnectWait are what nats does on its own after the
	// first connection. Negative means forever.
	MaxReconnects int
	ReconnectWait time.Duration

	// DrainTimeout bounds shutdown: a handler gets this long to finish what it
	// is holding before the connection is closed under it.
	DrainTimeout time.Duration
}

func LoadMQ(r *env.Reader) MQ {
	mq := MQ{
		URL:             r.RequiredSecret("MQ_URL"),
		Stream:          r.Str("MQ_STREAM", "NOTIFY"),
		DuplicateWindow: r.Duration("MQ_DUPLICATE_WINDOW", time.Hour),
		ConnectTimeout:  r.Duration("MQ_CONNECT_TIMEOUT", 10*time.Second),
		MaxReconnects:   r.Int("MQ_MAX_RECONNECTS", -1),
		ReconnectWait:   r.Duration("MQ_RECONNECT_WAIT", 2*time.Second),
		DrainTimeout:    r.Duration("MQ_DRAIN_TIMEOUT", 15*time.Second),
	}

	r.Check(mq.Stream != "", "NOTIF_MQ_STREAM must not be empty")
	r.Check(mq.MaxReconnects >= -1, "NOTIF_MQ_MAX_RECONNECTS must be -1 or above")
	return mq
}
