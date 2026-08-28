// Package web is the customer portal: server-rendered HTML, plain forms, no
// build pipeline and no javascript.
//
// It is a driving adapter like api/grpcsrv, and it knows the same amount about
// the core: a use case it was handed. The audience is people with browsers, so
// a single-page app would triple the work for nothing a customer would notice.
//
// The admin panel is deliberately NOT here. It is a separate private surface,
// because routing that puts both on one port is one bug away from handing
// source creation to the internet.
//
// One file, one type, and no type's methods leave the file that declares it:
//
//	web.go       Deps and New -- what exists, and who may reach it
//	session.go   sessions   the cookie, and the guard that reads it
//	render.go    renderer   a page and its data, turned into html
//	signin.go    signInHandler   getting in
//	account.go   accountHandler  being in
//
// The handlers hold what they use and nothing more. There is no type here that
// every handler can reach through, because that is how a page ends up quietly
// depending on something nobody meant to give it.
package web

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/public"
)

// SignIn is what this adapter needs from the core, declared here because that
// is where it is consumed. usecase.SignIn satisfies it and never learns that a
// browser exists.
type SignIn interface {
	Request(ctx context.Context, email string) error
	Verify(ctx context.Context, email, code string) (*session.Session, error)
	Whoami(ctx context.Context, sessionID shared.ID) (*user.User, error)
	End(ctx context.Context, sessionID shared.ID) error
}

// Deps is what the pages need. Everything in it was built by bootstrap: this
// package opens nothing and reads no config.
type Deps struct {
	SignIn SignIn

	// SecureCookie is off only for local development over plain http.
	SecureCookie bool

	Log *slog.Logger
}

func (d Deps) validate() error {
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

// New builds the whole page surface.
//
// Every route the portal answers is in the one table below, so nobody has to
// grep the package to find out what it serves or which pages need a session.
// Every mutating route is POST, so a link cannot cause one.
func New(d Deps) (http.Handler, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	render, err := newRenderer(d.Log, "signin", "code", "account")
	if err != nil {
		return nil, err
	}
	assets, err := browserFiles()
	if err != nil {
		return nil, err
	}

	sess := &sessions{signIn: d.SignIn, secure: d.SecureCookie}
	in := &signInHandler{signIn: d.SignIn, sessions: sess, render: render, log: d.Log}
	account := &accountHandler{signIn: d.SignIn, sessions: sess, render: render, log: d.Log}

	mux := http.NewServeMux()

	// --- getting in. no session, by definition ---------------------------
	mux.HandleFunc("GET "+pathSignIn, in.show)
	mux.HandleFunc("POST "+pathSignIn, in.request)
	mux.HandleFunc("GET "+pathCode, in.showCode)
	mux.HandleFunc("POST "+pathCode, in.verify)

	// --- being in --------------------------------------------------------
	//
	// "{$}" is an exact match. Without it "GET /" would catch every unrouted
	// path, and a GET of a POST-only route would come back as not-found
	// instead of the method refusal it is.
	mux.HandleFunc("GET "+pathHome+"{$}", sess.guard(account.show))
	mux.HandleFunc("POST "+pathSignOut, account.signOut)

	// --- files a browser fetches -----------------------------------------
	mux.Handle("GET "+pathStatic, http.StripPrefix(pathStatic, http.FileServerFS(assets)))

	return mux, nil
}

// browserFiles is the portal's static assets and nothing else.
//
// It subs into static/ deliberately: public.Files also carries the templates,
// and a file server pointed at its root would hand out the shape of every page
// and every field name in one request.
func browserFiles() (fs.FS, error) { return fs.Sub(public.Files, "static/portal") }

// redirect answers a browser after something happened. See other, not found:
// the next thing to do is a GET, which is also what stops a refresh from
// posting the form again.
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}
