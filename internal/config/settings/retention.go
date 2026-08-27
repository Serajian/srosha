package settings

import (
	"time"

	"github.com/Serajian/srosha/pkg/env"
)

// Retention is how long a message is kept. srosha is not an archive: a message
// and its deliveries answer "what happened to this", and past some age nobody
// asks -- so keeping them is a table that only grows, and slower queries for
// everyone who does ask.
//
// Deliveries have no age of their own. The foreign key takes them with the
// message, so there is one number here and not two that could disagree.
type Retention struct {
	Age time.Duration

	// Schedule is when the sweep runs, as a cron spec or an interval
	// descriptor. Nightly by default rather than every-so-many-hours, because a
	// heavy job should run at an hour somebody chose and not at whatever time
	// the process last restarted.
	Schedule string

	// Batch is how many messages one statement deletes. An unbounded DELETE over
	// a table that has been collecting for a year is a single transaction
	// holding locks on all of it.
	Batch int
}

// LoadRetentionAge reads how long a message is kept, and is read by **both**
// binaries. The dispatcher deletes by it; the gateway refuses a listing that
// reaches past it, because a short page that looks complete is worse than a
// refusal. Two loaders for one key would be two numbers that could disagree.
func LoadRetentionAge(r *env.Reader) time.Duration {
	age := r.Duration("RETENTION_AGE", 30*24*time.Hour)
	r.Check(age > 0, "NOTIF_RETENTION_AGE must be above zero")
	return age
}

func LoadRetention(r *env.Reader, d Dispatch) Retention {
	t := Retention{
		Age:      LoadRetentionAge(r),
		Schedule: r.Str("RETENTION_SCHEDULE", "0 3 * * *"),
		Batch:    r.Int("RETENTION_BATCH", 1000),
	}

	r.Check(t.Batch > 0, "NOTIF_RETENTION_BATCH must be above zero")
	r.Check(t.Schedule != "", "NOTIF_RETENTION_SCHEDULE must not be empty")

	// The sweep deletes by age alone, with no check that the deliveries settled,
	// and that is only safe while a delivery gives up long before a message is
	// dropped. Close the two together and the job starts deleting work recovery
	// would still have sent.
	r.Check(t.Age > d.ReconcileGiveUp*minRetentionMultiple,
		"NOTIF_RETENTION_AGE must be at least %d times NOTIF_RECONCILE_GIVE_UP",
		minRetentionMultiple)

	return t
}
