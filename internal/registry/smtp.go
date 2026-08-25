package registry

import (
	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/smtp"
)

// SMTPDialer opens the way to a mail server, and registers nothing to close.
//
// Nothing to close because there is nothing held: SMTP has no connection worth
// keeping between messages -- a server drops an idle session on its own schedule
// -- so each send dials, hands over and hangs up. The dialer holds only the
// timeout every identity shares.
//
// A dialer rather than a client, because mail is not http. One http client
// serves every provider; a mail client is one account on one server, and every
// source may bring its own.
func SMTPDialer(c settings.HTTPClient) (*smtp.Dialer, error) {
	return smtp.NewDialer(smtp.DialerConfig{Timeout: c.Timeout})
}
