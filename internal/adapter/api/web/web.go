// Package web is the shared half of the html surfaces: the engine they are
// built on, the cookie they share, and the way a page becomes a response.
//
// It serves no routes itself. Each surface is a STRUCT in this package that
// builds an engine of its own -- NewPortal and NewAdmin -- and they share the
// code and never an instance.
//
// Two structs and not two packages, which was tried twice and refused twice:
// `web/portal` and then `web/admin` would each have to import `web` for the
// engine, the session and the guard, and `make arch-check` allows a parent to
// import its subpackage and not the reverse. See docs/ARCHITECTURE.md, "Two
// surfaces in one binary, and what keeps them apart".
//
// What a package would have added is a compiler that refuses to let one
// surface reach into the other's route table -- every admin handler is in
// scope from portal.go, one typo from being mounted there. That is held by
// TestNoAdminRouteAnswersOnThePortal instead, which reads the admin engine's
// own route table back and asserts each route 404s on the portal's handler.
//
// Gin is confined to this adapter. Nothing in core, infra or registry knows it
// exists, and gin.Engine satisfies http.Handler, so registry serves a surface
// exactly as it served the standard mux before.
package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/session"
	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

// SignIn is what a surface needs from the core, declared here because this is
// where it is consumed. usecase.SignIn satisfies it and never learns that a
// browser exists.
//
// Both surfaces need all four: an operator signs in through the same flow as a
// customer, because there is one users table and role is the only difference.
type SignIn interface {
	Request(ctx context.Context, email string) error
	Verify(ctx context.Context, email, code string) (*session.Session, error)
	Whoami(ctx context.Context, sessionID shared.ID) (*user.User, error)
	End(ctx context.Context, sessionID shared.ID) error
}

// PortalHandler and AdminHandler are what NewPortal and NewAdmin hand back.
//
// Two distinct types rather than http.Handler twice, and the reason is the one
// mistake this whole section of docs/ARCHITECTURE.md exists to prevent: the
// two handlers go to two listeners, one published to the internet and one
// bound to loopback, and until these types existed swapping them in
// bootstrap.Console compiled, passed every test, and served the admin panel on
// the public port. Now it does not compile.
//
// A struct wrapping the handler, not `type PortalHandler http.Handler`: two
// interface types with the same method set are assignable to each other, so
// the named-interface version would have let the swap through unchanged. Two
// struct types are not.
//
// The addresses are typed for the same reason -- settings.PortalAddr and
// settings.AdminAddr -- because swapping only those would have been the same
// outage with the handlers left alone.
type (
	PortalHandler struct{ http.Handler }
	AdminHandler  struct{ http.Handler }
)

// engineConfig is what every surface's engine is built from.
type engineConfig struct {
	// Debug turns on gin's own startup noise and its verbose panic dumps.
	// Off everywhere but development: the dump prints the request, and these
	// surfaces' requests carry sign-in codes.
	Debug bool

	Render render.HTMLRender
	Log    *slog.Logger
}

// newEngine builds a surface's engine, already turned away from the four gin
// defaults that are wrong here.
//
// Each surface calls this and gets its **own** engine. Nothing is shared but
// the code: two surfaces on one engine would be one routing mistake away from
// serving the admin pages on the public port.
func newEngine(cfg engineConfig) *gin.Engine {
	gin.SetMode(mode(cfg.Debug))

	// gin.New, not gin.Default: Default adds gin's own logger, which would
	// write a second, differently-shaped line for every request beside the
	// structured one this service already emits.
	engine := gin.New()
	engine.Use(recovery(cfg.Log), reportFailures(cfg.Log))
	engine.HTMLRender = cfg.Render

	// Off by default in gin, and the difference is real: a GET of a POST-only
	// route would otherwise come back as not-found, which says the route does
	// not exist when it does.
	engine.HandleMethodNotAllowed = true

	return engine
}

func mode(debug bool) string {
	if debug {
		return gin.DebugMode
	}
	return gin.ReleaseMode
}

// recovery answers a panic with a 500 and one structured line, rather than
// gin's own dump.
//
// gin's prints the request that panicked, and the requests on these surfaces
// carry sign-in codes -- a crash would put one in the log, where it outlives
// the ten minutes it was supposed to be worth anything for.
func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err any) {
		log.ErrorContext(c.Request.Context(), "a page panicked",
			"path", c.FullPath(), "error", err)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

// reportFailures reports what gin collected and nobody would otherwise read.
//
// A template that fails halfway does not panic: gin records the error on the
// context, aborts, and returns whatever was already written. Without this the
// page comes back truncated and the log says nothing -- which is exactly how a
// broken sign-in form went unnoticed through a passing test.
func reportFailures(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		for _, e := range c.Errors {
			log.ErrorContext(c.Request.Context(), "a page did not finish",
				"path", c.FullPath(), "error", e.Err)
		}
	}
}
