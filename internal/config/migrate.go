package config

import (
	"fmt"

	"github.com/Serajian/srosha/internal/config/settings"
)

// Migrate is everything the migration tool needs, which is almost nothing: a
// database to reach and the directory the sql lives in.
//
// It has no broker, no crypto keys and no sending credentials, and that is the
// point of it being its own type: a tool that runs before the service starts
// should not need the service's secrets to run.
type Migrate struct {
	App       settings.App
	DB        settings.DB
	Migration settings.Migration
	Telemetry settings.Telemetry
}

func LoadMigrate() (Migrate, error) {
	r := reader("migrate")

	app := settings.LoadApp(r)

	c := Migrate{
		App:       app,
		DB:        settings.LoadDB(r, app.IsProduction()),
		Migration: settings.LoadMigration(r),
		Telemetry: settings.LoadTelemetry(r, app.IsProduction()),
	}

	if err := r.Err(); err != nil {
		return Migrate{}, fmt.Errorf("migrate configuration: %w", err)
	}
	return c, nil
}
