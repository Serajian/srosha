package web

// Where things live. One list, so a route is never spelled two ways.
const (
	pathHome    = "/"
	pathSignIn  = "/signin"
	pathCode    = "/signin/code"
	pathSignOut = "/signout"
	pathStatic  = "/static/"
)

// sessionCookieName is deliberately not "session": a name that says which
// service it belongs to is one less thing to guess at in a browser's cookie
// list.
const sessionCookieName = "srosha_portal"

// Form fields, spelled once so the template and the handler cannot drift.
const (
	fieldEmail = "email"
	fieldCode  = "code"
)

// maxFormBytes bounds what a form post may carry. Two short fields need
// nothing like this much, and without a bound a request body is unbounded.
const maxFormBytes = 4 << 10
