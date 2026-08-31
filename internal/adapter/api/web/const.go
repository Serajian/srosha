package web

// surfacePortal and surfaceAdmin name each surface's own directories under
// public/ -- its templates and its assets. Neither can reach the other's; see
// newPageRender and browserFiles.
const (
	surfacePortal = "portal"
	surfaceAdmin  = "admin"
)

// Each surface has its own cookie, and the name is what separates them.
//
// It used to be one name for both, and it had to be: the surfaces differed by
// port, and a cookie is not scoped by port. They differ by host now --
// panel.srosha.ir and admin.srosha.ir -- and a cookie is scoped by host, so a
// customer's session is not refused at the admin surface. It is never sent.
//
// Neither is "session": a name that says which service it belongs to is one
// less thing to guess at in a browser's cookie list.
const (
	portalCookieName = "srosha_portal"
	adminCookieName  = "srosha_admin"
)

// contextUser is where the guard leaves the person it let through. A key of our
// own, so nothing else in a gin context can collide with it.
const contextUser = "srosha.user"

// maxFormBytes bounds what a form post may carry. These surfaces post short
// fields and never a file, and without a bound a request body is unbounded.
const maxFormBytes = 4 << 10
