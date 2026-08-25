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

	srhttp "github.com/Serajian/srosha/internal/adapter/api/http"
	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/httpserver"
	"github.com/Serajian/srosha/internal/infra/telemetry"
	"github.com/Serajian/srosha/internal/registry"
)

// App is one running binary. Run blocks; Close undoes what starting it did.
type App struct {
	log       *slog.Logger
	resources *registry.Resources

	// failed carries whichever listener died first. The gateway has two -- gRPC
	// and the health endpoint -- and either one stopping on its own means the
	// process is answering less than it claims to.
	failed <-chan error
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
	case err := <-a.failed:
		return err
	}
}

// watch merges the listeners' error channels into the one Run selects on.
//
// A select over a slice cannot be written directly, and the alternative -- one
// case per listener -- is a line that has to be edited every time a binary
// grows a port. Nothing is closed here: a shutdown is not a failure and must
// never wake Run up.
func watch(chans ...<-chan error) <-chan error {
	merged := make(chan error, len(chans))

	for _, ch := range chans {
		go func() {
			if err, ok := <-ch; ok {
				merged <- err
			}
		}()
	}
	return merged
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

// httpServer registers at the top tier, so it stops accepting before anything
// it was using goes away and a request in flight still finds everything it
// needs underneath it.
//
// Today it serves only health. Every other http route this service grows is
// mounted inside api/http, not opened as a second listener here.
func httpServer(
	ctx context.Context,
	binary string,
	addr string,
	s settings.HTTPServer,
	log *slog.Logger,
	res *registry.Resources,
) (*httpserver.Server, error) {
	handler, err := srhttp.New(srhttp.Deps{
		Binary: binary,
		Ready:  checks(res),
		Log:    log,
	})
	if err != nil {
		return nil, err
	}
	return registry.HTTPServer(ctx, "http server", addr, s, handler, res)
}

// checks carries registry's answers across to the adapter's own type. The two
// are deliberately separate: an adapter may not see registry, and bootstrap is
// the one place that sees both.
func checks(res *registry.Resources) func(context.Context) []srhttp.Check {
	return func(ctx context.Context) []srhttp.Check {
		got := res.Ready(ctx)

		out := make([]srhttp.Check, 0, len(got))
		for _, c := range got {
			out = append(out, srhttp.Check{Name: c.Name, Err: c.Err})
		}
		return out
	}
}

// abandon closes whatever already opened before giving up on the rest. Without
// it a failure halfway through startup leaks a pool nobody will ever close.
func abandon(ctx context.Context, res *registry.Resources, err error) (*App, error) {
	_ = res.Close(ctx)
	return nil, err
}
