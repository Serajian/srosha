package grpcserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/grpcserver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// served builds a gRPC server with something on it, so a test can actually call
// through and see the listener working rather than only see it bind.
func served() *grpc.Server {
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())
	return srv
}

func loopback() grpcserver.Config {
	return grpcserver.Config{Addr: "127.0.0.1:0", StopTimeout: 5 * time.Second}
}

// Every value here is an operational decision, and a missing one is a wiring
// mistake worth finding at boot rather than at the first call.
func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  grpcserver.Config
	}{
		{"no addr", grpcserver.Config{StopTimeout: time.Second}},
		{"blank addr", grpcserver.Config{Addr: "  ", StopTimeout: time.Second}},
		{"no stop timeout", grpcserver.Config{Addr: "127.0.0.1:0"}},
		{
			"negative stop timeout",
			grpcserver.Config{Addr: "127.0.0.1:0", StopTimeout: -time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := grpcserver.New(tt.cfg, served(), discard()); err == nil {
				t.Error("an incomplete config was accepted")
			}
		})
	}
}

func TestNewRefusesWhatItCannotRun(t *testing.T) {
	if _, err := grpcserver.New(loopback(), nil, discard()); err == nil {
		t.Error("a nil server was accepted")
	}
	if _, err := grpcserver.New(loopback(), served(), nil); err == nil {
		t.Error("a nil logger was accepted")
	}
}

func TestItServesAndThenStops(t *testing.T) {
	s, err := grpcserver.New(loopback(), served(), discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := s.Addr()
	if addr == "127.0.0.1:0" {
		t.Fatal("Addr() gave back the config rather than the bound port")
	}

	if err := check(t, addr); err != nil {
		t.Fatalf("it did not serve: %v", err)
	}

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := check(t, addr); err == nil {
		t.Error("it kept serving after shutdown")
	}
}

// Binding happens in Start and before it returns, so a port already taken is a
// startup failure rather than a log line the process survives.
func TestStartFailsOnAPortAlreadyTaken(t *testing.T) {
	var lc net.ListenConfig
	held, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer held.Close()

	cfg := grpcserver.Config{Addr: held.Addr().String(), StopTimeout: time.Second}
	s, err := grpcserver.New(cfg, served(), discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Start(t.Context()); err == nil {
		t.Fatal("Start() = nil on a port already taken")
	}
}

// Shutdown paths cross, so calling it twice -- or before anything started -- has
// to be uneventful.
func TestShutdownIsSafeTwiceAndBeforeStart(t *testing.T) {
	s, err := grpcserver.New(loopback(), served(), discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown before Start: %v", err)
	}

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

// An ordinary stop must not look like a listener that died. Run selects on this
// channel, and a shutdown reported here would stop the process on its own way
// out.
func TestShutdownIsNotReportedAsAServeFailure(t *testing.T) {
	s, err := grpcserver.New(loopback(), served(), discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-s.Err():
		t.Errorf("a clean shutdown was reported as a failure: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// check dials the server and calls something on it, so the test sees a listener
// that answers rather than one that merely holds a port.
func check(t *testing.T, addr string) error {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}
