package email

import (
	"encoding/json"
	"fmt"
	"net/mail"

	"github.com/Serajian/srosha/pkg/errs"
)

// Config is a source's mail identity as it was registered: where to hand a
// message over, as whom, and in what shape.
//
// Unlike the bot channels, none of this is one value. A telegram identity is a
// token; a mail identity is a server, an account and an address, and any one of
// them wrong is a message that never arrives.
type Config struct {
	// Host, Port and Username describe the connection and are checked by
	// infra/smtp, which is what has to make it. Port may be zero: that means
	// submission, and infra says which port that is.
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`

	// From is who the recipient sees. It belongs to the message rather than the
	// connection, so it is checked here.
	From string `json:"from"`

	// ContentType is what Body is, text/plain or text/html. Empty means plain.
	ContentType string `json:"content_type"`
}

// ParseConfig reads what was stored with the credential.
//
// Checked at registration rather than at send time, because every one of these
// is a configuration mistake: a message is a bad place to discover a typo in a
// host name, and NO_SENDER points the source at their setup, which is where the
// fault is.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, errs.InvalidInputErr("email settings are not valid json").WithErr(err)
		}
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	if _, err := mail.ParseAddress(c.From); err != nil {
		// The address, not the parse error: that quotes the input back, and
		// this one is a configured value rather than somebody's own address.
		return errs.InvalidInputErr("email settings have no usable from address").
			WithStr(fmt.Sprintf("from %q", c.From))
	}

	switch c.ContentType {
	case "":
		c.ContentType = TypePlain
	case TypePlain, TypeHTML:
	default:
		return errs.InvalidInputErr("email settings name an unknown content type").
			WithStr(fmt.Sprintf("content_type %q, want %q or %q", c.ContentType, TypePlain, TypeHTML))
	}

	return nil
}
