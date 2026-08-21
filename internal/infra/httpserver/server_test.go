package httpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/httpserver"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Port zero lets the kernel choose, so tests never collide over a fixed one.
func anywhere() httpserver.Config {
	return httpserver.Config{
		Addr:              "127.0.0.1:0",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      2 * time.Second,
		IdleTimeout:       5 * time.Second,
	}
}

func ok() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*httpserver.Config)
		want   string
	}{
		{"no addr", func(c *httpserver.Config) { c.Addr = " " }, "addr"},
		{
			"no read header timeout",
			func(c *httpserver.Config) { c.ReadHeaderTimeout = 0 },
			"read header timeout",
		},
		{"no read timeout", func(c *httpserver.Config) { c.ReadTimeout = 0 }, "read timeout"},
		{"no write timeout", func(c *httpserver.Config) { c.WriteTimeout = 0 }, "write timeout"},
		{"no idle timeout", func(c *httpserver.Config) { c.IdleTimeout = 0 }, "idle timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := anywhere()
			tt.breaks(&cfg)

			if _, err := httpserver.New(cfg, ok(), discard()); err == nil {
				t.Fatal("New() accepted it")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

func TestNewRefusesNoHandler(t *testing.T) {
	if _, err := httpserver.New(anywhere(), nil, discard()); err == nil {
		t.Fatal("New() accepted a server with nothing to serve")
	}
}

func TestItServesAndThenStops(t *testing.T) {
	s, err := httpserver.New(anywhere(), ok(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	resp, err := http.Get("http://" + s.Addr()) //nolint:noctx // no context to carry here
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if after, err := http.Get("http://" + resp.Request.URL.Host); err == nil { //nolint:noctx // as above
		after.Body.Close()
		t.Error("it kept serving after shutdown")
	}
}

// A port already taken is a startup failure. If Start served in the background
// without binding first, it would be a log line the process survives -- a
// gateway that came up healthy and listened on nothing.
func TestStartFailsOnAPortAlreadyTaken(t *testing.T) {
	var lc net.ListenConfig
	held, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()

	cfg := anywhere()
	cfg.Addr = held.Addr().String()

	s, err := httpserver.New(cfg, ok(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() took a port that was already held")
	}
}

// Shutdown paths cross, and the second caller must not fail.
func TestShutdownIsSafeTwiceAndBeforeStart(t *testing.T) {
	s, err := httpserver.New(anywhere(), ok(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("shutdown before start: %v", err)
	}

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("first shutdown: %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("second shutdown: %v", err)
	}
}

// An ordinary shutdown is not a failure, and must not wake whoever is watching
// for one.
func TestShutdownIsNotReportedAsAServeFailure(t *testing.T) {
	s, err := httpserver.New(anywhere(), ok(), discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case err := <-s.Err():
		t.Errorf("a clean shutdown was reported as a failure: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}
