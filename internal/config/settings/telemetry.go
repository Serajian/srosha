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

func LoadTelemetry(r *env.Reader) Telemetry {
	t := Telemetry{
		LogLevel:  r.Str("TELEMETRY_LOG_LEVEL", "info"),
		LogFormat: r.Str("TELEMETRY_LOG_FORMAT", "json"),
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
