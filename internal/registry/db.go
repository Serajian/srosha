package registry

import (
	"context"

	"github.com/Serajian/srosha/internal/config/settings"
	"github.com/Serajian/srosha/internal/infra/database"
)

// Postgres opens the pool and puts it under res, so readiness pings it and
// shutdown closes it without the caller having to remember either.
//
// It also maps the service's settings onto what the infra package needs, which
// is why internal/infra/database knows nothing about srosha. This is the only
// place the dsn is revealed; everywhere else it stays an env.Secret that prints
// itself redacted.
func Postgres(
	ctx context.Context,
	s settings.DB,
	res *Resources,
) (*database.Postgres, error) {
	db, err := database.New(database.Config{
		DSN:               s.DSN.Reveal(),
		MaxConns:          s.MaxConns,
		MaxConnLifetime:   s.MaxConnLifetime,
		MaxConnIdleTime:   s.MaxConnIdleTime,
		HealthCheckPeriod: s.HealthCheckPeriod,
		ConnectTimeout:    s.ConnectTimeout,
		ConnectAttempts:   s.ConnectAttempts,
		ConnectBackoff:    s.ConnectBackoff,
	}, res.log)
	if err != nil {
		return nil, err
	}

	if err := db.Connect(ctx); err != nil {
		return nil, err
	}

	res.add(step{
		name:  "postgres",
		ready: db.Ping,
		close: func(context.Context) error { db.Close(); return nil },
	})
	return db, nil
}
