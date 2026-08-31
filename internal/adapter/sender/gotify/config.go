package gotify

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Config is which self-hosted server an application lives on.
//
// Required, and there is no default: Gotify is self-hosted, so there is no
// address that is right for everybody the way api.telegram.org is for
// Telegram. That is also why this is the one setting checked here rather
// than trusted -- an address somebody else chose, the same reasoning and the
// same shape of check as Matrix's homeserver had.
type Config struct {
	ServerURL string `json:"server_url"`
}

// ParseConfig reads what was stored with the credential.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, errs.InvalidInputErr("gotify settings are not valid json").WithErr(err)
		}
	}
	return c, c.validate()
}

// validate refuses what the other channels never have to think about: an
// address somebody else chose.
//
// https only, no credentials in it, no path. The application token travels as
// a query parameter rather than a header -- see (*Sender).endpoint -- so an
// address that arrived as http:// would carry it over the wire in the clear,
// the same reasoning that keeps Matrix's homeserver check this strict.
func (c *Config) validate() error {
	c.ServerURL = strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")

	if c.ServerURL == "" {
		return errs.InvalidInputErr("gotify settings have no server url")
	}

	u, err := url.Parse(c.ServerURL)
	if err != nil {
		return errs.InvalidInputErr("gotify server url is not a url")
	}
	switch {
	case u.Scheme != "https":
		return errs.InvalidInputErr("gotify server url must use https")
	case u.Host == "":
		return errs.InvalidInputErr("gotify server url has no host")
	case u.User != nil:
		return errs.InvalidInputErr("gotify server url must not carry credentials")
	case u.Path != "", u.RawQuery != "", u.Fragment != "":
		return errs.InvalidInputErr("gotify server url must be a bare address")
	}
	return nil
}
