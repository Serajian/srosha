// portal.go is the customer's surface: PortalDeps and NewPortal -- where
// somebody signs up, signs in, and runs their own sources.
//
// It is public, and it is the only surface a browser reaches from outside. The
// operator's pages are admin.go: a second struct in this same package, on a
// listener of its own. `web/admin` does not exist and will not -- it was tried
// and `make arch-check` refused it, correctly. What keeps the two apart is a
// separate engine each, a role check on the admin guard, and a listener that
// is never published; see docs/ARCHITECTURE.md.
//
// Routing that put both on one engine would be one mistake away from handing
// source creation to the internet, and NewPortal returns a PortalHandler
// rather than an http.Handler so it cannot be handed to the wrong listener.
//
// One file, one type, and no type's methods leave the file that declares it:
//
//	portal.go            PortalDeps and NewPortal
//	portal_signin.go     signInHandler    getting in
//	portal_account.go    accountHandler   being in
//	portal_source.go     sourceHandler    a customer's own sources
//	portal_key.go        keyHandler       their keys
//	portal_identity.go   identityHandler  their senders and callback
package web

import (
	"errors"
	"log/slog"
	"net/http"
)

// Deps is what the pages need. Everything in it was built by bootstrap: this
// package opens nothing and reads no config.
type PortalDeps struct {
	SignIn    SignIn
	Sources   SourcePages
	Keys      KeyPages
	Senders   SenderPages
	Callbacks CallbackPages

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
	if d.Sources == nil {
		errs = append(errs, errors.New("no sources use case"))
	}
	if d.Keys == nil {
		errs = append(errs, errors.New("no keys use case"))
	}
	if d.Senders == nil {
		errs = append(errs, errors.New("no credentials use case"))
	}
	if d.Callbacks == nil {
		errs = append(errs, errors.New("no webhook use case"))
	}
	if d.Log == nil {
		errs = append(errs, errors.New("no logger"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// chrome is what the layout needs from every page, whoever is looking at it.
//
// EVERY page embeds it, not only the guarded ones. A page that left it out
// would not render a nav-less layout -- html/template refuses a field that is
// not there, and the page stops mid-tag with the error going nowhere. That is
// exactly how the sign-in form disappeared once.
type chrome struct{ SignedIn bool }

// inside is what a page behind the guard embeds. The sign-in pages embed the
// zero value, which is the honest answer: nobody is signed in yet.
var inside = chrome{SignedIn: true}

// New builds this surface, on an engine of its own.
//
// Every route the portal answers is in the one table below, so nobody has to
// grep the package to find out what it serves or which pages need a session.
// Every mutating route is POST, so a link cannot cause one.
func NewPortal(d PortalDeps) (PortalHandler, error) {
	if err := d.validate(); err != nil {
		return PortalHandler{}, err
	}

	pages, err := newPageRender(surfacePortal,
		pageSignIn, pageCode, pageAccount,
		pageSources, pageSourceNew, pageSource, pageSourceEdit,
		pageKeys, pageKeyIssued,
		pageSenders, pageCallback, pageCallbackSecret,
	)
	if err != nil {
		return PortalHandler{}, err
	}
	assets, err := browserFiles(surfacePortal)
	if err != nil {
		return PortalHandler{}, err
	}

	engine := newEngine(engineConfig{Debug: d.Debug, Render: pages, Log: d.Log})

	sessions := newSessions(d.SignIn, portalCookieName, d.SecureCookie)
	in := &signInHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	account := &accountHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	sources := &sourceHandler{sources: d.Sources, log: d.Log}
	keys := &keyHandler{keys: d.Keys, log: d.Log}
	identity := &identityHandler{
		senders: d.Senders, callbacks: d.Callbacks, sources: d.Sources, log: d.Log,
	}

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
	authed.GET(pathSources, sources.list)
	authed.GET(pathSourceNew, sources.showNew)
	authed.POST(pathSources, sources.create)
	authed.GET(pathSource, sources.show)
	authed.GET(pathSourceEdit, sources.showEdit)
	authed.POST(pathSourceEdit, sources.update)
	authed.GET(pathSourceKeys, keys.list)
	authed.POST(pathSourceKeys, keys.issue)
	authed.POST(pathKeyRevoke, keys.revoke)
	authed.GET(pathSourceSenders, identity.showSenders)
	authed.POST(pathSourceSenders, identity.addSender)
	authed.POST(pathSenderOff, identity.switchSender(false))
	authed.POST(pathSenderOn, identity.switchSender(true))
	authed.GET(pathSourceCallback, identity.showCallback)
	authed.POST(pathSourceCallback, identity.setCallback)

	// --- files a browser fetches -----------------------------------------
	engine.StaticFS(pathStatic, http.FS(assets))

	return PortalHandler{engine}, nil
}
