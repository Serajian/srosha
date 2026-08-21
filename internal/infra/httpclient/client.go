// Package httpclient builds an outbound http client. It knows how to reach
// somewhere over http and nothing about what this service sends there.
package httpclient

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Config is what this package needs. It is not the service's settings type:
// infra should stay copyable into another service, and mapping one to the other
// is registry's job.
//
// Nothing here has a default. Every value is an operational decision, so it
// comes from config and is named in one place rather than two.
type Config struct {
	// Timeout caps a whole request, redirects and body included. The others cap
	// one phase each, so a host that accepts a connection and then says nothing
	// still fails.
	Timeout     time.Duration
	DialTimeout time.Duration
	TLSTimeout  time.Duration

	// MaxIdleConnsPerHost matters more than it looks: Go's default is two, and
	// with a hundred sources that means dialing again on almost every callback.
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration

	// DenyPrivateAddresses refuses a connection whose resolved address is on a
	// private network. Turn it on whenever the address was chosen by whoever is
	// being called back, and off for hosts we picked ourselves.
	DenyPrivateAddresses bool

	// FollowRedirects should be off for the same case: an endpoint answering a
	// callback has no business sending us somewhere else.
	FollowRedirects bool
}

func (c Config) validate() error {
	var errs []error

	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(c.Timeout > 0, "timeout must be above zero")
	check(c.DialTimeout > 0, "dial timeout must be above zero")
	check(c.TLSTimeout > 0, "tls timeout must be above zero")
	check(c.MaxIdleConns > 0, "max idle conns must be above zero")
	check(c.MaxIdleConnsPerHost > 0, "max idle conns per host must be above zero")
	check(c.MaxIdleConnsPerHost <= c.MaxIdleConns,
		"max idle conns per host %d is above max idle conns %d",
		c.MaxIdleConnsPerHost, c.MaxIdleConns)
	check(c.IdleConnTimeout > 0, "idle conn timeout must be above zero")

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("httpclient: %w", errors.Join(errs...))
}

// New builds a client and opens nothing: a connection is made on the first
// request. There is no Connect and no Ping -- a client has no destination of
// its own, so it has no health of its own either.
func New(cfg Config) (*http.Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: cfg.DialTimeout}
	if cfg.DenyPrivateAddresses {
		dialer.Control = denyPrivate
	}

	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: cfg.TLSTimeout,
			MaxIdleConns:        cfg.MaxIdleConns,
			MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
			IdleConnTimeout:     cfg.IdleConnTimeout,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyFromEnvironment,
		},
	}

	if !cfg.FollowRedirects {
		// ErrUseLastResponse hands the 3xx back rather than failing, so the
		// caller sees what the endpoint actually answered.
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client, nil
}

// denyPrivate is the check the webhook domain says it cannot do. Validating the
// callback url only proves its shape: a name that resolves to 169.254.169.254
// or to `postgres` on our own network passes it.
//
// Control runs after DNS and before the connection, once per attempt, so it
// catches a redirect and a name that resolves differently the second time --
// neither of which a check on the url could see.
func denyPrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("httpclient: %q is not an address: %w", address, err)
	}

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("httpclient: %q did not resolve to an address", host)
	}

	// An IPv4 address wearing an IPv6 coat is still that address, and every
	// test below would miss it.
	if addr.Is4In6() {
		addr = addr.Unmap()
	}

	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return fmt.Errorf("httpclient: %s is not a public address", addr)
	}
	return nil
}
