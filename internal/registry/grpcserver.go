package registry

import (
	"context"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/grpcserver"

	"google.golang.org/grpc"
)

// GRPCServer binds and serves, and registers its shutdown at the top tier: it
// stops accepting first, so a call in flight still has underneath it everything
// it needs.
//
// The server itself is built by the adapter, already carrying its services and
// interceptors, and the listener is infra's. This only maps the service's
// settings onto what infra needs and remembers how to close it -- which is the
// whole of what this package does for every other dependency too.
func GRPCServer(
	ctx context.Context,
	name string,
	s settings.GRPC,
	srv *grpc.Server,
	res *Resources,
) (*grpcserver.Server, error) {
	server, err := grpcserver.New(grpcserver.Config{
		Addr:        s.Addr,
		StopTimeout: s.StopTimeout,
	}, srv, res.log)
	if err != nil {
		return nil, err
	}

	if err := server.Start(ctx); err != nil {
		return nil, err
	}

	res.add(step{
		tier:  tierServer,
		name:  name,
		close: server.Shutdown,
	})
	return server, nil
}
