package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type MQ struct {
	// URL is a secret: each binary connects as its own user, password included.
	URL env.Secret

	// Stream and DuplicateWindow belong together: retrying a publish is only
	// safe because the broker drops a message id it has seen inside this
	// window. Recovery does not republish -- it sends -- so this window guards
	// the lost acknowledgement and nothing else.
	// They are the adapter's, not the connection's -- the stream is this
	// service's vocabulary and nats knows nothing about it.
	Stream          string
	DuplicateWindow time.Duration

	// MaxAge is a backstop, not a correctness rule: the deliveries table is the
	// record of what must be sent, and a dispatcher that has fallen behind is
	// caught by recovery reading rows rather than by the broker keeping events.
	// This only stops a stream nobody is draining from growing without bound.
	MaxAge time.Duration

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
		MaxAge:          r.Duration("MQ_MAX_AGE", 24*time.Hour),
		ConnectTimeout:  r.Duration("MQ_CONNECT_TIMEOUT", 10*time.Second),
		MaxReconnects:   r.Int("MQ_MAX_RECONNECTS", -1),
		ReconnectWait:   r.Duration("MQ_RECONNECT_WAIT", 2*time.Second),
		DrainTimeout:    r.Duration("MQ_DRAIN_TIMEOUT", 15*time.Second),
	}

	r.Check(mq.Stream != "", "NOTIF_MQ_STREAM must not be empty")

	// Shorter than the duplicate window and an event could be dropped while the
	// broker still refuses to store it again -- the one publish we had would be
	// gone, and a republish silently ignored.
	r.Check(mq.MaxAge > mq.DuplicateWindow,
		"NOTIF_MQ_MAX_AGE must be longer than NOTIF_MQ_DUPLICATE_WINDOW")
	r.Check(mq.MaxReconnects >= -1, "NOTIF_MQ_MAX_RECONNECTS must be -1 or above")
	return mq
}
