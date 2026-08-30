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
