// admin.go is the operator's surface: AdminDeps and NewAdmin -- what exists,
// and who may reach it. It mirrors portal.go's shape file for file; see
// docs/ARCHITECTURE.md, "Two surfaces in one binary, and what keeps them
// apart".
//
// One file, one type, and no type's methods leave the file that declares it,
// same as the portal:
//
//	admin.go            AdminDeps and NewAdmin
//	admin_review.go     reviewHandler   the queue and one source's decisions
//	admin_people.go     peopleHandler   roles and accounts, super_admin only
//	admin_audit.go      auditHandler    who did what, super_admin only
//
// Signing in is not a fourth file: an operator signs in through the same
// signInHandler and accountHandler the portal uses, on this listener, so the
// panel does not depend on the public one being reachable.
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/credential"
	"github.com/Serajian/srosha/internal/core/domain/source"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"
	"github.com/Serajian/srosha/internal/core/usecase"
)

// AdminDeps is what the admin pages need. Everything in it was built by
// bootstrap: this package opens nothing and reads no config.
type AdminDeps struct {
	SignIn    SignIn
	Operators Operators

	// SecureCookie is off only for local development over plain http.
	SecureCookie bool

	// Debug turns on gin's own startup noise and its verbose panic dumps.
	Debug bool

	Log *slog.Logger
}

