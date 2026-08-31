// Package migrations carries the sql that shapes the database.
//
// It lives at the repository root rather than inside the Go tree so that
// somebody adding a migration does not have to go looking for it, and so goose
// on a laptop can read the same directory. It is compiled into every binary
// besides -- go:embed cannot reach outside its own directory, which is the
// whole reason this file exists.
//
// Embedding is not a convenience here. A binary that carries its own sql can
// never be run against a schema from a different commit without noticing: the
// version it expects is a fact about the binary, not about a directory
// somebody remembered to copy.
package migrations

import (
	"embed"
	"io/fs"
	"strconv"
	"strings"
)

//go:embed *.sql
var Files embed.FS

// Latest is the highest version these files define, which is the version a
// binary built from them expects the database to be at.
//
// Zero when there are none, which cannot happen in a build that compiled: the
// embed above fails at compile time on an empty match.
func Latest() int64 {
	entries, err := fs.ReadDir(Files, ".")
	if err != nil {
		return 0
	}

	var latest int64
	for _, e := range entries {
		name, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(name, 10, 64)
		if err == nil && v > latest {
			latest = v
		}
	}
	return latest
}
