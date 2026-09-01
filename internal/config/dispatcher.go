package config

import (
	"fmt"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Dispatcher is everything the dispatcher binary needs. It has the sending
// credentials and the callback secrets; the gateway has neither, and must not.
type Dispatcher struct {
	App        settings.App
	HTTP       settings.HTTP
	HTTPServer settings.HTTPServer
	HTTPClient settings.HTTPClient
	DB         settings.DB
	MQ         settings.MQ
	Sender     settings.Sender
	Crypto     settings.Crypto
	Webhook    settings.Webhook
	Dispatch   settings.Dispatch
	Retention  settings.Retention

	// Alert is the operator's own channel, reached directly rather than
	// through this service's pipeline. Empty means off.
	Alert settings.Alert

	Telemetry settings.Telemetry
}

func LoadDispatcher() (Dispatcher, error) {
	r := reader("dispatcher")

	app := settings.LoadApp(r)
	dispatch := settings.LoadDispatch(r)

	c := Dispatcher{
		App:        app,
		HTTP:       settings.LoadHTTP(r),
		HTTPServer: settings.LoadHTTPServer(r),
		HTTPClient: settings.LoadHTTPClient(r),
		DB:         settings.LoadDB(r),
		MQ:         settings.LoadMQ(r),
		Sender:     settings.LoadSender(r),
		Crypto:     settings.LoadCrypto(r),
		Webhook:    settings.LoadWebhook(r, app.IsProduction()),
		Dispatch:   dispatch,
		Retention:  settings.LoadRetention(r, dispatch),
		Alert:      settings.LoadAlert(r),
		Telemetry:  settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Dispatcher{}, fmt.Errorf("dispatcher configuration: %w", err)
	}
	return c, nil
}
