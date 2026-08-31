package srosha

import (
	"encoding/json"
	"fmt"
)

// A Credential is one sending identity: which bot, which mail account, which
// signing key.
//
// Typed rather than the json string the wire carries, because that string's
// shape differs per channel -- {"key_id":…,"team_id":…,"topic":…} for APNs,
// {"phone_number_id":…} for WhatsApp, nothing at all for three of them -- and
// it is documented nowhere a customer can read. A misspelled key would compile
// and fail days later on the first send. Here the compiler catches it.
//
// This is not the multiplication that per-channel Send methods would have been.
// Those would have been seven identical bodies; these are seven genuinely
// different shapes, and the type carries information rather than repeating it.
//
// The set is closed by an unexported method, so nothing outside this package
// can pose as one. RawCredential is the way out when the service is newer than
// this build.
type Credential interface {
	// Settings is the json the service stores beside the secret. Empty is
	// legitimate: three channels need none.
	//
	// Exported so that the server, which imports this module, can hand it to
	// the very parser that will read it in production. That test is the whole
	// point of keeping the SDK and the service in one repository.
	Settings() (string, error)

	// channel and secret stay unexported, and between them they do two jobs.
	// They close the set -- nothing outside this package can pose as a
	// Credential -- and they keep the secret from being read off the interface
	// by anything that merely holds one.
	channel() Channel
	secret() string
}

// TelegramCredential is a bot token from BotFather.
type TelegramCredential struct {
	// Never marshaled: a credential reaches a log line eventually.
	Token string `json:"-"`
}

func (c TelegramCredential) channel() Channel          { return ChannelTelegram }
func (c TelegramCredential) Settings() (string, error) { return "", nil }
func (c TelegramCredential) secret() string            { return c.Token }
func (c TelegramCredential) String() string            { return "TelegramCredential{…}" }

// BaleCredential is a bot token from Bale's BotFather.
type BaleCredential struct {
	Token string `json:"-"`
}

func (c BaleCredential) channel() Channel          { return ChannelBale }
func (c BaleCredential) Settings() (string, error) { return "", nil }
func (c BaleCredential) secret() string            { return c.Token }
func (c BaleCredential) String() string            { return "BaleCredential{…}" }

// SMTPCredential is a mail account: where to hand a message over, and as whom.
type SMTPCredential struct {
	Host string

	// Port picks the encryption too. 465 is TLS from the first byte, anything
	// else is STARTTLS. Zero means 587, which is what a provider hands a
	// customer unless they ask for something else.
	Port int

	// Username may be empty, for a relay that authenticates by network rather
	// than by password.
	Username string

	// From is the address recipients see.
	From string

	Password string `json:"-"`
}

func (c SMTPCredential) channel() Channel { return ChannelEmail }
func (c SMTPCredential) secret() string   { return c.Password }

func (c SMTPCredential) Settings() (string, error) {
	return marshal(map[string]any{
		"host":     c.Host,
		"port":     c.Port,
		"username": c.Username,
		"from":     c.From,
	})
}

func (c SMTPCredential) String() string {
	return fmt.Sprintf("SMTPCredential{Host:%q, Port:%d, Username:%q, From:%q}",
		c.Host, c.Port, c.Username, c.From)
}

// MatrixCredential is an account on a homeserver.
//
// The homeserver is the one address in this service a source chooses rather
// than a constant somewhere: Matrix is federated, so there is no host that is
// right for everybody. It must be https -- an access token over plain http is
// a token in the clear.
type MatrixCredential struct {
	Homeserver string

	Token string `json:"-"`
}

func (c MatrixCredential) channel() Channel { return ChannelMatrix }
func (c MatrixCredential) secret() string   { return c.Token }

func (c MatrixCredential) Settings() (string, error) {
	return marshal(map[string]any{"homeserver": c.Homeserver})
}

func (c MatrixCredential) String() string {
	return fmt.Sprintf("MatrixCredential{Homeserver:%q}", c.Homeserver)
}

// GotifyCredential is an application on a self-hosted Gotify server.
//
// The server url is the one address in this service a source chooses rather
// than a constant somewhere: Gotify is self-hosted, so there is no host that
// is right for everybody. It must be https -- the application token travels
// as a query parameter, not a header, so it would be a token in the clear
// over plain http.
//
// Which application receives a message is decided by Token alone under
// Gotify's documented API -- see the service's own gotify package for what
// that means for the address a Route to this channel carries.
type GotifyCredential struct {
	ServerURL string

	Token string `json:"-"`
}

