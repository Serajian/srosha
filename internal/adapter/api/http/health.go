package http

import (
	"encoding/json"
	nethttp "net/http"
)

// report is what /readyz answers with. The binary is in the body rather than in
// the path, so both binaries keep the standard endpoint names and an operator
// holding one response still knows which process produced it.
type report struct {
	Binary string        `json:"binary"`
	Status string        `json:"status"`
	Checks []checkStatus `json:"checks"`
}

type checkStatus struct {
	Name string `json:"name"`
	// Status, not the error. The reason names our dependencies and the
	// addresses they live at, and this port is reachable from further away than
	// its author expects. The reason goes to the log.
	Status string `json:"status"`
}

const (
	statusReady    = "ready"
	statusNotReady = "not ready"
	statusUp       = "up"
	statusDown     = "down"
)

// mountHealth adds the two questions an orchestrator asks, which are
// deliberately different questions.
func mountHealth(mux *nethttp.ServeMux, d Deps) {
	// Liveness asks whether the process is still itself. A dependency being
	// down is not an answer to that: restarting does not bring postgres back,
	// and a restart loop buries the real fault under noise.
	mux.HandleFunc("GET /healthz", func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.WriteHeader(nethttp.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Readiness asks whether it can serve. A process that cannot reach its
	// database should be taken out of rotation and left running.
	mux.HandleFunc("GET /readyz", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		checks := d.Ready(r.Context())

		out := report{
			Binary: d.Binary,
			Status: statusReady,
			Checks: make([]checkStatus, 0, len(checks)),
		}

		for _, c := range checks {
			status := statusUp
			if c.Err != nil {
				status = statusDown
				out.Status = statusNotReady

				d.Log.WarnContext(r.Context(), "dependency is not ready",
					"name", c.Name, "err", c.Err)
			}
			out.Checks = append(out.Checks, checkStatus{Name: c.Name, Status: status})
		}

		code := nethttp.StatusOK
		if out.Status != statusReady {
			code = nethttp.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(out)
	})
}
