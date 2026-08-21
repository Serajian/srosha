package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// HTTPServer is how the listeners behave, shared by both binaries. Where each
// one listens is its own: GRPC.HTTPAddr for the gateway, HTTP.Addr for the
// dispatcher.
type HTTPServer struct {
	// ReadHeaderTimeout is the one that matters for safety: without it a client
	// can hold a connection open by sending headers a byte at a time.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func LoadHTTPServer(r *env.Reader) HTTPServer {
	return HTTPServer{
		ReadHeaderTimeout: r.Duration("HTTP_SERVER_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       r.Duration("HTTP_SERVER_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:      r.Duration("HTTP_SERVER_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:       r.Duration("HTTP_SERVER_IDLE_TIMEOUT", 60*time.Second),
	}
}
