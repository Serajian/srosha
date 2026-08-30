package registry

import (
	"time"

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
// The timeout is passed rather than a settings group: it is the only thing a
// dialer holds, and the two binaries that need one read it from different
// places -- the dispatcher's outbound client settings, and the console's own.
func SMTPDialer(timeout time.Duration) (*smtp.Dialer, error) {
	return smtp.NewDialer(smtp.DialerConfig{Timeout: timeout})
}
