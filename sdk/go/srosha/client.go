// Package srosha is the client for srosha, an asynchronous notification
// service. A source submits a message once and the service delivers it across
// its channels out of band, with at-least-once delivery and per-channel retry.
//
//	c, err := srosha.New(ctx, "srosha.acme.test:443", apiKey)
//	if err != nil {
//	    return err
//	}
//	defer c.Close()
//
//	r, err := c.Submit(ctx, srosha.Message{
//	    Title: "Your order shipped",
//	    Body:  "Tracking: 123",
//	    Routes: []srosha.Route{srosha.Email("a@b.test")},
//	})
//
// Nothing in this package's surface is protobuf or gRPC. Times are time.Time,
// failures are errors that errors.Is understands, and a listing is a range.
package srosha

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/Serajian/srosha/sdk/go/internal/retry"
	"github.com/Serajian/srosha/sdk/go/internal/transport"
	pb "github.com/Serajian/srosha/sdk/go/notification/v1"

	"google.golang.org/grpc"
)

// Client is a connection to srosha, authenticated as one source.
//
// Safe for concurrent use, and meant to be kept: it holds a connection, so
// building one per request throws away everything gRPC does about reconnection
// and load balancing.
type Client struct {
	conn *grpc.ClientConn

	notifications pb.NotificationServiceClient

	// Credentials is a source's sending identities: which bot, which mail
	// account, which signing key. Registered once and then never mentioned
	// again -- Submit names a channel, not an identity.
	Credentials *Credentials

	// Webhooks is where srosha pushes a delivery's final status.
	Webhooks *Webhooks

	timeout  time.Duration
	attempts int
}

// options is what New was told, before it becomes a Client.
type options struct {
	insecure  bool
	tlsConfig *tls.Config
	timeout   time.Duration
	attempts  int
	dial      []grpc.DialOption
}

// Option changes how the client connects or behaves.
type Option func(*options)

// WithInsecure turns encryption off.
//
// For a caller inside srosha's own network, where the service listens without
// TLS. It is a call a customer has to make rather than a default, because a
// default that is insecure is what reaches production by accident: the mistake
// should fall in the safe direction.
func WithInsecure() Option {
	return func(o *options) { o.insecure = true }
}

// WithTLSConfig trusts a certificate this machine otherwise would not -- a
// private CA in staging. Ignored alongside WithInsecure.
func WithTLSConfig(c *tls.Config) Option {
	return func(o *options) { o.tlsConfig = c }
}

// WithTimeout bounds a call whose context carries no deadline of its own.
func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

// WithRetry sets how many times a call is made in total. One means no retrying;
// zero and below are read as one.
//
// Retrying is safe here because every Submit carries an idempotency key,
// generated when the caller did not supply one.
func WithRetry(attempts int) Option {
	return func(o *options) { o.attempts = attempts }
}

// WithDialOptions passes gRPC options straight through, for whatever this
// package has not thought of. It is an escape hatch and is not covered by the
// stability this package otherwise promises.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(o *options) { o.dial = append(o.dial, opts...) }
}

// New connects to srosha and authenticates as whoever the key belongs to.
//
// It reaches nobody: gRPC establishes the connection on the first call, so a
// wrong address is a failed request rather than a failed constructor. The
// context is here for the day that changes.
func New(_ context.Context, address, apiKey string, opts ...Option) (*Client, error) {
	o := &options{timeout: defaultTimeout, attempts: retry.DefaultAttempts}
	for _, apply := range opts {
		apply(o)
	}

	conn, err := transport.Dial(transport.Config{
		Address:   address,
		APIKey:    apiKey,
		Insecure:  o.insecure,
		TLSConfig: o.tlsConfig,
	}, o.dial...)
	if err != nil {
		return nil, err
	}

	c := &Client{
		conn:          conn,
		notifications: pb.NewNotificationServiceClient(conn),
		timeout:       o.timeout,
		attempts:      o.attempts,
	}
	c.Credentials = &Credentials{client: c, api: pb.NewCredentialServiceClient(conn)}
	c.Webhooks = &Webhooks{client: c, api: pb.NewWebhookServiceClient(conn)}
	return c, nil
}

// Close releases the connection. A Client is finished after it.
func (c *Client) Close() error { return c.conn.Close() }

// call runs one request under this client's timeout and retry policy. Every
// method goes through it, so no method decides on its own what is worth
// repeating.
func (c *Client) call(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	return wrap(retry.Do(ctx, c.attempts, func() error { return fn(ctx) }))
}
