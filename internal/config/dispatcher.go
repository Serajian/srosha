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
	HTTPClient settings.HTTPClient
	DB         settings.DB
	MQ         settings.MQ
	Sender     settings.Sender
	Webhook    settings.Webhook
	Dispatch   settings.Dispatch
	Telemetry  settings.Telemetry
}

func LoadDispatcher() (Dispatcher, error) {
	r := reader("dispatcher")

	app := settings.LoadApp(r)
	c := Dispatcher{
		App:        app,
		HTTP:       settings.LoadHTTP(r),
		HTTPClient: settings.LoadHTTPClient(r),
		DB:         settings.LoadDB(r),
		MQ:         settings.LoadMQ(r),
		Sender:     settings.LoadSender(r),
		Webhook:    settings.LoadWebhook(r, app.IsProduction()),
		Dispatch:   settings.LoadDispatch(r),
		Telemetry:  settings.LoadTelemetry(r),
	}

	if err := r.Err(); err != nil {
		return Dispatcher{}, fmt.Errorf("dispatcher configuration: %w", err)
	}
	return c, nil
}
