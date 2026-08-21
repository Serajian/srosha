package bootstrap

import (
	"context"

	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/registry"
)

// Dispatcher opens what the dispatcher needs. On top of the gateway's two it
// calls out: to the sources' callbacks, and to the providers. Those are two
// clients rather than one, because only the callback address is chosen by
// somebody else.
func Dispatcher(ctx context.Context, cfg config.Dispatcher) (*App, error) {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryDispatcher)
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

	if _, err := registry.WebhookClient(cfg.HTTPClient, cfg.Webhook, res); err != nil {
		return abandon(ctx, res, err)
	}

	if _, err := registry.SenderClient(cfg.HTTPClient, res); err != nil {
		return abandon(ctx, res, err)
	}

	server, err := httpServer(ctx, binaryDispatcher, cfg.HTTP.Addr, cfg.HTTPServer, log, res)
	if err != nil {
		return abandon(ctx, res, err)
	}

	log.InfoContext(ctx,
		"dispatcher started",
		"service", cfg.App.ServiceName,
		"env", cfg.App.Env,
		"http", server.Addr(),
	)
	return &App{log: log, resources: res, server: server}, nil
}
