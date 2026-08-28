package web

// surface names this one's own directories under public/ -- its templates and
// its assets. The admin surface has its own and cannot reach these.
const surface = "portal"

// Where things live. One list, so a route is never spelled two ways.
const (
	pathHome    = "/"
	pathSignIn  = "/signin"
	pathCode    = "/signin/code"
	pathSignOut = "/signout"
	pathStatic  = "/static"
)

// The pages, by the name a handler asks for them by.
const (
	pageSignIn  = "signin"
	pageCode    = "code"
	pageAccount = "account"
)

// Form fields, spelled once so the template and the handler cannot drift.
const (
	fieldEmail = "email"
	fieldCode  = "code"
)
