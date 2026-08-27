package settings

import (
	"encoding/base64"

	"github.com/Serajian/srosha/pkg/env"
)

// Sender holds srosha's own credentials: the ones used when a source has not
// brought its own.
type Sender struct {
	SMTP     SMTP
	Telegram env.Secret
	Bale     env.Secret
	WhatsApp WhatsApp
	Matrix   Matrix

	// FCM is the service account json itself, already decoded. See LoadSender.
	FCM env.Secret

	APNs APNs
}

// APNs is srosha's own Apple push identity: a signing key and the three names
// that say which key, which developer account and which app.
//
// Only the key is secret. The other three are on the key file's filename, in
// the developer portal, and inside every copy of the shipped app.
type APNs struct {
	// Key is the p8 file's contents, already decoded. See LoadSender.
	Key env.Secret

	KeyID       string
	TeamID      string
	Topic       string
	Environment string
}

// Matrix is srosha's own account. Two values, because the protocol is federated
// and there is no homeserver that is right for everybody the way there is one
// api.telegram.org.
type Matrix struct {
	Token      env.Secret
	Homeserver string
}

// WhatsApp is srosha's own business number. Two values and not one, because Meta
// identifies the sending number separately from the account that owns it -- the
// id goes in the url, the token in a header.
type WhatsApp struct {
	Token env.Secret

	// PhoneNumberID is Meta's id for the sending number, not the number itself.
	PhoneNumberID string
}

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password env.Secret
	From     string
}

// LoadSender requires nothing. A deployment that only sends on Telegram should
// not have to invent an SMTP host, and a channel with no credential fails as
// NO_SENDER on the delivery that asked for it -- reported to the source, rather
// than keeping the whole service down.
func LoadSender(r *env.Reader) Sender {
	return Sender{
		SMTP: SMTP{
			Host:     r.Str("SENDER_SMTP_HOST", ""),
			Port:     r.Int("SENDER_SMTP_PORT", 587),
			Username: r.Str("SENDER_SMTP_USER", ""),
			Password: r.Secret("SENDER_SMTP_PASSWORD", ""),
			From:     r.Str("SENDER_SMTP_FROM", ""),
		},
		Telegram: r.Secret("SENDER_TELEGRAM_TOKEN", ""),
		Bale:     r.Secret("SENDER_BALE_TOKEN", ""),
		Matrix: Matrix{
			Token:      r.Secret("SENDER_MATRIX_TOKEN", ""),
			Homeserver: r.Str("SENDER_MATRIX_HOMESERVER", ""),
		},
		FCM: fcmServiceAccount(r),
		APNs: APNs{
			Key:         decodeKey(r, "SENDER_APNS_KEY"),
			KeyID:       r.Str("SENDER_APNS_KEY_ID", ""),
			TeamID:      r.Str("SENDER_APNS_TEAM_ID", ""),
			Topic:       r.Str("SENDER_APNS_TOPIC", ""),
			Environment: r.Str("SENDER_APNS_ENVIRONMENT", ""),
		},
		WhatsApp: WhatsApp{
			Token:         r.Secret("SENDER_WHATSAPP_TOKEN", ""),
			PhoneNumberID: r.Str("SENDER_WHATSAPP_PHONE_NUMBER_ID", ""),
		},
	}
}

// fcmServiceAccount reads the service account key file.
func fcmServiceAccount(r *env.Reader) env.Secret {
	return decodeKey(r, "SENDER_FCM_SERVICE_ACCOUNT")
}

// decodeKey reads a key file that travels as base64.
//
// Both push channels carry one: FCM's is multi-line json wrapped around a PEM
// key, APNs' is a PEM key on its own. .env files, compose files and secret
// managers each mangle multi-line values differently, so they travel encoded
// and are decoded here -- the layer that can name the variable that is wrong.
//
// The encoding is an environment concern only. A source registering its own
// sends the file itself: nothing between a gRPC string field and the database
// needs it.
func decodeKey(r *env.Reader, name string) env.Secret {
	raw := r.Secret(name, "").Reveal()
	if raw == "" {
		return ""
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// The name, never the value.
		r.Check(false, "NOTIF_%s is not valid base64", name)
		return ""
	}
	return env.Secret(key)
}
