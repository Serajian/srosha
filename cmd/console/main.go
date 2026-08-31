// Command portal is the customer portal: where somebody signs in, and from
// there runs their own sources. It serves pages and sends nothing but a
// sign-in code.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Serajian/srosha/internal/bootstrap"
	"github.com/Serajian/srosha/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadConsole()
	if err != nil {
		// The only error that cannot be logged: there is no logger until the
		// configuration that describes it has been read.
		return err
	}

	// argv is the process's own, like the signals below. Checked after the
	// config loads, deliberately: a container whose configuration no longer
	// reads is not a healthy one either, and saying so is the truth rather
	// than a false negative.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		return bootstrap.Probe(cfg.HTTP.Addr)
	}

	// Signals are the one thing that is genuinely the process's own, so they
	// stay here rather than in bootstrap.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.Console(ctx, cfg)
	if err != nil {
		return err
	}

	runErr := app.Run(ctx)

	// ctx is canceled by the time Run returns. Closing with it would fail every
	// step before it did anything, so shutdown gets its own budget.
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
	defer cancel()

	return errors.Join(runErr, app.Close(shutdown))
}
