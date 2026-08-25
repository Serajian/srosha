// Package grpcserver runs a gRPC listener and owns its lifecycle. It knows
// nothing about the services registered on it, exactly as httpserver knows
// nothing about the routes it serves.
package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is registry's job.
type Config struct {
	Addr string

	// StopTimeout bounds the graceful stop. Past it the connections are cut
	// rather than holding the whole shutdown hostage to one slow caller.
	StopTimeout time.Duration
}

func (c Config) validate() error {
	var errs []error

	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, errors.New("addr is empty"))
	}
	if c.StopTimeout <= 0 {
		errs = append(errs, errors.New("stop timeout must be above zero"))
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("grpcserver: %w", errors.Join(errs...))
}

// Server owns the listener: it binds it, serves on it and shuts it down.
//
// The *grpc.Server is built elsewhere, already carrying its services and its
// interceptors. This package would otherwise have to know what srosha serves,
// which is the same line httpserver draws at http.Handler.
type Server struct {
	cfg Config
	log *slog.Logger
	srv *grpc.Server
	ln  net.Listener

	// errs carries a serve failure that was not a shutdown. Nothing else
	// notices a listener that dies, so whoever runs the process selects on it.
	errs chan error
}

// New checks the configuration and binds nothing. Start does that, so a wiring
// mistake is found before a port is taken.
func New(cfg Config, srv *grpc.Server, log *slog.Logger) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if srv == nil {
		return nil, errors.New("grpcserver: no server")
	}
	if log == nil {
		return nil, errors.New("grpcserver: no logger")
	}

	return &Server{cfg: cfg, log: log, srv: srv, errs: make(chan error, 1)}, nil
}

// Start binds the listener before it returns and only then serves in the
// background. Binding first is the point: a port already taken is a startup
// failure, and serving in a goroutine would turn it into a log line the process
// survives.
func (s *Server) Start(ctx context.Context) error {
	if s.ln != nil {
		return errors.New("grpcserver: already started")
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("grpcserver: listen on %s: %w", s.cfg.Addr, err)
	}
	s.ln = ln

	go func() {
		// ErrServerStopped is what Shutdown produces, so it is not a failure.
		if err = s.srv.Serve(ln); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			s.errs <- fmt.Errorf("grpcserver: %w", err)
		}
	}()

	s.log.InfoContext(ctx, "grpc listening", "addr", ln.Addr().String())
	return nil
}

// Shutdown stops accepting and lets the calls in flight finish, unless the
// context or the configured budget runs out first -- then it cuts them. Waiting
// forever on one slow caller would hold up everything below this in the
// shutdown order.
//
// It is safe to call twice, because shutdown paths cross.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.ln == nil {
		return nil
	}
	s.ln = nil

	ctx, cancel := context.WithTimeout(ctx, s.cfg.StopTimeout)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		s.srv.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		s.log.WarnContext(ctx, "grpc did not stop in time, cutting connections")
		s.srv.Stop()
		<-stopped
		return fmt.Errorf("grpcserver: shutdown: %w", ctx.Err())
	}
}

// Addr is the address actually bound, which is not Config.Addr when the port
// was left to the kernel.
func (s *Server) Addr() string {
	if s.ln == nil {
		return s.cfg.Addr
	}
	return s.ln.Addr().String()
}

// Err reports a serve failure. It never carries the ordinary shutdown.
func (s *Server) Err() <-chan error { return s.errs }
