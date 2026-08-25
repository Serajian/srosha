package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type GRPC struct {
	Addr     string
	HTTPAddr string

	// StopTimeout is how long a graceful stop waits for the calls in flight.
	// Past it the connections are cut: the gRPC server is the first thing to
	// close, and everything below it in the shutdown order is waiting.
	//
	// Keep it under NOTIF_APP_SHUTDOWN_TIMEOUT, which is the budget for the
	// whole of shutdown and not just this part of it.
	StopTimeout time.Duration
}

func LoadGRPC(r *env.Reader) GRPC {
	g := GRPC{
		Addr:        r.Str("GRPC_ADDR", ":50051"),
		HTTPAddr:    r.Str("GRPC_HTTP_ADDR", ":8080"),
		StopTimeout: r.Duration("GRPC_STOP_TIMEOUT", 10*time.Second),
	}

	// Zero would cut every call the instant a shutdown started, which is not a
	// graceful stop at all.
	r.Check(g.StopTimeout > 0, "NOTIF_GRPC_STOP_TIMEOUT must be above zero")
	return g
}
