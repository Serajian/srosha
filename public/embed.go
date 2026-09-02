// Package public carries everything this service renders or serves: the HTML it
// renders on the server, the files a browser fetches, and the letters it mails.
//
// It lives at the repository root rather than beside the handlers so that
// somebody changing a page or a stylesheet does not have to go looking inside
// the Go tree for it. It is still compiled into the binary -- go:embed cannot
// reach outside its own directory, which is the whole reason this file exists.
//
// The three are NOT the same thing, and the split is the point:
//
//	static/     a browser may fetch these, byte for byte
//	templates/  rendered on the server, and never served
//	guarded/    a whole document, read by a route, only behind a guard
//
// guarded/ was added because neither of the other two could hold it: a whole
// document that not everybody signed in may read. Under static/ it would be
// public to anybody who guessed the url; under templates/ it would be PARSED
// as a template, which it is not -- and the SDK's README below is full of the
// braces that would take that literally. Nothing serves this half as a file
// system -- a route reads one named file and hands it back, so the guard is on
// the route rather than on the directory.
//
// Two of them now, and they leave by different doors, which is worth being
// explicit about:
//
//	admin/architecture.html  standalone html, written out as bytes
//	portal/sdk.md            markdown, handed to a page as a field
//
// The second still never becomes a template. It is data inside one, escaped
// like any other field, so the rule above is untouched: nothing in this
// directory is parsed. It is a COPY of sdk/go/README.md, which an embed
// directive cannot reach and this module may not import -- `make sdk-docs`
// refreshes it and a test in this package fails while it is stale.
//
// Under templates/ the subdirectory is the surface: portal/ and admin/ are
// pages, email/ is what goes in the post. They are parsed separately, and none
// of them can reach another's files.
//
// Serving templates/ would hand out the shape of every page and every field
// name in one request. Nothing here may point a file server at the root of this
// FS -- see web.assets, which subs into static/ before serving anything.
package public

import "embed"

//go:embed static templates guarded
var Files embed.FS
