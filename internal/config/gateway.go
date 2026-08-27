package config

import (
	"fmt"
	"time"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Gateway is everything the gateway binary needs, and nothing else. A single
// shared config would make it refuse to start over a Telegram token it never
// uses.
type Gateway struct {
	App        settings.App
	GRPC       settings.GRPC
	Auth       settings.Auth
	HTTPServer settings.HTTPServer
	DB         settings.DB
	MQ         settings.MQ
	RateLimit  settings.RateLimit

	// RetentionAge is the dispatcher's number, read here too. The gateway never
	// deletes anything -- it refuses a listing that reaches past what is left,
	// because serving one would hand back a short page with nothing saying so.
	RetentionAge time.Duration

	// The gateway seals a sending credential when a source registers one, and
	// the cipher is symmetric: holding the key to seal is holding the key to
	// open. That is accepted rather than overlooked -- the threat this guards
	// against is a database dump, and the gateway already reads those rows with
	// the same connection string. It is not a license to read them here.
	Crypto settings.Crypto

	// The gateway validates a callback address when a source registers one, so
	// it needs the policy -- and nothing else about webhooks. The signing
	// secrets are the dispatcher's and must not be loaded here.
	WebhookPolicy settings.WebhookPolicy
	Telemetry     settings.Telemetry
}

// LoadGateway reads the environment and reports everything wrong with it at
// once. Failing here is the point: a missing DSN found at boot is an outage of
// seconds, and the same DSN found on the first request is an outage nobody was
// watching for.
func LoadGateway() (Gateway, error) {
	r := reader("gateway")

	app := settings.LoadApp(r)
	c := Gateway{
		App:          app,
		RetentionAge: settings.LoadRetentionAge(r),
		GRPC:         settings.LoadGRPC(r),
		Auth:         settings.LoadAuth(r),
		HTTPServer:   settings.LoadHTTPServer(r),
		DB:           settings.LoadDB(r),
		MQ:           settings.LoadMQ(r),
		RateLimit:    settings.LoadRateLimit(r),
		Crypto:       settings.LoadCrypto(r),

		WebhookPolicy: settings.LoadWebhookPolicy(r, app.IsProduction()),
		Telemetry:     settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Gateway{}, fmt.Errorf("gateway configuration: %w", err)
	}
	return c, nil
}
