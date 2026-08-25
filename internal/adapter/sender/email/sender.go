// Package email sends a message as mail. It knows what a notification looks like
// as an email and what a reply code means for a retry; how the conversation with
// a server happens is internal/infra/smtp's.
package email

import (
	"context"
	"fmt"

	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/infra/smtp"
	"github.com/Serajian/srosha/pkg/errs"
)

// Dialer opens the way to one mail account.
//
// Declared here rather than taken as a concrete type, so this package is handed
// what registry opened rather than opening anything: nothing outside registry
// may open a technology. internal/infra/smtp satisfies it.
type Dialer interface {
	Open(smtp.Identity) (*smtp.Client, error)
}

// Sender is one mail identity.
type Sender struct {
	dialer Dialer
	cfg    Config
	id     smtp.Identity
}

func New(dialer Dialer, cfg Config, password string) (*Sender, error) {
	if dialer == nil {
		return nil, errs.InternalErr("email sender has no dialer")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	id := smtp.Identity{
		Host: cfg.Host, Port: cfg.Port, Username: cfg.Username, Password: password,
	}
	if err := checkIdentity(dialer, id); err != nil {
		return nil, err
	}

	return &Sender{dialer: dialer, cfg: cfg, id: id}, nil
}

func (s *Sender) Channel() shared.Channel { return shared.ChannelEmail }

// Send hands one message over and returns the Message-ID it went out with.
//
// That id is the handle a source needs to find the message on their own side.
// We do not track delivery ourselves, so it is the only thing we can hand back.
func (s *Sender) Send(ctx context.Context, m shared.Message) (string, error) {
	client, err := s.dialer.Open(s.id)
	if err != nil {
		return "", classify(err)
	}

	id, err := client.Send(ctx, smtp.Message{
		From:        s.cfg.From,
		To:          m.Recipient.Address,
		Subject:     m.Title,
		Body:        m.Body,
		ContentType: s.cfg.ContentType,
	})
	if err != nil {
		return "", classify(err)
	}
	return id, nil
}

// checkIdentity refuses at registration what would otherwise fail on every
// message: a host that is not one, a user with no password. The dialer holds no
// connection, so asking it costs nothing.
func checkIdentity(dialer Dialer, id smtp.Identity) error {
	if _, err := dialer.Open(id); err != nil {
		return errs.InvalidInputErr("email settings cannot be used").
			WithStr(fmt.Sprintf("host %q", id.Host)).
			WithErr(err)
	}
	return nil
}
