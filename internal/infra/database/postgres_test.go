package database_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/database"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// nowhere points at a port nothing listens on, so connecting always fails.
func nowhere() database.Config {
	return database.Config{
		DSN:               "postgres://srosha:hunter2@127.0.0.1:1/srosha?sslmode=disable",
		MaxConns:          10,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: 30 * time.Second,
		ConnectTimeout:    200 * time.Millisecond,
		ConnectAttempts:   3,
		ConnectBackoff:    10 * time.Millisecond,
	}
}

// New checks the wiring and dials nothing, so a mistake is found before any
// I/O happens.
func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*database.Config)
		want   string
	}{
		{"no dsn", func(c *database.Config) { c.DSN = " " }, "dsn"},
		{"no pool size", func(c *database.Config) { c.MaxConns = 0 }, "max conns"},
		{
			"pool size that wraps the driver's int32",
			func(c *database.Config) { c.MaxConns = 5_000_000_000 }, "max conns",
		},
		{
			"no connect timeout",
			func(c *database.Config) { c.ConnectTimeout = 0 },
			"connect timeout",
		},
		{"no attempts", func(c *database.Config) { c.ConnectAttempts = 0 }, "connect attempts"},
		{"no backoff", func(c *database.Config) { c.ConnectBackoff = 0 }, "connect backoff"},
		{"no lifetime", func(c *database.Config) { c.MaxConnLifetime = 0 }, "lifetime"},
		{"no idle time", func(c *database.Config) { c.MaxConnIdleTime = 0 }, "idle"},
		{"no health period", func(c *database.Config) { c.HealthCheckPeriod = 0 }, "health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := nowhere()
			tt.breaks(&cfg)

			_, err := database.New(cfg, discard())
			if err == nil {
				t.Fatal("New() accepted it")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

// Every problem at once, so one restart fixes all of them.
func TestNewReportsEveryProblemTogether(t *testing.T) {
	_, err := database.New(database.Config{}, discard())
	if err == nil {
		t.Fatal("New() accepted an empty config")
	}
	for _, want := range []string{"dsn", "max conns", "connect timeout", "connect attempts"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// A driver error quotes the string it was given, and that string holds the
// password.
func TestConnectDoesNotLeakTheDSN(t *testing.T) {
	cfg := nowhere()
	cfg.DSN = "postgres://srosha:hunter2@:::/srosha"

	p, err := database.New(cfg, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = p.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect() accepted a malformed dsn")
	}
	if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), cfg.DSN) {
		t.Errorf("the error carries the dsn: %v", err)
	}
}

func TestPingBeforeConnect(t *testing.T) {
	p, err := database.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := p.Ping(context.Background()); err == nil {
		t.Error("Ping() succeeded before Connect()")
	}
	if p.Pool() != nil {
		t.Error("Pool() gave something back before Connect()")
	}
}

// Shutdown paths cross, and the second caller must not panic.
func TestCloseIsSafeTwiceAndBeforeConnect(t *testing.T) {
	p, err := database.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	p.Close()
	p.Close()
}

// The retry loop exists for a container still starting. A canceled context
// must stop it at once rather than waiting out every attempt.
func TestACanceledContextStopsTheRetryLoop(t *testing.T) {
	cfg := nowhere()
	cfg.ConnectAttempts = 20
	cfg.ConnectBackoff = time.Second

	p, err := database.New(cfg, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if err := p.Connect(ctx); err == nil {
		t.Fatal("Connect() succeeded against nothing")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("took %v: the loop kept going after the context was canceled", took)
	}
}

// A failed Connect must leave nothing half-open behind it.
func TestAFailedConnectLeavesNoPool(t *testing.T) {
	p, err := database.New(nowhere(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := p.Connect(context.Background()); err == nil {
		t.Fatal("Connect() succeeded against nothing")
	}
	if p.Pool() != nil {
		t.Error("a pool was left behind after a failed connect")
	}
}
