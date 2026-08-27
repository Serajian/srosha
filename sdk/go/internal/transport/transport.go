// Package transport is how the client reaches srosha: the connection, the
// encryption, and the key on every call. It knows nothing about what is being
// sent.
package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Config is what opening a connection needs.
type Config struct {
	// Address is host:port.
	Address string

	// APIKey goes on every call.
	//
	// Never marshaled: a config struct ends up in a log line eventually, and
	// this is the one field in it that must not.
	APIKey string `json:"-"`

	// Insecure turns encryption off. It exists for a caller inside srosha's own
	// network, where the service listens without TLS -- and it is a field a
	// caller has to set rather than a default, because a default that is
	// insecure is what reaches production by accident.
	Insecure bool

	// TLSConfig is for a certificate this machine would not otherwise trust: a
	// private CA in staging. Ignored when Insecure is set.
	TLSConfig *tls.Config
}

// String keeps the key out of whatever this ends up inside.
func (c Config) String() string {
	return fmt.Sprintf("Config{Address:%q, Insecure:%t}", c.Address, c.Insecure)
}

func (c Config) validate() error {
	switch {
	case strings.TrimSpace(c.Address) == "":
		return errors.New("srosha: no address")
	case strings.TrimSpace(c.APIKey) == "":
		return errors.New("srosha: no api key")
	}
	return nil
}

// Dial opens the connection and connects to nothing: gRPC establishes it on the
// first call, so a wrong address is a failed request rather than a failed
// constructor.
func Dial(cfg Config, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(cfg.creds()),
		grpc.WithUnaryInterceptor(authenticate(cfg.APIKey)),
	}
	return grpc.NewClient(cfg.Address, append(opts, extra...)...)
}

func (c Config) creds() credentials.TransportCredentials {
	if c.Insecure {
		return insecure.NewCredentials()
	}
	if c.TLSConfig != nil {
		return credentials.NewTLS(c.TLSConfig)
	}
	return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
}

// authenticate puts the key on every call, so that no method has to remember
// to. The key is never logged here and never put in a request body: it belongs
// in metadata, where it is not part of anything that gets stored.
func authenticate(key string) grpc.UnaryClientInterceptor {
	header := bearerPrefix + key

	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, authHeader, header)
		return invoke(ctx, method, req, reply, cc, opts...)
	}
}
