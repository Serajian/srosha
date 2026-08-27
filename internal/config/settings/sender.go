package settings

import "github.com/Serajian/srosha/pkg/env"

// Sender holds srosha's own credentials: the ones used when a source has not
// brought its own.
type Sender struct {
	SMTP     SMTP
	Telegram env.Secret
	Bale     env.Secret
	WhatsApp WhatsApp
	Matrix   Matrix
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
		WhatsApp: WhatsApp{
			Token:         r.Secret("SENDER_WHATSAPP_TOKEN", ""),
			PhoneNumberID: r.Str("SENDER_WHATSAPP_PHONE_NUMBER_ID", ""),
		},
	}
}
