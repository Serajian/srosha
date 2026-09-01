package bootstrap

import (
	"log/slog"

	"github.com/Serajian/srosha/internal/adapter/alert"
	"github.com/Serajian/srosha/internal/adapter/sender/gotify"
	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/registry"
)

// alerts builds the operator's channel, or a silent one.
//
// The gotify sender is constructed here and not inside the alert package
// because one adapter may not import another -- bootstrap is the one place
// that sees both. See make arch-check, whose error says exactly this.
//
// Unconfigured is not an error. On a laptop nobody has a Gotify, and the
// alerter it gets back has no queue and no goroutine.
func alerts(
	cfg settings.Alert, res *registry.Resources, log *slog.Logger,
) (*alert.Alerter, error) {
	if !cfg.Configured() {
		return alert.New(nil, "", alert.Config{}, log), nil
	}

	client, err := registry.AlertClient(cfg, res)
	if err != nil {
		return nil, err
	}

	push, err := gotify.New(client, cfg.GotifyToken.Reveal(), gotify.Config{
		ServerURL: cfg.GotifyURL,
	})
	if err != nil {
		return nil, err
	}

	// Gotify ignores the application id entirely: the token is what selects
	// the application. Verified on 2026-09-01 against a real server -- a
	// message sent with appid=999, which does not exist, landed in the token's
	// own application exactly like one sent with the right id and one sent
	// with none at all.
	//
	// So there is no key for it. Something has to be passed because the sender
	// refuses an empty address, and this is that something.
	a := alert.New(push, gotifyIgnoredAppID, alert.Config{
		Queue:   cfg.Queue,
		Timeout: cfg.Timeout,
	}, log)

	// Registered so shutdown drains it: an alert raised on the way down still
	// has somewhere to go.
	res.Alerts("alerts", a)
	return a, nil
}
