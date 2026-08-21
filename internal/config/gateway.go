package config

import (
	"fmt"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Gateway is everything the gateway binary needs, and nothing else. A single
// shared config would make it refuse to start over a Telegram token it never
// uses.
type Gateway struct {
	App        settings.App
	GRPC       settings.GRPC
	HTTPServer settings.HTTPServer
	DB         settings.DB
	MQ         settings.MQ
	RateLimit  settings.RateLimit
	Telemetry  settings.Telemetry
}

// LoadGateway reads the environment and reports everything wrong with it at
// once. Failing here is the point: a missing DSN found at boot is an outage of
// seconds, and the same DSN found on the first request is an outage nobody was
// watching for.
func LoadGateway() (Gateway, error) {
	r := reader("gateway")

	app := settings.LoadApp(r)
	c := Gateway{
		App:        app,
		GRPC:       settings.LoadGRPC(r),
		HTTPServer: settings.LoadHTTPServer(r),
		DB:         settings.LoadDB(r),
		MQ:         settings.LoadMQ(r),
		RateLimit:  settings.LoadRateLimit(r),
		Telemetry:  settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Gateway{}, fmt.Errorf("gateway configuration: %w", err)
	}
	return c, nil
}
