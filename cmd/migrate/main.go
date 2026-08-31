// Command migrate applies pending database migrations and exits, or with
// `status` reports what the database has without changing it.
//
// It is a deployment step, never something a service does on the way up: a
// failed migration should stop the release rather than crash-loop the
// application.
package main

import (
	"context"
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
	cfg, err := config.LoadMigrate()
	if err != nil {
		// The only error that cannot be logged: there is no logger until the
		// configuration that describes it has been read.
		return err
	}

	// Signals are the one thing that is genuinely the process's own. Here they
	// matter more than usual: cancelling releases the lock, so a migration
	// interrupted by a deploy timeout does not leave the next one waiting.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One argument, and only one: `status` changes nothing and reports what
	// the database has. Anything else would be a flag package for a tool with
	// two behaviours.
	report := len(os.Args) > 1 && os.Args[1] == "status"

	return bootstrap.Migrate(ctx, cfg, report)
}
