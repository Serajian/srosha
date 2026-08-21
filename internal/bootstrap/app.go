// Package bootstrap wires a binary: it opens what that binary needs, in an
// order it decides, and hands back something that can be shut down again.
//
// It is the only package that may import internal/registry. Everything it
// builds receives what was opened rather than opening anything itself.
package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/Serajian/srosha/internal/adapter/api/health"
	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/httpserver"
	"github.com/Serajian/srosha/internal/infra/telemetry"
	"github.com/Serajian/srosha/internal/registry"
)

// App is one running binary. Run blocks; Close undoes what starting it did.
type App struct {
	log       *slog.Logger
	resources *registry.Resources
	server    *httpserver.Server
}

// Run blocks until the process is asked to stop, or until something that was
// supposed to keep running stops on its own.
//
// A listener that dies is the case worth the select: without it the process
// would sit there healthy and answering nothing.
func (a *App) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		a.log.InfoContext(ctx, "shutting down")
		return nil
	case err := <-a.server.Err():
		return err
	}
}

// Close shuts everything in the reverse of the order it opened.
//
// The context must NOT be the one Run returned on: that one is already
// canceled, and every step would fail before doing anything. Give it a fresh
// one bounded by the shutdown budget.
func (a *App) Close(ctx context.Context) error {
	return a.resources.Close(ctx)
}

// logger is the first thing built, because registry needs one and so does
// everything after it. It writes to stderr: a container's log driver picks that
// up, and nothing interleaves with real program output.
func logger(t settings.Telemetry, service, binary string) (*slog.Logger, error) {
	return telemetry.NewLogger(telemetry.Config{
		Level:   t.LogLevel,
		Format:  t.LogFormat,
		Source:  t.LogSource,
		Service: service,
		Binary:  binary,
	}, os.Stderr)
}

// healthServer registers at the top tier, so it stops accepting before
// anything it was using goes away and a request in flight still finds
// everything it needs underneath it.
func healthServer(
	ctx context.Context,
	addr string,
	s settings.HTTPServer,
	log *slog.Logger,
	res *registry.Resources,
) (*httpserver.Server, error) {
	return registry.HTTPServer(ctx, "health server", addr, s,
		health.Handler(res.Ready, log), res)
}

// abandon closes whatever already opened before giving up on the rest. Without
// it a failure halfway through startup leaks a pool nobody will ever close.
func abandon(ctx context.Context, res *registry.Resources, err error) (*App, error) {
	_ = res.Close(ctx)
	return nil, err
}
