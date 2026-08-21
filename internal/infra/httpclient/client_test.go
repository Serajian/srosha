package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/infra/httpclient"
)

func sane() httpclient.Config {
	return httpclient.Config{
		Timeout:             2 * time.Second,
		DialTimeout:         time.Second,
		TLSTimeout:          time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	tests := []struct {
		name   string
		breaks func(*httpclient.Config)
		want   string
	}{
		{"no timeout", func(c *httpclient.Config) { c.Timeout = 0 }, "timeout"},
		{"no dial timeout", func(c *httpclient.Config) { c.DialTimeout = 0 }, "dial timeout"},
		{"no tls timeout", func(c *httpclient.Config) { c.TLSTimeout = 0 }, "tls timeout"},
		{"no pool", func(c *httpclient.Config) { c.MaxIdleConns = 0 }, "max idle conns"},
		{
			"per-host above the pool it comes out of",
			func(c *httpclient.Config) { c.MaxIdleConnsPerHost = 200 },
			"per host",
		},
		{
			"no idle timeout",
			func(c *httpclient.Config) { c.IdleConnTimeout = 0 },
			"idle conn timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := sane()
			tt.breaks(&cfg)

			if _, err := httpclient.New(cfg); err == nil {
				t.Fatal("New() accepted it")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}

func TestNewReportsEveryProblemTogether(t *testing.T) {
	_, err := httpclient.New(httpclient.Config{})
	if err == nil {
		t.Fatal("New() accepted an empty config")
	}
	for _, want := range []string{"timeout", "dial timeout", "tls timeout", "max idle conns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// The guard has to stop a real request, not just classify a string: a test
// server listens on loopback, which is exactly what a callback must never
// reach.
func TestTheGuardStopsARealRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := sane()
	cfg.DenyPrivateAddresses = true

	client, err := httpclient.New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resp, err := client.Get(server.URL) //nolint:noctx // no context to carry here
	if err == nil {
		resp.Body.Close()
		t.Fatal("the guard let a loopback address through")
	}
}

// And it must be off by default, or the provider APIs would be unreachable the
// day one of them sits behind something the check dislikes.
func TestWithoutTheGuardTheSameRequestSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.New(sane())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resp, err := client.Get(server.URL) //nolint:noctx // no context to carry here
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// An endpoint answering a callback has no business sending us somewhere else,
// so the redirect comes back as what it is rather than being followed.
func TestARedirectIsReturnedNotFollowed(t *testing.T) {
	var arrived int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			arrived++
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()

	client, err := httpclient.New(sane())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resp, err := client.Get(server.URL) //nolint:noctx // no context to carry here
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d, want 302", resp.StatusCode)
	}
	if arrived != 0 {
		t.Error("the redirect was followed")
	}
}

func TestARedirectIsFollowedWhenAsked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()

	cfg := sane()
	cfg.FollowRedirects = true

	client, err := httpclient.New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	resp, err := client.Get(server.URL) //nolint:noctx // no context to carry here
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
