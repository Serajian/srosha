package web

import (
	"log/slog"
	"net/http"
	"net/url"
)

// formValue reads one value from a posted form, with the body bounded.
//
// A form nobody could parse yields an empty string, which the core refuses the
// way it refuses any other bad input -- there is nothing useful to say here
// that it will not say better. The log line is for the other case: a body that
// hit the size limit, which is somebody probing rather than somebody typing.
func formValue(log *slog.Logger, r *http.Request, name string) string {
	r.Body = http.MaxBytesReader(nil, r.Body, maxFormBytes)

	if err := r.ParseForm(); err != nil {
		log.WarnContext(r.Context(), "form could not be read", "error", err)
		return ""
	}
	return r.PostFormValue(name)
}

// urlValue escapes what goes in a query string. The address is not a secret --
// whoever is looking at the screen typed it -- but it is untrusted text, and
// untrusted text is never pasted into a url unescaped.
func urlValue(s string) string { return url.QueryEscape(s) }
