// Package telemetry builds what the process reports about itself. It knows how
// to write a log line and nothing about what this service logs.
package telemetry

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is bootstrap's job -- telemetry is the one dependency registry does not open,
// because registry needs a logger before it exists.
type Config struct {
	// Level and Format are the operator's. Text is for a person reading a
	// terminal; json is for the collector that parses it in production.
	Level  string
	Format string

	// Source adds the file and line to every record. It is worth having while
	// chasing something and costs real time at volume, so it is a decision
	// rather than a default.
	Source bool

	// Service and Binary go on every line. Both binaries write to one
	// collector, and without Binary nothing separates them there.
	Service string
	Binary  string
}

func (c Config) validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	_, known := parseLevel(c.Level)
	check(known, "level %q is not one of %s, %s, %s, %s",
		c.Level, levelDebug, levelInfo, levelWarn, levelError)
	check(c.Format == formatJSON || c.Format == formatText,
		"format %q is not %s or %s", c.Format, formatJSON, formatText)
	check(strings.TrimSpace(c.Service) != "", "service is empty")
	check(strings.TrimSpace(c.Binary) != "", "binary is empty")

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("telemetry: %w", errors.Join(errs...))
}

// NewLogger builds the logger and makes it the package default, so a library
// reaching for slog directly -- or code that forgot to take one -- lands in the
// same place and the same shape rather than in a second stream nobody watches.
//
// out is the caller's: stderr in a binary, so a container's log driver picks it
// up and nothing interleaves with real program output.
func NewLogger(cfg Config, out io.Writer) (*slog.Logger, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	level, _ := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level, AddSource: cfg.Source}

	var handler slog.Handler
	if cfg.Format == formatText {
		handler = slog.NewTextHandler(out, opts)
	} else {
		handler = slog.NewJSONHandler(out, opts)
	}

	log := slog.New(handler).With(
		slog.String("service", cfg.Service),
		slog.String("binary", cfg.Binary),
	)

	slog.SetDefault(log)
	return log, nil
}

func parseLevel(name string) (slog.Level, bool) {
	switch name {
	case levelDebug:
		return slog.LevelDebug, true
	case levelInfo:
		return slog.LevelInfo, true
	case levelWarn:
		return slog.LevelWarn, true
	case levelError:
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
