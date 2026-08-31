package web

import (
	"github.com/gin-gonic/gin"
)

// This file is package web, compiled only into the test binary, and it exists
// for one test: the 404 boundary docs/ARCHITECTURE.md calls not optional.
//
// That test lives in admin_test.go, which is package web_test, so it cannot
// see admin_const.go's unexported path constants. Spelling them again there as
// string literals is what made the first version of the test iterate "/queue"
// -- a path that exists on neither surface -- and pass for two weeks. So the
// names cross the boundary here, once, and nothing is spelled twice.

// PortalCookieName and AdminCookieName are the two surfaces' session cookies.
//
// They cross the boundary here for the same reason the route table does: a
// name spelled again as a literal in a test is a name that can drift from the
// one the code writes, and the test would go on passing while the surfaces
// stopped being separated.
const (
	PortalCookieName = portalCookieName
	AdminCookieName  = adminCookieName
)

// AdminRoute is one row of a surface's own route table.
type AdminRoute struct{ Method, Path string }

// AdminRouteTable reads back every route a handler built by NewAdmin mounts.
//
// Read from the engine rather than listed by hand, so a route added to
// NewAdmin is checked by the boundary test without anybody remembering to
// name it. That is the whole difference between this test and the comment it
// used to be: a list somebody maintains is a list somebody forgets.
func AdminRouteTable(h AdminHandler) []AdminRoute {
	engine, ok := h.Handler.(*gin.Engine)
	if !ok {
		return nil
	}

	table := engine.Routes()
	out := make([]AdminRoute, 0, len(table))
	for _, r := range table {
		out = append(out, AdminRoute{Method: r.Method, Path: r.Path})
	}
	return out
}

// AdminPathsSharedWithThePortal are the paths NewAdmin mounts that the portal
// answers too, by design -- so the boundary test must NOT demand a 404 for
// them.
//
// Three of them are the same string reaching a different handler, on a
// different engine, behind a different guard:
//
//	pathQueue         "/"             the portal's own account page
//	pathSources  "/sources"      the customer's own list
//	pathSource   "/sources/:id"  the customer's own source
//
// The rest are the sign-in flow and the static files. Both surfaces serve
// those, each from its own engine, deliberately: an operator signs in on the
// admin listener so the panel does not depend on the public one being
// reachable.
//
// Adding a path here is exempting it from the boundary, so it is a decision
// with a reason, not a way to make a failing test pass.
var AdminPathsSharedWithThePortal = []string{
	pathQueue, pathSources, pathSource,
	pathSignIn, pathCode, pathSignOut,
	pathStatic + "/*filepath",
}

// AdminOnlyPaths is every path that exists for this surface and no other.
// The boundary test checks the admin engine's live table; this is the floor
// under that check, so a route silently dropped from NewAdmin -- which would
// shrink what gets asserted and break nothing -- fails instead.
var AdminOnlyPaths = []string{
	pathApprove, pathRefuse, pathSuspend, pathRestore, pathAdminLog,
	pathAudit, pathPeople, pathPerson, pathPersonRole, pathPersonActive,
}
