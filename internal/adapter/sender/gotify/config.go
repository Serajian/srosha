package gotify

import (
	"encoding/json"
	"fmt"
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

	// ContentType is how the message should be rendered: plain, or markdown.
	// Empty means plain, and plain is what Gotify does with no extras at all.
	//
	// Unlike Telegram's parse_mode, getting this wrong is not expensive.
	// Gotify does not validate the body against the type -- markup it cannot
	// parse is shown as written, where Telegram refuses the whole message. So
	// this is safe to turn on for text somebody typed, which parse_mode is not.
	ContentType string `json:"content_type"`
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

	switch c.ContentType {
	case "":
		c.ContentType = TypePlain
	case TypePlain, TypeMarkdown:
	default:
		return errs.InvalidInputErr("gotify settings name an unknown content type").
			WithStr(fmt.Sprintf("content_type %q, want %q or %q",
				c.ContentType, TypePlain, TypeMarkdown))
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

// renderAs is the extras block for this configuration, or nothing.
//
// Plain gets nil rather than an explicit "text/plain": that is Gotify's own
// default, and saying it out loud would put a key on the wire for every message
// this service has ever sent, to say what was already true.
func (c Config) renderAs() *extras {
	if c.ContentType == "" || c.ContentType == TypePlain {
		return nil
	}
	return &extras{Display: display{ContentType: c.ContentType}}
}
