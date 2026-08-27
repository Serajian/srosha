package apns

import (
	"encoding/json"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Config is everything about an APNs identity that is not the key.
//
// Four fields, which makes this the first credential in the service to need
// more than a secret and a setting or two. They are here rather than inside the
// sealed value because none of them is secret: a key id names a file, a team id
// names an account, and a topic is the app's bundle id, which ships inside
// every copy of the app.
type Config struct {
	// KeyID names the p8 file, and goes in the signed token's header.
	KeyID string `json:"key_id"`

	// TeamID is the developer account the key belongs to.
	TeamID string `json:"team_id"`

	// Topic is the app's bundle id. APNs will not deliver without it: one key
	// can push to every app on an account, so the message has to say which.
	Topic string `json:"topic"`

	// Environment is "production" or "sandbox", and defaults to production.
	// They are separate services with separate device tokens.
	Environment string `json:"environment"`
}

// ParseConfig reads what was stored with the credential.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, errs.InvalidInputErr("apns settings are not valid json").WithErr(err)
		}
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	c.KeyID = strings.TrimSpace(c.KeyID)
	c.TeamID = strings.TrimSpace(c.TeamID)
	c.Topic = strings.TrimSpace(c.Topic)
	c.Environment = strings.ToLower(strings.TrimSpace(c.Environment))

	if c.Environment == "" {
		c.Environment = environmentProduction
	}

	switch {
	case c.KeyID == "":
		return errs.InvalidInputErr("apns settings have no key id")
	case c.TeamID == "":
		return errs.InvalidInputErr("apns settings have no team id")
	case c.Topic == "":
		return errs.InvalidInputErr("apns settings have no topic")
	case c.Environment != environmentProduction && c.Environment != environmentSandbox:
		return errs.InvalidInputErr("apns environment must be production or sandbox").
			WithStr("got " + c.Environment)
	}
	return nil
}

// host is which of the two services this identity sends through.
func (c Config) host() string {
	if c.Environment == environmentSandbox {
		return sandboxHost
	}
	return productionHost
}
