package settings

import "github.com/Serajian/srosha/pkg/env"

type Telemetry struct {
	LogLevel string
}

func LoadTelemetry(r *env.Reader) Telemetry {
	t := Telemetry{LogLevel: r.Str("TELEMETRY_LOG_LEVEL", "info")}

	switch t.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		r.Check(false, "NOTIF_TELEMETRY_LOG_LEVEL must be debug, info, warn or error")
	}
	return t
}
