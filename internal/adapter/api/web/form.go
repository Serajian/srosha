package web

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
)

// formValue reads one value from a posted form, with the body bounded.
//
// The bound is set here rather than left to gin: gin's MaxMultipartMemory is
// about multipart uploads, and these surfaces post short fields and never a
// file. A body that hit the limit yields an empty string, which the core
// refuses the way it refuses any other bad input.
func formValue(log *slog.Logger, c *gin.Context, name string) string {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFormBytes)

	if err := c.Request.ParseForm(); err != nil {
		log.WarnContext(c.Request.Context(), "form could not be read", "error", err)
		return ""
	}
	return c.Request.PostFormValue(name)
}

// urlValue escapes what goes in a query string. An address is not a secret --
// whoever is looking at the screen typed it -- but it is untrusted text, and
// untrusted text is never pasted into a url unescaped.
func urlValue(s string) string { return url.QueryEscape(s) }
