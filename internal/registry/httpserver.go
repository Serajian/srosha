package registry

import (
	"context"
	"net/http"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/httpserver"
)

// HTTPServer binds and serves, and registers its shutdown at the top tier: it
// stops accepting first, so a request in flight still has underneath it
// everything it needs.
//
// addr is passed rather than read from a settings group, because where each
// binary listens is its own: GRPC.HTTPAddr for the gateway, HTTP.Addr for the
// dispatcher.
func HTTPServer(
	ctx context.Context,
	name string,
	addr string,
	s settings.HTTPServer,
	h http.Handler,
	res *Resources,
) (*httpserver.Server, error) {
	server, err := httpserver.New(httpserver.Config{
		Addr:              addr,
		ReadHeaderTimeout: s.ReadHeaderTimeout,
		ReadTimeout:       s.ReadTimeout,
		WriteTimeout:      s.WriteTimeout,
		IdleTimeout:       s.IdleTimeout,
	}, h, res.log)
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
