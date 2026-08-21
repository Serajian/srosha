// Package messagequeue opens the connection to nats and owns it. It knows how
// to reach the broker and nothing about what this service publishes there: no
// stream, no subject, no consumer.
package messagequeue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is registry's job.
//
// Nothing here has a default. Every value is an operational decision, so it
// comes from config and is named in one place rather than two.
type Config struct {
	URL string

	// ConnectTimeout bounds one dial, and bounds the wait at startup: nats
	// retries underneath, and this is how long the process lets it before
	// calling the broker unreachable.
	ConnectTimeout time.Duration

	// MaxReconnects and ReconnectWait are what nats does on its own after the
	// first connection, so this package writes no retry loop. Negative means
	// forever; zero disables reconnecting entirely.
	MaxReconnects int
	ReconnectWait time.Duration

	// DrainTimeout bounds shutdown. Draining lets a handler finish what it is
	// holding; past this, closing is the honest answer.
	DrainTimeout time.Duration
}

func (c Config) validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(strings.TrimSpace(c.URL) != "", "url is empty")
	check(c.ConnectTimeout > 0, "connect timeout must be above zero")
	check(c.MaxReconnects >= reconnectForever,
		"max reconnects %d is below %d", c.MaxReconnects, reconnectForever)
	check(c.ReconnectWait > 0, "reconnect wait must be above zero")
	check(c.DrainTimeout > 0, "drain timeout must be above zero")

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("messagequeue: %w", errors.Join(errs...))
}

// NATS owns the connection: it opens it, proves it works, answers for its
// health and drains it. Nothing else in the process may close it.
type NATS struct {
	cfg  Config
	log  *slog.Logger
	conn *nats.Conn
	js   jetstream.JetStream

	// closed is shut by nats itself once draining has finished, which is the
	// only way to wait for a Drain: it returns before the work is done.
	closed chan struct{}

	// lastErr is written from the library's callbacks and read by waitReady.
	mu      sync.Mutex
	lastErr error
}

// New checks the configuration and touches nothing. Connect does the I/O, so a
// wiring mistake is found before anything is dialed.
func New(cfg Config, log *slog.Logger) (*NATS, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &NATS{cfg: cfg, log: log}, nil
}

// Connect opens the connection and does not return until JetStream has
// answered. nats retries the first dial itself, which covers the seconds a
// broker takes to accept connections after its container starts; this waits on
// that rather than writing the same loop again.
func (n *NATS) Connect(ctx context.Context) error {
	if n.conn != nil {
		return errors.New("messagequeue: already connected")
	}

	closed := make(chan struct{})
	conn, err := nats.Connect(n.cfg.URL,
		nats.Timeout(n.cfg.ConnectTimeout),
		nats.MaxReconnects(n.cfg.MaxReconnects),
		nats.ReconnectWait(n.cfg.ReconnectWait),
		nats.DrainTimeout(n.cfg.DrainTimeout),
		// Without this the first dial is fatal, and a broker that is merely
		// slow to start would take the process down with it.
		nats.RetryOnFailedConnect(true),
		nats.ClosedHandler(func(*nats.Conn) { close(closed) }),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			// A drain disconnects on purpose and reports no error. Warning
			// about it would put a line in every clean shutdown log.
			if err == nil {
				return
			}
			n.noteError(err)
			n.log.WarnContext(ctx, "nats disconnected", "err", err)
		}),
		nats.ReconnectErrHandler(func(_ *nats.Conn, err error) {
			n.noteError(err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			n.log.InfoContext(ctx, "nats reconnected", "url", c.ConnectedUrlRedacted())
		}),
	)
	if err != nil {
		// The url carries the password, so it must not reach the message.
		return fmt.Errorf("messagequeue: %w", n.redact(err))
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("messagequeue: jetstream: %w", n.redact(err))
	}

	n.conn, n.js, n.closed = conn, js, closed

	if err := n.waitReady(ctx); err != nil {
		conn.Close()
		n.conn, n.js, n.closed = nil, nil, nil
		return err
	}
	return nil
}

