package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type DB struct {
	// DSN is a secret: it carries the password.
	DSN         env.Secret
	MaxConns    int
	MaxLifetime time.Duration
}

func LoadDB(r *env.Reader) DB {
	return DB{
		DSN:         r.RequiredSecret("DB_DSN"),
		MaxConns:    r.Int("DB_MAX_CONNS", 10),
		MaxLifetime: r.Duration("DB_MAX_LIFETIME", 30*time.Minute),
	}
}
