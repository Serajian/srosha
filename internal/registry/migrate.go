package registry

import (
	"context"
	"database/sql"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/database"
)

// MigrationDB opens the handle the migration tool runs against.
//
// Separate from Postgres because the two want opposite things: a service holds
// many connections for a long time, and a migration holds exactly one and then
// exits. That one connection is not a saving -- it is what makes the advisory
// lock mean anything. See database.OpenSQL.
func MigrationDB(ctx context.Context, s settings.DB, res *Resources) (*sql.DB, error) {
	db, err := database.OpenSQL(s.DSN.Reveal(), s.ConnectTimeout)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	res.add(step{
		tier:  tierStore,
		name:  "postgres",
		close: func(context.Context) error { return db.Close() },
	})
	return db, nil
}
