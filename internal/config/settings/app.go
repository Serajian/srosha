// Package settings holds one group of configuration per file. A new key is
// added to the group it belongs to and nowhere else.
package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type App struct {
	Env             string
	ServiceName     string
	ShutdownTimeout time.Duration
}

func LoadApp(r *env.Reader) App {
	return App{
		Env:             r.Str("APP_ENV", "development"),
		ServiceName:     r.Str("APP_SERVICE_NAME", "srosha"),
		ShutdownTimeout: r.Duration("APP_SHUTDOWN_TIMEOUT", 15*time.Second),
	}
}

// IsProduction gates the checks that must not be relaxed on a real deployment.
func (a App) IsProduction() bool { return a.Env == "production" }
