package settings

import "github.com/Serajian/srosha/pkg/env"

// Sender holds srosha's own credentials: the ones used when a source has not
// brought its own.
type Sender struct {
	SMTP     SMTP
	Telegram env.Secret
	Bale     env.Secret
	WhatsApp env.Secret
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
		WhatsApp: r.Secret("SENDER_WHATSAPP_TOKEN", ""),
	}
}
