package web

// sessionCookieName is deliberately not "session": a name that says which
// service it belongs to is one less thing to guess at in a browser's cookie
// list.
//
// Both surfaces use it, and they have to -- a cookie is not scoped by port, so
// a second name would not separate them anyway. What separates them is the
// admin surface's own role check. See docs/ARCHITECTURE.md.
const sessionCookieName = "srosha_portal"

// contextUser is where the guard leaves the person it let through. A key of our
// own, so nothing else in a gin context can collide with it.
const contextUser = "srosha.user"

// maxFormBytes bounds what a form post may carry. These surfaces post short
// fields and never a file, and without a bound a request body is unbounded.
const maxFormBytes = 4 << 10
