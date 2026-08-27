package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Config is which homeserver a source's account lives on.
//
// Required, and there is no default: Matrix is federated, so there is no address
// that is right for everybody the way api.telegram.org is. That is also why this
// is the one channel whose host comes from a source rather than a constant --
// and why the url is checked here rather than trusted.
type Config struct {
	Homeserver string `json:"homeserver"`
}

// ParseConfig reads what was stored with the credential.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, errs.InvalidInputErr("matrix settings are not valid json").WithErr(err)
		}
	}
	return c, c.validate()
}

// validate refuses what the other channels never have to think about: an address
// somebody else chose.
//
// https only, no credentials in it, no path. A homeserver that arrives as
// http:// would carry an access token over the wire in the clear, and one with a
// path would let the endpoint below be built onto something else entirely.
func (c *Config) validate() error {
	c.Homeserver = strings.TrimRight(strings.TrimSpace(c.Homeserver), "/")

	if c.Homeserver == "" {
		return errs.InvalidInputErr("matrix settings have no homeserver")
	}

	u, err := url.Parse(c.Homeserver)
	if err != nil {
		return errs.InvalidInputErr("matrix homeserver is not a url")
	}
	switch {
	case u.Scheme != "https":
		return errs.InvalidInputErr("matrix homeserver must use https")
	case u.Host == "":
		return errs.InvalidInputErr("matrix homeserver has no host")
	case u.User != nil:
		return errs.InvalidInputErr("matrix homeserver must not carry credentials")
	case u.Path != "", u.RawQuery != "", u.Fragment != "":
		return errs.InvalidInputErr("matrix homeserver must be a bare address").
			WithStr(fmt.Sprintf("got %q", c.Homeserver))
	}
	return nil
}
