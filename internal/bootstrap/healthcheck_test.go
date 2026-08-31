package bootstrap_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Serajian/srosha/internal/bootstrap"
)

// Probe asks the readiness endpoint, which already answers 503 when a
// dependency is down. It adds no judgement of its own: the question was
// answered in health.go and asking it twice is how two answers start to
// disagree.
func TestProbeReportsWhatReadyzSaid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		wantErr bool
	}{
		{"ready", http.StatusOK, false},
		{"a dependency is down", http.StatusServiceUnavailable, true},
		{"something else entirely", http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/readyz" {
						t.Errorf("asked for %q, want /readyz", r.URL.Path)
					}
					w.WriteHeader(tc.code)
				}))
			defer srv.Close()

			err := bootstrap.Probe(strings.TrimPrefix(srv.URL, "http://"))
			if tc.wantErr && err == nil {
				t.Error("Probe said ready when the endpoint did not")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Probe = %v, want nil", err)
			}
		})
	}
}

// A listener that is not there is not ready. This is the case that runs while
// a container is still starting, so it must be an error and not a panic.
func TestProbeRefusesAClosedPort(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close()

	if err := bootstrap.Probe(addr); err == nil {
		t.Error("Probe said ready with nothing listening")
	}
}

// The address comes from the same config the server binds, and a server binds
// ":8080" to mean every interface. A client cannot dial that, so the host has
// to be filled in -- and it must be loopback, because the probe runs inside the
// container it is asking about.
func TestProbeDialsLoopbackForABarePort(t *testing.T) {
	var reached bool
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { reached = true }))
	defer srv.Close()

	_, port, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")

	if err := bootstrap.Probe(":" + port); err != nil {
		t.Fatalf("Probe(%q) = %v", ":"+port, err)
	}
	if !reached {
		t.Error("a bare :port was not dialled on loopback")
	}
}
