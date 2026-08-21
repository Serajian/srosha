package settings

import "github.com/Serajian/srosha/pkg/env"

type Telemetry struct {
	LogLevel string

	// LogFormat is text for a person reading a terminal, json for the collector
	// that parses it in production.
	LogFormat string

	// LogSource adds the file and line to every record: worth having while
	// chasing something, and real cost at volume.
	LogSource bool
}

// LoadTelemetry defaults the format to what the reader of the logs actually is:
// a collector in production, a person at a terminal anywhere else. Setting
// NOTIF_TELEMETRY_LOG_FORMAT still wins over both.
func LoadTelemetry(r *env.Reader, production bool) Telemetry {
	format := "text"
	if production {
		format = "json"
	}

	t := Telemetry{
		LogLevel:  r.Str("TELEMETRY_LOG_LEVEL", "info"),
		LogFormat: r.Str("TELEMETRY_LOG_FORMAT", format),
		LogSource: r.Bool("TELEMETRY_LOG_SOURCE", false),
	}

	switch t.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		r.Check(false, "NOTIF_TELEMETRY_LOG_LEVEL must be debug, info, warn or error")
	}

	switch t.LogFormat {
	case "json", "text":
	default:
		r.Check(false, "NOTIF_TELEMETRY_LOG_FORMAT must be json or text")
	}
	return t
}
