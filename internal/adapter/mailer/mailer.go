// Package mailer sends the one message this service sends on its own behalf: a
// sign-in code.
//
// It does not go through srosha. A sign-in that depends on the service you are
// signing in to fix is a trap, and this needs no queue to reach one mailbox.
package mailer

import (
	"context"
	"fmt"
	"strings"

	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/pkg/errs"
)

// Dialer opens the way to one mail account.
//
// Declared here rather than taken as a concrete type, so this package is handed
// what registry opened rather than opening anything itself.
type Dialer interface {
	Open(smtp.Identity) (*smtp.Client, error)
}

// Mailer is one mail account, used for one kind of message.
//
// from is separate from the identity because smtp.Identity is who we
// authenticate as, which is not always the address a person should see and
// reply to.
type Mailer struct {
	dialer Dialer
	id     smtp.Identity
	from   string
}

func New(dialer Dialer, id smtp.Identity, from string) (*Mailer, error) {
	if dialer == nil {
		return nil, errs.InternalErr("mailer has no dialer")
	}
	if strings.TrimSpace(from) == "" {
		return nil, errs.InternalErr("mailer has no from address")
	}
	return &Mailer{dialer: dialer, id: id, from: from}, nil
}

// SendCode sends one code to one address.
//
// A failure is Unavailable rather than Internal: the mail server is somebody
// else's, and a caller can reasonably ask again.
func (m *Mailer) SendCode(ctx context.Context, email, code string) error {
	client, err := m.dialer.Open(m.id)
	if err != nil {
		return errs.UnavailableErr("the sign-in code could not be sent").
			WithStr("open smtp").
			WithErr(err)
	}

	if _, err := client.Send(ctx, compose(m.from, email, code)); err != nil {
		return errs.UnavailableErr("the sign-in code could not be sent").
			WithStr(fmt.Sprintf("send to %q", email)).
			WithErr(err)
	}
	return nil
}

// compose is pure so that what a person actually receives can be asserted on
// without a mail server.
func compose(from, to, code string) smtp.Message {
	return smtp.Message{
		From:        from,
		To:          to,
		Subject:     subject,
		Body:        fmt.Sprintf(body, code),
		ContentType: contentType,
	}
}
