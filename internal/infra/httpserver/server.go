// Package httpserver runs an http listener and owns its lifecycle. It knows
// nothing about the routes it serves.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is registry's job.
//
// Nothing here has a default. Every value is an operational decision, so it
// comes from config and is named in one place rather than two.
type Config struct {
	Addr string

	// ReadHeaderTimeout is the one that matters for safety: without it a client
	// can hold a connection open by sending headers a byte at a time.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func (c Config) validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(strings.TrimSpace(c.Addr) != "", "addr is empty")
	check(c.ReadHeaderTimeout > 0, "read header timeout must be above zero")
	check(c.ReadTimeout > 0, "read timeout must be above zero")
	check(c.WriteTimeout > 0, "write timeout must be above zero")
	check(c.IdleTimeout > 0, "idle timeout must be above zero")

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("httpserver: %w", errors.Join(errs...))
}

// Server owns the listener: it binds it, serves on it and shuts it down.
type Server struct {
	cfg Config
	log *slog.Logger
	srv *http.Server
	ln  net.Listener

	// errs carries a serve failure that was not a shutdown. Nothing else
	// notices a listener that dies, so whoever runs the process selects on it.
	errs chan error
}

// New checks the configuration and binds nothing. Start does that, so a wiring
// mistake is found before a port is taken.
func New(cfg Config, h http.Handler, log *slog.Logger) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if h == nil {
		return nil, errors.New("httpserver: no handler")
	}

	return &Server{
		cfg: cfg,
		log: log,
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           h,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		errs: make(chan error, 1),
	}, nil
}

// Start binds the listener before it returns and only then serves in the
// background. Binding first is the point: a port already taken is a startup
// failure, and serving in a goroutine would turn it into a log line the process
// survives.
func (s *Server) Start(ctx context.Context) error {
	if s.ln != nil {
		return errors.New("httpserver: already started")
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("httpserver: listen on %s: %w", s.cfg.Addr, err)
	}
	s.ln = ln

	go func() {
		// ErrServerClosed is what Shutdown produces, so it is not a failure.
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.errs <- fmt.Errorf("httpserver: %w", err)
		}
	}()

	s.log.InfoContext(ctx, "http listening", "addr", ln.Addr().String())
	return nil
}

// Shutdown stops accepting and lets the requests in flight finish. It is safe
// to call twice, because shutdown paths cross.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.ln == nil {
		return nil
	}
	s.ln = nil

	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("httpserver: shutdown: %w", err)
	}
	return nil
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
