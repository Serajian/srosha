package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// Migration is how long one migration run may wait for another.
//
// There is no directory here: the sql is compiled into the binary, so which
// files a build applies is a fact about the build and not about what somebody
// copied next to it. See the migrations package at the repository root.
type Migration struct {
	// LockTimeout bounds how long this process waits for another migration to
	// finish before giving up. Two deploys at once is the case it exists for,
	// and waiting forever would turn that into a hung release rather than a
	// failed one.
	LockTimeout time.Duration
}

func LoadMigration(r *env.Reader) Migration {
	return Migration{
		LockTimeout: r.Duration("MIGRATION_LOCK_TIMEOUT", 5*time.Minute),
	}
}