// Ping asks JetStream, not the socket. RetryOnFailedConnect hands back a
// connection that is still dialing, and a broker can be reachable with
// JetStream disabled on the account -- both look connected and neither can
// carry a message. The service talks to the JetStream API, so that is what a
// readiness check has to reach.
func (n *NATS) Ping(ctx context.Context) error {
	if n.js == nil {
		return errors.New("messagequeue: not connected")
	}

	ctx, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
	defer cancel()

	if _, err := n.js.AccountInfo(ctx); err != nil {
		return fmt.Errorf("messagequeue: %w", n.redact(err))
	}
	return nil
}

// Drain closes the subscriptions, waits for what is in flight to finish, and
// only then closes the connection. Closing outright would abandon a message a
// handler is halfway through: at-least-once means it comes back, so nothing is
// lost, but every deploy would resend whatever was in hand.
//
// Past DrainTimeout, closing is the last word. It is safe to call twice,
// because shutdown paths cross.
func (n *NATS) Drain(ctx context.Context) error {
	if n.conn == nil {
		return nil
	}

	conn, closed := n.conn, n.closed
	n.conn, n.js, n.closed = nil, nil, nil

	if err := conn.Drain(); err != nil {
		conn.Close()
		return fmt.Errorf("messagequeue: drain: %w", n.redact(err))
	}

	ctx, cancel := context.WithTimeout(ctx, n.cfg.DrainTimeout)
	defer cancel()

	select {
	case <-closed:
		return nil
	case <-ctx.Done():
		conn.Close()
		return fmt.Errorf("messagequeue: drain unfinished after %s", n.cfg.DrainTimeout)
	}
}

// Conn is the handle for anything that is not JetStream -- a request/reply, a
// plain subject. It is the driver's own type on purpose: infra hands out what
// it built.
func (n *NATS) Conn() *nats.Conn { return n.conn }

// JetStream is the handle a publisher or a consumer needs. The stream, its
// subjects and its consumers are this service's own vocabulary and are built by
// the adapter, not here.
func (n *NATS) JetStream() jetstream.JetStream { return n.js }

// waitReady covers the seconds between a container starting and the broker
// answering. nats is already retrying the dial; this is the process deciding
// how long to let it.
func (n *NATS) waitReady(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, n.cfg.ConnectTimeout)
	defer cancel()

	var last error
	for {
		if last = n.Ping(deadline); last == nil {
			return nil
		}
		if deadline.Err() != nil {
			return n.unreachable(last)
		}

		n.log.WarnContext(ctx, "nats not ready yet", "err", n.reason(last))

		select {
		case <-deadline.Done():
			return n.unreachable(last)
		case <-time.After(n.cfg.ReconnectWait):
		}
	}
}

func (n *NATS) unreachable(last error) error {
	return fmt.Errorf("messagequeue: not reachable in %s: %w",
		n.cfg.ConnectTimeout, n.reason(last))
}

// noteError keeps why an attempt failed. During the first connect,
// RetryOnFailedConnect reports the reason through ReconnectErrHandler and
// nowhere else -- not through LastError, not through the disconnect handler --
// so without this the process only ever learns that a deadline expired.
//
// The callback runs on the library's own goroutine, hence the lock.
func (n *NATS) noteError(err error) {
	n.mu.Lock()
	n.lastErr = err
	n.mu.Unlock()
}

// reason prefers what an attempt actually said over the deadline that ran out
// on it. It is the difference between "no servers available" and
// "authorization violation", and only one of those tells an operator what to do.
func (n *NATS) reason(fallback error) error {
	n.mu.Lock()
	last := n.lastErr
	n.mu.Unlock()

	if last != nil {
		return n.redact(last)
	}
	return fallback
}

// redact keeps the url out of an error. The url carries the password, and an
// error that quotes it whole would carry the password with it.
func (n *NATS) redact(err error) error {
	if n.cfg.URL == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), n.cfg.URL, "[REDACTED URL]"))
}
