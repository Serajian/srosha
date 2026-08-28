package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// Console is the human-facing binary's configuration: the pages people sign in
// to, as opposed to the gRPC surface other services call.
//
// It carries a mail identity of its own rather than reusing the sender's:
// signing in must not depend on how a customer's messages happen to be
// configured, and the two are changed by different people for different
// reasons.
//
// One binary, two surfaces. The mail account and the cookie rules are the
// binary's and are shared; each surface brings its own address, because which
// of them is exposed is the whole security argument.
type Console struct {
	// PortalAddr is where the customer pages are served. Public.
	//
	// The admin surface will bring AdminAddr beside this one, on a port that is
	// never published. Health is a third listener on HTTP.Addr, because this
	// one is public and readiness is not.
	PortalAddr string

	// SMTP sends the sign-in code, and nothing else. Both surfaces use it:
	// operators and customers sign in through the same flow.
	SMTP SMTP

	// MailTimeout is how long one code may take to hand over.
	MailTimeout time.Duration

	// SecureCookie is off only for local development over plain http. A cookie
	// without it travels in the clear.
	SecureCookie bool
}

func LoadConsole(r *env.Reader, production bool) Console {
	c := Console{
		PortalAddr: r.Str("PORTAL_ADDR", ":8090"),
		SMTP: SMTP{
			Host:     r.Str("CONSOLE_SMTP_HOST", ""),
			Port:     r.Int("CONSOLE_SMTP_PORT", 587),
			Username: r.Str("CONSOLE_SMTP_USER", ""),
			Password: r.Secret("CONSOLE_SMTP_PASSWORD", ""),
			From:     r.Str("CONSOLE_SMTP_FROM", ""),
		},
		MailTimeout:  r.Duration("CONSOLE_SMTP_TIMEOUT", 15*time.Second),
		SecureCookie: r.Bool("CONSOLE_SECURE_COOKIE", true),
	}

	r.Check(c.SMTP.Host != "",
		"NOTIF_CONSOLE_SMTP_HOST is required: nobody can sign in without it")
	r.Check(c.SMTP.From != "", "NOTIF_CONSOLE_SMTP_FROM is required")
	r.Check(!production || c.SecureCookie,
		"NOTIF_CONSOLE_SECURE_COOKIE must be on in production")

	return c
}
