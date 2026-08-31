package bootstrap

import (
	"context"
	"log/slog"

	"github.com/Serajian/srosha/internal/config"
	"github.com/Serajian/srosha/internal/infra/migrations"
	"github.com/Serajian/srosha/internal/registry"

	sqlfiles "github.com/Serajian/srosha/migrations"
)

// Migrate applies every pending migration and returns. With report set it
// changes nothing and only says what the database has.
//
// It is not an App: there is nothing to run, nothing to listen on and nothing
// to shut down gracefully. It opens a database, takes a lock, applies what is
// pending and exits -- which is the whole reason migrations are a separate
// step rather than something a service does on the way up.
func Migrate(ctx context.Context, cfg config.Migrate, report bool) error {
	log, err := logger(cfg.Telemetry, cfg.App.ServiceName, binaryMigrate)
	if err != nil {
		return err
	}

	res := registry.New(log)
	defer closeAll(ctx, res, log)

	db, err := registry.MigrationDB(ctx, cfg.DB, res)
	if err != nil {
		return err
	}

	runner, err := migrations.New(db, sqlfiles.Files, migrations.Config{
		LockTimeout: cfg.Migration.LockTimeout,
	})
	if err != nil {
		return err
	}

	if report {
		return status(ctx, runner, log)
	}
	return up(ctx, runner, log)
}

func up(ctx context.Context, runner *migrations.Runner, log *slog.Logger) error {
	applied, err := runner.Up(ctx)
	if err != nil {
		return err
	}

	if len(applied) == 0 {
		log.InfoContext(ctx, "database is already up to date")
		return nil
	}
	for _, a := range applied {
		log.InfoContext(ctx, "applied", "version", a.Version, "file", a.File)
	}
	return nil
}

func status(ctx context.Context, runner *migrations.Runner, log *slog.Logger) error {
	rows, err := runner.Status(ctx)
	if err != nil {
		return err
	}

	for _, s := range rows {
		state := "pending"
		if s.Applied {
			state = "applied"
		}
		if s.AppliedAt.IsZero() {
			log.InfoContext(ctx, "migration", "state", state, "file", s.File)
			continue
		}
		log.InfoContext(ctx, "migration",
			"state", state, "file", s.File, "at", s.AppliedAt.UTC())
	}
	return nil
}

// closeAll is Close for a tool rather than a service: nothing is serving, so
// there is no deadline to respect beyond the caller's context.
func closeAll(ctx context.Context, res *registry.Resources, log *slog.Logger) {
	if err := res.Close(ctx); err != nil {
		log.ErrorContext(ctx, "could not close cleanly", "error", err)
	}
}
