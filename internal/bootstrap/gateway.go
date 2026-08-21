package bootstrap

import (
	"context"

	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/registry"
)

// Gateway opens what the gateway needs: it accepts requests and publishes them,
// so it gets the database and the broker. It has no sender credentials and no
// callback secrets, and must not be given any.
func Gateway(ctx context.Context, cfg config.Gateway) (*App, error) {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryGateway)
	if err != nil {
		return nil, err
	}

	res := registry.New(log)

	if _, err := registry.Postgres(ctx, cfg.DB, res); err != nil {
		return abandon(ctx, res, err)
	}

	if _, err := registry.NATS(ctx, cfg.MQ, res); err != nil {
		return abandon(ctx, res, err)
	}

	server, err := httpServer(ctx, binaryGateway, cfg.GRPC.HTTPAddr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	// service and binary are already on every line, so naming them again here
	// would be a duplicate key in json and nothing at all in text, where the
	// handler drops them. env is the one thing this line adds.
	log.InfoContext(ctx, "gateway started", "env", cfg.App.Env, "http", server.Addr())
	return &App{log: log, resources: res, server: server}, nil
}
