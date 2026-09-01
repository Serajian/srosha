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
//	guarded/    fetched byte for byte, but only behind a guard
//
// guarded/ was added because neither of the other two could hold it: a
// document served whole, with no template in it, that not everybody signed in
// may read. Under static/ it would be public to anybody who guessed the url;
// under templates/ it would be parsed as a template, which it is not. Nothing
// serves this half as a file system -- a route reads one named file and hands
// it back, so the guard is on the route rather than on the directory.
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