func (d AdminDeps) validate() error {
	var errs []error

	if d.SignIn == nil {
		errs = append(errs, errors.New("no sign-in use case"))
	}
	if d.Operators == nil {
		errs = append(errs, errors.New("no operators use case"))
	}
	if d.Log == nil {
		errs = append(errs, errors.New("no logger"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Operators is what this surface needs from the core, across its three
// handlers. usecase.Operators satisfies it.
//
// One interface rather than one per handler, unlike the portal's SourcePages,
// KeyPages, SenderPages and CallbackPages: those are four different use case
// types there. Here there is one -- Operators -- and splitting its own shape
// three ways would not narrow what AdminDeps has to hold, only add three names
// for the same thing.
type Operators interface {
	// Queue and AllSources return a second bool: whether the list was
	// truncated to fit the configured cap. Every other list read below
	// carries the same second return, for the same reason -- see
	// usecase.Operators.truncate.
	Queue(ctx context.Context, actor *user.User) ([]source.Source, bool, error)
	AllSources(ctx context.Context, actor *user.User) ([]source.Source, bool, error)
	Source(ctx context.Context, actor *user.User, id string) (*source.Source, error)
	Approve(ctx context.Context, actor *user.User, id string) error
	Refuse(ctx context.Context, actor *user.User, id, note string) error
	Suspend(ctx context.Context, actor *user.User, id, note string) error
	Restore(ctx context.Context, actor *user.User, id string) error

	Messages(
		ctx context.Context, actor *user.User, sourceID string,
	) ([]usecase.OperatorMessage, bool, error)
	Deliveries(
		ctx context.Context, actor *user.User, sourceID, messageID string,
	) ([]usecase.OperatorDelivery, error)
	Senders(
		ctx context.Context, actor *user.User, sourceID string,
	) ([]credential.Credential, error)

	// SourceHistory is one source's own decisions -- see admin_review.go's
	// show, which renders it below the decisions the page already offers.
	SourceHistory(
		ctx context.Context, actor *user.User, sourceID string,
	) ([]usecase.AuditEntry, bool, error)

	People(ctx context.Context, actor *user.User) ([]user.User, bool, error)
	Person(ctx context.Context, actor *user.User, id shared.ID) (*user.User, error)
	SetRole(
		ctx context.Context, actor *user.User, id shared.ID, role user.Role, note string,
	) error
	SetPersonActive(
		ctx context.Context, actor *user.User, id shared.ID, on bool, note string,
	) error

	Audit(ctx context.Context, actor *user.User) ([]usecase.AuditEntry, bool, error)
}

// adminChrome is what this surface's layout needs from every page, whoever is
// looking at it.
//
// A struct of its own rather than widening the portal's chrome: this
// surface's nav needs to know whether to show /people and /audit, and widening
// chrome would give every portal page a field it never uses, and let a portal
// template branch on it.
//
// EVERY page behind the guard embeds it. The sign-in and code pages are the
// one exception, forced by sharing signInHandler and its page types with the
// portal -- see NewAdmin below -- so those two still embed the portal's
// chrome, whose zero value answers SignedIn: false. That is safe rather than
// a repeat of the bug docs/changes/2026-08-29-portal-navigation.md describes:
// the nav below reads .SuperAdmin only inside a `{{if .SignedIn}}` branch,
// which a page carrying that zero value never enters, so the field is never
// evaluated on a page that does not have it.
type adminChrome struct {
	SignedIn   bool
	SuperAdmin bool
}

// chromeFor builds this surface's chrome from who is signed in.
//
// Unlike the portal's single package-level `inside`, SuperAdmin genuinely
// varies by who is looking -- an admin and a super_admin both reach the
// guarded queue, and the nav must differ for each -- so one shared value
// cannot answer it. Every guarded page computes its own from the request.
func chromeFor(u *user.User) adminChrome {
	return adminChrome{SignedIn: true, SuperAdmin: u.Role == user.RoleSuperAdmin}
}

// NewAdmin builds this surface, on an engine of its own.
//
// Every route it answers is in the one table below, exactly as the portal's
// is, so nobody has to grep the package to find out what it serves or which
// pages need which role.
func NewAdmin(d AdminDeps) (AdminHandler, error) {
	if err := d.validate(); err != nil {
		return AdminHandler{}, err
	}

	pages, err := newPageRender(surfaceAdmin,
		pageSignIn, pageCode,
		pageQueue, pageSources, pageSource, pageAdminLog,
		pagePeople, pagePerson, pageAudit,
	)
	if err != nil {
		return AdminHandler{}, err
	}
	assets, err := browserFiles(surfaceAdmin)
	if err != nil {
		return AdminHandler{}, err
	}

	engine := newEngine(engineConfig{Debug: d.Debug, Render: pages, Log: d.Log})
	sessions := newSessions(d.SignIn, d.SecureCookie)

	in := &signInHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	account := &accountHandler{signIn: d.SignIn, sessions: sessions, log: d.Log}
	review := &reviewHandler{ops: d.Operators, log: d.Log}
	people := &peopleHandler{ops: d.Operators, log: d.Log}
	audit := &auditHandler{ops: d.Operators, log: d.Log}

	// --- getting in. no session, by definition. the same four steps as the
	//     portal, on this listener, so the panel does not depend on the public
	//     one being reachable ----------------------------------------------
	engine.GET(pathSignIn, in.show)
	engine.POST(pathSignIn, in.request)
	engine.GET(pathCode, in.showCode)
	engine.POST(pathCode, in.verify)

	// --- leaving. not guarded, for the same reason the portal's is not: a
	//     session that already expired still has a cookie to clear ---------
	engine.POST(pathSignOut, account.signOut)

	// --- ordinary operator work. named guarded, NOT inside: portal.go
	//     already declares a package-level `var inside = chrome{SignedIn:
	//     true}`, and a local of that name here would shadow it for the whole
	//     function --------------------------------------------------------
	guarded := engine.Group("", sessions.guard(operator, pathSignIn))
	guarded.GET(pathQueue, review.queue)
	guarded.GET(pathSources, review.list)
	guarded.GET(pathSource, review.show)
	guarded.POST(pathApprove, review.approve)
	guarded.POST(pathRefuse, review.refuse)
	guarded.POST(pathSuspend, review.suspend)
	guarded.POST(pathRestore, review.restore)
	guarded.GET(pathAdminLog, review.messages)

	// --- three pages behind a second rule. a group of its own rather than a
	//     check inside the handlers, so which pages need it is visible here,
	//     and the use case's own super_admin check is defense in depth rather
	//     than the only thing standing between an admin and the roster ------
	//
	//     pathAudit is here and it is NOT obvious from the page: it renders
	//     ActorEmail, and the gate records the actor of every act -- which is
	//     the CUSTOMER for source.create, key.issue, key.revoke and
	//     source.update. So the audit log is the roster by another door, and
	//     worse: it also ties a source's owner id, which is all
	//     admin/source.html shows, back to a person. /people was locked away
	//     from an admin deliberately; this had to follow it.
	top := engine.Group("", sessions.guard(superAdmin, pathQueue))
	top.GET(pathAudit, audit.show)
	top.GET(pathPeople, people.list)
	top.GET(pathPerson, people.show)
	top.POST(pathPersonRole, people.setRole)
	top.POST(pathPersonActive, people.setActive)

	// --- files a browser fetches -------------------------------------------
	engine.StaticFS(pathStatic, http.FS(assets))

	return AdminHandler{engine}, nil
}
