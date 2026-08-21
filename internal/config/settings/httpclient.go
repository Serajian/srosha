package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// HTTPClient is what the dispatcher calls out with. It is not settings.HTTP:
// that one is the dispatcher's own listener.
type HTTPClient struct {
	Timeout     time.Duration
	DialTimeout time.Duration
	TLSTimeout  time.Duration

	// MaxIdleConnsPerHost matters more than it looks: Go's default is two, and
	// with a hundred sources that means dialing again on almost every callback.
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}

func LoadHTTPClient(r *env.Reader) HTTPClient {
	c := HTTPClient{
		Timeout:             r.Duration("HTTP_CLIENT_TIMEOUT", 30*time.Second),
		DialTimeout:         r.Duration("HTTP_CLIENT_DIAL_TIMEOUT", 5*time.Second),
		TLSTimeout:          r.Duration("HTTP_CLIENT_TLS_TIMEOUT", 5*time.Second),
		MaxIdleConns:        r.Int("HTTP_CLIENT_MAX_IDLE_CONNS", 100),
		MaxIdleConnsPerHost: r.Int("HTTP_CLIENT_MAX_IDLE_PER_HOST", 10),
		IdleConnTimeout:     r.Duration("HTTP_CLIENT_IDLE_CONN_TIMEOUT", 90*time.Second),
	}

	r.Check(c.MaxIdleConns > 0, "NOTIF_HTTP_CLIENT_MAX_IDLE_CONNS must be above zero")
	r.Check(c.MaxIdleConnsPerHost > 0 && c.MaxIdleConnsPerHost <= c.MaxIdleConns,
		"NOTIF_HTTP_CLIENT_MAX_IDLE_PER_HOST must be between one and "+
			"NOTIF_HTTP_CLIENT_MAX_IDLE_CONNS")
	return c
}
