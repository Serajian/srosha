package web

import (
	"io/fs"

	"github.com/Serajian/srosha/public"
)

// browserFiles is one surface's static assets and nothing else.
//
// It subs into that surface's directory deliberately, twice over: public.Files
// carries the templates as well, and a file server pointed at its root would
// hand out the shape of every page and every field name in one request -- and
// each surface's assets are its own besides.
func browserFiles(surface string) (fs.FS, error) {
	return fs.Sub(public.Files, "static/"+surface)
}

// guardedFile is one document a surface serves whole, to some of the people
// signed in and not all of them.
//
// It reads ONE named file rather than handing back an fs.FS, and that is the
// difference from browserFiles above: a file system behind a route would let a
// path in the request choose the file, and the guard is then protecting a
// directory whose contents nobody listed. Here the caller names the file at
// startup and a request chooses nothing.
//
// It is read once, when the surface is built, so a missing file is a console
// that will not start rather than a page that 500s the first time somebody
// opens it.
func guardedFile(surface, name string) ([]byte, error) {
	return public.Files.ReadFile("guarded/" + surface + "/" + name)
}
