package whatsapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Serajian/srosha/pkg/errs"
)

// Config is what a source configures about its WhatsApp business account beyond
// the token.
type Config struct {
	// PhoneNumberID is the number messages are sent FROM, as Meta identifies it.
	// Not a phone number: an id they issue, and it goes in the url path.
	PhoneNumberID string `json:"phone_number_id"`
}

// ParseConfig reads what was stored with the credential. Checked at registration
// rather than at send time, because an id that is not one is a configuration
// mistake and a message is a bad place to find one.
func ParseConfig(raw []byte) (Config, error) {
	var c Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, errs.InvalidInputErr("whatsapp settings are not valid json").WithErr(err)
		}
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	c.PhoneNumberID = strings.TrimSpace(c.PhoneNumberID)

	if c.PhoneNumberID == "" {
		return errs.InvalidInputErr("whatsapp settings have no phone number id")
	}
	if !usableID(c.PhoneNumberID) {
		// Never echoed back: it is somebody's account identifier.
		return errs.InvalidInputErr("whatsapp phone number id has the wrong format")
	}
	return nil
}

// usableID keeps the id to characters that cannot change which endpoint is
// called. Same reason the api base is a constant: everything deciding where a
// request goes has to be ours.
func usableID(id string) bool {
	return strings.IndexFunc(id, func(r rune) bool {
		return !strings.ContainsRune(idAlphabet, r)
	}) < 0
}

// template is what a source asked for through metadata, or nothing.
//
// Metadata is string to string, so an ordered list of parameters travels as json
// inside one value. That is the honest shape: the data has structure, so it is
// encoded rather than squeezed into a format that cannot hold it -- numbered
// keys sort wrongly past nine, and a separator breaks on the first parameter
// containing it.
type template struct {
	name     string
	language string
	params   []string
}

func templateFrom(metadata map[string]string) (template, bool, error) {
	name := strings.TrimSpace(metadata[metaTemplate])
	if name == "" {
		return template{}, false, nil
	}

	t := template{name: name, language: metadata[metaLanguage]}
	if t.language == "" {
		t.language = defaultLanguage
	}

	if raw := metadata[metaParameters]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &t.params); err != nil {
			return template{}, false, refused(fmt.Sprintf(
				"%s must be a json array of strings", metaParameters))
		}
	}
	return t, true, nil
}
