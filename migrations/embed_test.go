package migrations_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/Serajian/srosha/migrations"
)

// Every .sql file is embedded. A migration that exists on disk and not in the
// binary is the failure this package exists to prevent, and it would otherwise
// show up as a table that is missing in production and present in development.
func TestEveryMigrationIsEmbedded(t *testing.T) {
	got, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("no migrations are embedded")
	}

	on, err := filepath.Glob("*.sql")
	if err != nil {
		t.Fatalf("glob on disk: %v", err)
	}
	if len(got) != len(on) {
		t.Errorf("%d embedded, %d on disk: %v vs %v", len(got), len(on), got, on)
	}
}

// Latest is what a binary compares the database against, so it has to be the
// real highest number and not the count of files.
func TestLatestIsTheHighestVersion(t *testing.T) {
	names, _ := fs.Glob(migrations.Files, "*.sql")

	if got := migrations.Latest(); got != int64(len(names)) {
		t.Logf("latest=%d files=%d -- equal only while numbering is unbroken", got, len(names))
	}
	if migrations.Latest() <= 0 {
		t.Fatal("Latest() is zero, so no binary could ever call itself up to date")
	}
}
