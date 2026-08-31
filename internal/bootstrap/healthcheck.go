package bootstrap

import (
	"fmt"
	"net"
	"net/http"
)

// Probe asks this process's own /readyz and reports whether it said ready.
//
// It exists so the runtime image needs no wget, curl or shell: a docker
// healthcheck runs the binary itself. It adds no judgement of its own --
// readiness is decided in the adapter, and a second opinion here is a second
// answer that can disagree with the first.
func Probe(addr string) error {
	url := "http://" + dialable(addr) + "/readyz"

	client := http.Client{Timeout: probeTimeout}
	res, err := client.Get(url) //nolint:noctx // the timeout above is the budget
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s said %s", url, res.Status)
	}
	return nil
}

// dialable turns a listen address into one a client can reach.
//
// A server binds ":8080" or "0.0.0.0:8080" to mean every interface, and
// neither is dialable. The probe runs inside the container it is asking about,
// so the answer is always loopback.
func dialable(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
