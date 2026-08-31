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
	PortalAddr PortalAddr

	// AdminAddr is where the operator pages are served. Never published --
	// it defaults to the loopback interface rather than every interface, so
	// staying off the network is a property of the process and not only a
	// deployment fact. Health is a third listener on HTTP.Addr, because the
	// portal is public and readiness is not.
	AdminAddr AdminAddr

	// AdminListLimit bounds every list a panel page reads: the queue, all
	// sources, a source's message log, a source's own decisions, the roster,
	// and the global audit feed. One number for all five (six, counting a
	// source's own decision history) rather than one per page: it is the
	// same concept everywhere it appears -- how many rows one operator reads
	// off one screen -- and separate knobs would be separate numbers that
	// drift apart with nothing to notice. Raising it mid-incident, or
	// lowering it because a query got slow, is an operational decision, so
	// it is read here rather than compiled in.
	AdminListLimit int32

	// SMTP sends the sign-in code, and nothing else. Both surfaces use it:
	// operators and customers sign in through the same flow.
	SMTP SMTP

	// MailTimeout is how long one code may take to hand over.
	MailTimeout time.Duration

	// SecureCookie is off only for local development over plain http. A cookie
	// without it travels in the clear.
	SecureCookie bool
}

// PortalAddr and AdminAddr are the two surfaces' listen addresses, each with
// a type of its own.
//
// Not two strings: while both were `string`, swapping them in bootstrap.Console
// compiled and served the admin panel where the portal was meant to be.
// web.PortalHandler and web.AdminHandler are the other half of the same guard
// -- swapping either alone is now a compile error.
type (
	PortalAddr string
	AdminAddr  string
)

func LoadConsole(r *env.Reader, production bool) Console {
	c := Console{
		PortalAddr: PortalAddr(r.Str("PORTAL_ADDR", ":8090")),
		// Loopback by default, which is right for a laptop and is only a
		// default. Deployed, the admin surface listens like any other service
		// and Traefik routes to it by host -- loopback inside a container
		// reaches nothing at all. See docs/ARCHITECTURE.md.
		AdminAddr:      AdminAddr(r.Str("ADMIN_ADDR", "127.0.0.1:8092")),
		AdminListLimit: int32(r.Int("ADMIN_LIST_LIMIT", 200)), //nolint:gosec // checked below
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
	r.Check(c.AdminListLimit > 0,
		"NOTIF_ADMIN_LIST_LIMIT must be above zero: a limit of zero would read "+
			"as a page with nothing on it, indistinguishable from a page that "+
			"genuinely has nothing to show")

	return c
}
