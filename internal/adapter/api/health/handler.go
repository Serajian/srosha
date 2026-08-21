// Package health answers the two questions an orchestrator asks about a
// process. It is the only inbound surface that exists before the service has
// an API.
package health

import (
	"context"
	"log/slog"
	"net/http"
)

// Handler serves liveness and readiness, which are deliberately different
// questions.
//
// ready is whatever knows what is open -- the handler does not, and must not:
// it is an adapter, and the list of open dependencies belongs to bootstrap.
func Handler(ready func(context.Context) error, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Liveness asks whether the process is still itself. A dependency being
	// down is not an answer to that: restarting does not bring postgres back,
	// and a restart loop buries the real fault under noise.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Readiness asks whether it can serve. A process that cannot reach its
	// database should be taken out of rotation and left running.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := ready(r.Context()); err != nil {
			// The reason goes to the log, not to the body. This port can be
			// reachable from further away than its author expects, and the
			// reason names our dependencies and how they failed.
			log.WarnContext(r.Context(), "not ready", "err", err)

			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready\n"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	return mux
}
