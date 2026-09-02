package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

type DB struct {
	// DSN is a secret: it carries the password.
	DSN env.Secret

	MaxConns        int
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration

	// HealthCheckPeriod is how often the pool notices a connection died under
	// it, after a failover or a firewall dropping an idle socket.
	HealthCheckPeriod time.Duration

	// ConnectTimeout bounds one attempt; Attempts and Backoff bound the loop
	// that covers a database container still starting up.
	ConnectTimeout  time.Duration
	ConnectAttempts int
	ConnectBackoff  time.Duration
}

func LoadDB(r *env.Reader, production bool) DB {
	db := DB{
		DSN:               r.RequiredSecret("DB_DSN"),
		MaxConns:          r.Int("DB_MAX_CONNS", 10),
		MaxConnLifetime:   r.Duration("DB_MAX_CONN_LIFETIME", 30*time.Minute),
		MaxConnIdleTime:   r.Duration("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		HealthCheckPeriod: r.Duration("DB_HEALTH_CHECK_PERIOD", 30*time.Second),
		ConnectTimeout:    r.Duration("DB_CONNECT_TIMEOUT", 5*time.Second),
		ConnectAttempts:   r.Int("DB_CONNECT_ATTEMPTS", 5),
		ConnectBackoff:    r.Duration("DB_CONNECT_BACKOFF", 2*time.Second),
	}

	checkURLPassword(r, production, "DB_DSN", db.DSN)

	r.Check(db.MaxConns > 0, "NOTIF_DB_MAX_CONNS must be above zero")
	r.Check(db.ConnectAttempts > 0, "NOTIF_DB_CONNECT_ATTEMPTS must be above zero")
	return db
}