func (c GotifyCredential) channel() Channel { return ChannelGotify }
func (c GotifyCredential) secret() string   { return c.Token }

func (c GotifyCredential) Settings() (string, error) {
	return marshal(map[string]any{"server_url": c.ServerURL})
}

func (c GotifyCredential) String() string {
	return fmt.Sprintf("GotifyCredential{ServerURL:%q}", c.ServerURL)
}

// WhatsAppCredential is a business number.
//
// Two values and not one, because Meta identifies the sending number separately
// from the account that owns it: the id goes in the url and the token in a
// header.
type WhatsAppCredential struct {
	PhoneNumberID string

	Token string `json:"-"`
}

func (c WhatsAppCredential) channel() Channel { return ChannelWhatsApp }
func (c WhatsAppCredential) secret() string   { return c.Token }

func (c WhatsAppCredential) Settings() (string, error) {
	return marshal(map[string]any{"phone_number_id": c.PhoneNumberID})
}

func (c WhatsAppCredential) String() string {
	return fmt.Sprintf("WhatsAppCredential{PhoneNumberID:%q}", c.PhoneNumberID)
}

// FCMCredential is a Firebase service account file, json and private key
// together.
//
// No settings at all, which makes it the only one: the project id is inside the
// file, so asking for it again would only make a way for the two to disagree.
type FCMCredential struct {
	// ServiceAccount is the whole json file, not base64 of it. The encoding is
	// an environment concern and there is no environment between here and the
	// service.
	ServiceAccount string `json:"-"`
}

func (c FCMCredential) channel() Channel          { return ChannelFCM }
func (c FCMCredential) Settings() (string, error) { return "", nil }
func (c FCMCredential) secret() string            { return c.ServiceAccount }
func (c FCMCredential) String() string            { return "FCMCredential{…}" }

// APNsEnvironment is which of Apple's two push services a token belongs to.
//
// They are separate services with separate device tokens: a token from a
// development build is unknown to production, and the answer is a delivery
// refused for the device when the mistake was the address of the service.
type APNsEnvironment string

const (
	// APNsProduction is the default when the field is left empty, because it is
	// the one a shipped app uses.
	APNsProduction APNsEnvironment = "production"
	APNsSandbox    APNsEnvironment = "sandbox"
)

// APNsCredential is an Apple push identity: a signing key and the three names
// that say which key, which developer account, and which app.
//
// Only the key is secret. The key id is in the file's name, the team id names
// the developer account, and the topic is the app's bundle id, which ships
// inside every copy of the app.
type APNsCredential struct {
	// KeyID names the .p8 file, and goes in the signed token's header.
	KeyID string

	// TeamID is the developer account the key belongs to.
	TeamID string

	// Topic is the app's bundle id. APNs will not deliver without it: one key
	// can push to every app on an account, so the message has to say which.
	Topic string

	Environment APNsEnvironment

	// Key is the contents of the .p8 file, not base64 of it.
	Key string `json:"-"`
}

func (c APNsCredential) channel() Channel { return ChannelAPNs }
func (c APNsCredential) secret() string   { return c.Key }

func (c APNsCredential) Settings() (string, error) {
	return marshal(map[string]any{
		"key_id":      c.KeyID,
		"team_id":     c.TeamID,
		"topic":       c.Topic,
		"environment": string(c.Environment),
	})
}

func (c APNsCredential) String() string {
	return fmt.Sprintf("APNsCredential{KeyID:%q, TeamID:%q, Topic:%q, Environment:%q}",
		c.KeyID, c.TeamID, c.Topic, c.Environment)
}

// RawCredential is for a channel this build has no type for, which is what a
// customer reaches for when the service is newer than their SDK. Being stuck
// until the SDK catches up is worse than writing the json by hand once.
type RawCredential struct {
	Channel Channel

	// Config is the settings json, exactly as the service expects it. Empty is
	// legitimate.
	Config string

	Secret string `json:"-"`
}

func (c RawCredential) channel() Channel          { return c.Channel }
func (c RawCredential) Settings() (string, error) { return c.Config, nil }
func (c RawCredential) secret() string            { return c.Secret }

func (c RawCredential) String() string {
	return fmt.Sprintf("RawCredential{Channel:%q}", c.Channel)
}

func marshal(v map[string]any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
