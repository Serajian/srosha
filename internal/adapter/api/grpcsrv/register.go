package grpcsrv

import (
	"errors"
	"log/slog"

	pb "github.com/Serajian/srosha/gen/notification/v1"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/pkg/errs"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Deps is what the gRPC surface needs. Everything in it was built by bootstrap;
// this package opens nothing and reads no config.
type Deps struct {
	Notifications *NotificationServer
	Webhooks      *WebhookServer

	// Authn and Scheme are the auth interceptor's. The scheme knows what a key
	// looks like, the authenticator knows whose it is.
	Authn  *source.Authenticator
	Scheme KeyScheme

	Log *slog.Logger

	// Reflection lets a client ask the server what it serves, so grpcurl and
	// Postman work with no proto files in hand.
	//
	// It is off in production and the caller decides, because reflection is a
	// STREAMING method: the interceptors below are unary and do not run on it,
	// so a server with it on hands its whole API surface -- every service,
	// method, message and field -- to anyone who can reach the port, with no
	// key and no log line that looks like a request.
	//
	// Off, a client needs the proto files, which every real client already has.
	Reflection bool
}

func (d Deps) validate() error {
	var missing []error

	if d.Notifications == nil {
		missing = append(missing, errs.InternalErr("no notification server"))
	}
	if d.Webhooks == nil {
		missing = append(missing, errs.InternalErr("no webhook server"))
	}
	if d.Authn == nil {
		missing = append(missing, errs.InternalErr("no authenticator"))
	}
	if d.Scheme == nil {
		missing = append(missing, errs.InternalErr("no key scheme"))
	}
	if d.Log == nil {
		missing = append(missing, errs.InternalErr("no logger"))
	}

	if len(missing) == 0 {
		return nil
	}
	return errs.InternalErr("grpc server is not fully wired").
		WithErr(errors.Join(missing...))
}

// New builds the server with every service mounted and every interceptor in
// place. Every rpc this service answers is registered from here, so nobody has
// to grep the package to find out what it serves.
//
// Constructing it and running it are deliberately apart: this returns a server
// that has not been listened on, and starting and stopping it is the registry's,
// which is the only place that knows the order things shut down in.
func New(d Deps) (*grpc.Server, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	// Outermost first. The order is the decision -- see interceptors.go.
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			Recovery(d.Log),
			Logging(d.Log),
			Errors(),
			Auth(d.Authn, d.Scheme, d.Log),
		),
	)

	pb.RegisterNotificationServiceServer(server, d.Notifications)
	pb.RegisterWebhookServiceServer(server, d.Webhooks)

	if d.Reflection {
		reflection.Register(server)
	}
	return server, nil
}
