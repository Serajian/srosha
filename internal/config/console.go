package config

import (
	"fmt"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Console is everything the console binary needs, and nothing else. It has no
// broker and no callback secrets: it serves pages and reads rows.
//
// It holds no sending identity of its own -- the registry it builds has an
// empty Fallback -- but it does open a source's own, to send a trial message.
// That is why it needs an http client.
type Console struct {
	App        settings.App
	HTTPServer settings.HTTPServer
	HTTP       settings.HTTP
	HTTPClient settings.HTTPClient
	DB         settings.DB
	Console    settings.Console

	// The console seals a sending credential when a customer registers one, and
	// issues a callback's signing secret. Both use the keyring, so it holds the
	// same keys the gateway does -- see ARCHITECTURE.md on why that widening is
	// accepted rather than overlooked.
	Crypto settings.Crypto

	// It validates a callback address when a customer registers one, so it
	// needs the policy and nothing else about webhooks.
	WebhookPolicy settings.WebhookPolicy

	// Alert is the operator's own channel, reached directly rather than
	// through this service's pipeline. Empty means off.
	Alert settings.Alert

	Telemetry settings.Telemetry
}

// LoadConsole reads the environment and reports everything wrong with it at
// once, the way the other two binaries do.
func LoadConsole() (Console, error) {
	r := reader("console")

	app := settings.LoadApp(r)
	c := Console{
		App:        app,
		HTTPServer: settings.LoadHTTPServer(r),
		HTTP:       settings.LoadHTTP(r),
		HTTPClient: settings.LoadHTTPClient(r),
		DB:         settings.LoadDB(r),
		Console:    settings.LoadConsole(r, app.IsProduction()),
		Crypto:     settings.LoadCrypto(r),

		WebhookPolicy: settings.LoadWebhookPolicy(r, app.IsProduction()),
		Alert:         settings.LoadAlert(r),
		Telemetry:     settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Console{}, fmt.Errorf("console configuration: %w", err)
	}
	return c, nil
}
