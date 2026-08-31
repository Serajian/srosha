package settings

import (
	"net"
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
// Not two strings: the two are handed to two listeners, one published to the
// internet and one bound to loopback, and while both were `string` swapping
// them in bootstrap.Console compiled and served the admin panel on the public
// port. web.PortalHandler and web.AdminHandler are the other half of the same
// guard -- swapping either alone is now a compile error.
type (
	PortalAddr string
	AdminAddr  string
)

// bindsLoopback reports whether this address reaches only the machine it runs
// on.
//
// Checked in production for the same reason SecureCookie is, right beside it:
// the default is safe, and a default is not a guard. Somebody copying the
// portal's ":8090" into NOTIF_ADMIN_ADDR gets ":8092", which is every
// interface, and the panel that switches off customers' sources is then on the
// network -- with no error, no log line, and a service that starts perfectly.
//
// An unparseable address is not loopback. Refusing at boot is right either
// way: the listener would fail on the same string seconds later.
func (a AdminAddr) bindsLoopback() bool {
	host, _, err := net.SplitHostPort(string(a))
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func LoadConsole(r *env.Reader, production bool) Console {
	c := Console{
		PortalAddr: PortalAddr(r.Str("PORTAL_ADDR", ":8090")),
		AdminAddr:  AdminAddr(r.Str("ADMIN_ADDR", "127.0.0.1:8092")),
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
	r.Check(!production || c.AdminAddr.bindsLoopback(),
		"NOTIF_ADMIN_ADDR must bind the loopback interface in production "+
			"(127.0.0.1, ::1 or localhost): the admin panel is never published, "+
			"and an address like \":8092\" puts it on every interface")

	return c
}
