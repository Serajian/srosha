// Command dispatcher consumes notifications, sends them and calls back.
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
	cfg, err := config.LoadDispatcher()
	if err != nil {
		// The only error that cannot be logged: there is no logger until the
		// configuration that describes it has been read.
		return err
	}

	// Signals are the one thing that is genuinely the process's own, so they
	// stay here rather than in bootstrap.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := bootstrap.Dispatcher(ctx, cfg)
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
