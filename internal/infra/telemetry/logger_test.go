package telemetry_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/infra/telemetry"
)

func sane() telemetry.Config {
	return telemetry.Config{
		Level:   "info",
		Format:  "json",
		Service: "srosha",
		Binary:  "gateway",
	}
}

func TestNewLoggerRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*telemetry.Config)
		want   string
	}{
		{"unknown level", func(c *telemetry.Config) { c.Level = "verbose" }, "level"},
		{"no level", func(c *telemetry.Config) { c.Level = "" }, "level"},
		{"unknown format", func(c *telemetry.Config) { c.Format = "logfmt" }, "format"},
		{"no service", func(c *telemetry.Config) { c.Service = " " }, "service"},
		{"no binary", func(c *telemetry.Config) { c.Binary = "" }, "binary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := sane()
			tt.breaks(&cfg)

			if _, err := telemetry.NewLogger(cfg, &bytes.Buffer{}); err == nil {
				t.Fatal("NewLogger() accepted it")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

func TestNewLoggerReportsEveryProblemTogether(t *testing.T) {
	_, err := telemetry.NewLogger(telemetry.Config{}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("NewLogger() accepted an empty config")
	}
	for _, want := range []string{"level", "format", "service", "binary"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// Both binaries write to one collector, so every line has to say which one it
// came from.
func TestEveryLineNamesTheServiceAndTheBinary(t *testing.T) {
	var out bytes.Buffer

	log, err := telemetry.NewLogger(sane(), &out)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	log.Info("started")

	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("the line is not json: %v\n%s", err, out.String())
	}
	if record["service"] != "srosha" {
		t.Errorf("service = %v, want srosha", record["service"])
	}
	if record["binary"] != "gateway" {
		t.Errorf("binary = %v, want gateway", record["binary"])
	}
}

func TestTextFormatIsNotJSON(t *testing.T) {
	var out bytes.Buffer

	cfg := sane()
	cfg.Format = "text"

	log, err := telemetry.NewLogger(cfg, &out)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	log.Info("started")

	if strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
		t.Errorf("text format produced json: %s", out.String())
	}
	// service and binary are left out on purpose: they are there so one
	// collector can tell two processes apart, and a person at a terminal
	// already knows which one they started.
	for _, noise := range []string{"service=", "binary="} {
		if strings.Contains(out.String(), noise) {
			t.Errorf("text format still carries %q: %s", noise, out.String())
		}
	}
	if !strings.Contains(out.String(), "started") {
		t.Errorf("text format dropped the message: %s", out.String())
	}
}

// The date and the offset belong in the record, not on a terminal.
func TestTextFormatShowsTheClockOnly(t *testing.T) {
	var out bytes.Buffer

	cfg := sane()
	cfg.Format = "text"

	log, err := telemetry.NewLogger(cfg, &out)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	log.Info("started")

	line := out.String()
	if strings.Contains(line, "T") && strings.Contains(line, "+") {
		t.Errorf("text format still carries the full timestamp: %s", line)
	}
	if !regexp.MustCompile(`time=\d{2}:\d{2}:\d{2}\.\d{3}`).MatchString(line) {
		t.Errorf("time is not a plain clock: %s", line)
	}
}

// The level is what keeps a production log readable, so it has to actually
// filter.
func TestTheLevelFilters(t *testing.T) {
	var out bytes.Buffer

	cfg := sane()
	cfg.Level = "warn"

	log, err := telemetry.NewLogger(cfg, &out)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	log.Info("this is below the bar")
	if out.Len() != 0 {
		t.Errorf("info was written at level warn: %s", out.String())
	}

	log.Warn("this is not")
	if !strings.Contains(out.String(), "this is not") {
		t.Errorf("warn was dropped at level warn: %s", out.String())
	}
}

func TestSourceIsOffUnlessAskedFor(t *testing.T) {
	var quiet, loud bytes.Buffer

	off, err := telemetry.NewLogger(sane(), &quiet)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	off.Info("started")

	cfg := sane()
	cfg.Source = true

	on, err := telemetry.NewLogger(cfg, &loud)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	on.Info("started")

	if strings.Contains(quiet.String(), "logger_test.go") {
		t.Errorf("source appeared without being asked for: %s", quiet.String())
	}
	if !strings.Contains(loud.String(), "logger_test.go") {
		t.Errorf("source was asked for and did not appear: %s", loud.String())
	}
}

// A library reaching for the package-level slog, or code that forgot to take a
// logger, must land here too -- not in a second stream nobody watches.
func TestThePackageDefaultGoesToTheSamePlace(t *testing.T) {
	var out bytes.Buffer

	if _, err := telemetry.NewLogger(sane(), &out); err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	slog.Info("from somewhere that never took a logger")

	if !strings.Contains(out.String(), "never took a logger") {
		t.Errorf("the package default went elsewhere: %q", out.String())
	}
	if !strings.Contains(out.String(), `"binary":"gateway"`) {
		t.Errorf("the package default lost the attributes: %s", out.String())
	}
}
