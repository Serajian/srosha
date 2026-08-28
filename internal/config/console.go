package config

import (
	"fmt"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Console is everything the console binary needs, and nothing else. It has no
// broker, no sending credentials and no callback secrets: it serves pages and
// reads rows.
type Console struct {
	App        settings.App
	HTTPServer settings.HTTPServer
	HTTP       settings.HTTP
	DB         settings.DB
	Console    settings.Console
	Telemetry  settings.Telemetry
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
		DB:         settings.LoadDB(r),
		Console:    settings.LoadConsole(r, app.IsProduction()),
		Telemetry:  settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Console{}, fmt.Errorf("console configuration: %w", err)
	}
	return c, nil
}
