package web

// The admin pages, by the name a handler asks for them by.
//
// Signing in shares pageSignIn and pageCode with the portal -- portal_const.go
// -- and so do the list and one source, pageSources and pageSource. A page
// name is a template's file name and newPageRender reads it out of THIS
// surface's own directory, so one name is two different files.
//
// pageAdminSources and pageAdminSource used to sit here as a second spelling of
// those last two strings, which is precisely what these blocks exist to
// prevent: two names for one value drift the day somebody edits one.
const (
	pageQueue    = "queue"
	pageAdminLog = "log"
	pagePeople   = "people"
	pagePerson   = "person"
	pageAudit    = "audit"
)

// Where things live on this surface. ":id" is gin's parameter syntax, read
// with c.Param("id").
//
// Six paths are not here because they are the portal's own spelling of the
// same strings -- portal_const.go: pathSignIn, pathCode and pathSignOut, which
// both surfaces answer on their own separate listeners, and pathSources and
// pathSource, which reach a different handler on a different engine behind a
// different guard. TestNoAdminRouteAnswersOnThePortal names all of them as the
// paths the two surfaces genuinely share, with the reason for each.
const (
	// pathQueue IS pathHome, deliberately and not by coincidence: signIn.verify
	// is shared with the portal and redirects to pathHome, so an operator lands
	// on the queue only because these two are the same string. Defined as the
	// other rather than asserted equal to it, so nothing can move one without
	// the other.
	pathQueue = pathHome

	pathApprove      = "/sources/:id/approve"
	pathRefuse       = "/sources/:id/refuse"
	pathSuspend      = "/sources/:id/suspend"
	pathRestore      = "/sources/:id/restore"
	pathAdminLog     = "/sources/:id/log"
	pathAudit        = "/audit"
	pathPeople       = "/people"
	pathPerson       = "/people/:id"
	pathPersonRole   = "/people/:id/role"
	pathPersonActive = "/people/:id/active"

	// pathArchitecture is the diagram of this service, served whole. It sits
	// with the two above rather than with the queue: it names every host,
	// every port, every store and the private network they sit on, which is
	// the shape of the deployment and not an operator's daily work.
	pathArchitecture = "/architecture"
)

// fileArchitecture is the document pathArchitecture answers with, under
// public/guarded/admin/. Not a page name: it is served byte for byte and
// never goes through a template, so newPageRender never hears about it.
const fileArchitecture = "architecture.html"

// Form fields, spelled once so the template and the handler cannot drift.
const (
	// fieldNote is an operator's free-text reason: a refusal's, a suspension's,
	// a role change's, a deactivation's.
	fieldNote = "note"

	fieldRole   = "role"
	fieldActive = "active"

	// fieldMessage is the query string key that opens one message in the log to
	// its deliveries -- a follow-up question on the same page, not a route of
	// its own.
	fieldMessage = "message"

	// fieldState narrows /sources to one state. A query string rather than
	// four routes, for the same reason fieldMessage is: it is the same page
	// answering a narrower question.
	fieldState = "state"
)

// The four states a source can be in, as /sources spells them.
//
// Four values and not a column: a source's state is three fields --
// is_active, approved_at, reviewed_at -- and these are the names an operator
// thinks in. Spelled once here so the handler that filters and the template
// that links cannot drift.
const (
	stateWaiting   = "waiting"
	stateSending   = "sending"
	stateSuspended = "suspended"
	stateRefused   = "refused"
)
