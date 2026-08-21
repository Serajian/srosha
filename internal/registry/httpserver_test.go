package registry

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Serajian/srosha/internal/config/settings"
)

// The server binds before it serves, so a config it refuses must leave nothing
// in the list to close.
func TestHTTPServerRefusesAnEmptyAddr(t *testing.T) {
	res := New(discard())

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	_, err := HTTPServer(context.Background(), "health", "", settings.HTTPServer{}, handler, res)
	if err == nil {
		t.Fatal("want an empty addr refused")
	}
	if len(res.steps) != 0 {
		t.Fatal("a failed open must not leave a step behind")
	}
}

// And one it accepts closes with everything else, in reverse.
func TestHTTPServerClosesWithTheRest(t *testing.T) {
	res := New(discard())

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s := settings.HTTPServer{
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
	}

	server, err := HTTPServer(context.Background(), "health", "127.0.0.1:0", s, handler, res)
	if err != nil {
		t.Fatalf("HTTPServer() error = %v", err)
	}

	addr := server.Addr()
	resp, err := http.Get("http://" + addr) //nolint:noctx // no context to carry here
	if err != nil {
		t.Fatalf("it did not serve: %v", err)
	}
	resp.Body.Close()

	if err := res.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if after, err := http.Get("http://" + addr); err == nil { //nolint:noctx // as above
		after.Body.Close()
		t.Error("it kept serving after close")
	}
}
