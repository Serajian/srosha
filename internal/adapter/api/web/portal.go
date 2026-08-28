// Package portal is the customer's surface: where somebody signs up, signs in,
// and runs their own sources.
//
// It is public, and it is the only surface a browser reaches from outside. The
// operator's pages are web/admin -- a separate package on a separate listener,
// which does not import this one and is not imported by it. Routing that put
// both on one engine would be one mistake away from handing source creation to
// the internet.
//
// One file, one type, and no type's methods leave the file that declares it:
//
//	portal.go   Deps and New -- what exists, and who may reach it
//	signin.go   signInHandler   getting in
//	account.go  accountHandler  being in
package web

import (
	"errors"
	"log/slog"
	"net/http"
)

// Deps is what the pages need. Everything in it was built by bootstrap: this
// package opens nothing and reads no config.
type PortalDeps struct {
	SignIn SignIn

	// SecureCookie is off only for local development over plain http.
	SecureCookie bool

	// Debug turns on gin's own startup noise and its verbose panic dumps.
	Debug bool

	Log *slog.Logger
}

func (d PortalDeps) validate() error {
	var errs []error

	if d.SignIn == nil {
		errs = append(errs, errors.New("no sign-in use case"))
	}
	if d.Log == nil {
		errs = append(errs, errors.New("no logger"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// New builds this surface, on an engine of its own.
//
// Every route the portal answers is in the one table below, so nobody has to
// grep the package to find out what it serves or which pages need a session.
// Every mutating route is POST, so a link cannot cause one.
func NewPortal(d PortalDeps) (http.Handler, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	pages, err := newPageRender(surface, pageSignIn, pageCode, pageAccount)
	if err != nil {
		return nil, err
	}
	assets, err := browserFiles(surface)
	if err != nil {
		return nil, err
	}

	engine := newEngine(engineConfig{Debug: d.Debug, Render: pages, Log: d.Log})

	sessions := newSessions(d.SignIn, d.SecureCookie)
	in := &signInHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	account := &accountHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}

	// --- getting in. no session, by definition ---------------------------
	engine.GET(pathSignIn, in.show)
	engine.POST(pathSignIn, in.request)
	engine.GET(pathCode, in.showCode)
	engine.POST(pathCode, in.verify)

	// --- leaving. not guarded on purpose: a session that already expired
	//     still has a cookie to clear --------------------------------------
	engine.POST(pathSignOut, account.signOut)

	// --- being in. the guard is on the group, so a page added here cannot
	//     forget it. anybody is this surface's rule: signing in is the
	//     whole of it, which is exactly what the admin surface will not say --
	authed := engine.Group("", sessions.guard(anybody, pathSignIn))
	authed.GET(pathHome, account.show)

	// --- files a browser fetches -----------------------------------------
	engine.StaticFS(pathStatic, http.FS(assets))

	return engine, nil
}
