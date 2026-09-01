package registry

import (
	"context"
	"net/http"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/httpclient"
)

// WebhookClient calls back to an address the source chose, so it refuses one
// that resolves onto our own network and does not follow redirects. Its timeout
// is the webhook's own: a callback is a courtesy, and waiting on a slow one
// holds up the delivery that triggered it.
func WebhookClient(
	c settings.HTTPClient,
	w settings.Webhook,
	res *Resources,
) (*http.Client, error) {
	return openHTTPClient("webhook http client", httpclient.Config{
		Timeout:              w.Timeout,
		DialTimeout:          c.DialTimeout,
		TLSTimeout:           c.TLSTimeout,
		MaxIdleConns:         c.MaxIdleConns,
		MaxIdleConnsPerHost:  c.MaxIdleConnsPerHost,
		IdleConnTimeout:      c.IdleConnTimeout,
		DenyPrivateAddresses: !w.AllowPrivateURL,
		FollowRedirects:      false,
	}, res)
}

// SenderClient calls the provider APIs -- Telegram, Bale, WhatsApp. Those are
// fixed public hosts we chose ourselves, so neither guard belongs on it: a
// private-address check could one day refuse a legitimate API, and a redirect
// from a provider is theirs to make.
func SenderClient(c settings.HTTPClient, res *Resources) (*http.Client, error) {
	return openHTTPClient("sender http client", httpclient.Config{
		Timeout:              c.Timeout,
		DialTimeout:          c.DialTimeout,
		TLSTimeout:           c.TLSTimeout,
		MaxIdleConns:         c.MaxIdleConns,
		MaxIdleConnsPerHost:  c.MaxIdleConnsPerHost,
		IdleConnTimeout:      c.IdleConnTimeout,
		DenyPrivateAddresses: false,
		FollowRedirects:      true,
	}, res)
}

// AlertClient reaches the operator's own Gotify, which is self-hosted and may
// well be on a private network -- so unlike the webhook client, it refuses
// nothing on that basis. It is the operator's own address, not one a stranger
// supplied.
//
// Its own client and not the sender's, because the two must not share a fate:
// the whole point of an alert is to arrive when the sending path is the thing
// that is broken.
func AlertClient(a settings.Alert, res *Resources) (*http.Client, error) {
	return openHTTPClient("alert http client", httpclient.Config{
		Timeout:              a.Timeout,
		DialTimeout:          alertDialTimeout,
		TLSTimeout:           alertTLSTimeout,
		MaxIdleConns:         alertIdleConns,
		MaxIdleConnsPerHost:  alertIdleConns,
		IdleConnTimeout:      alertIdleTimeout,
		DenyPrivateAddresses: false,
		FollowRedirects:      true,
	}, res)
}

// openHTTPClient registers with no readiness check: a client has no destination
// of its own, so there is nothing to ask. Closing is real, though -- idle
// sockets stay open to whoever was called last.
func openHTTPClient(name string, cfg httpclient.Config, res *Resources) (*http.Client, error) {
	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, err
	}

	res.add(step{
		tier: tierClient,
		name: name,
		close: func(context.Context) error {
			client.CloseIdleConnections()
			return nil
		},
	})
	return client, nil
}
