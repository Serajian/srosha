package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// referenceHandler serves the SDK's README inside the portal.
//
// The senders form used to end at "what each channel wants is in the SDK's
// README", with no link -- and the README is in a module a customer only has
// once they already know what to import. So the document comes here, where the
// person who has not sent anything yet already is.
//
// It is the SAME file, copied by `make sdk-docs` rather than retold: a summary
// of a document is a second document, and the day they disagree the one on
// this page is the one somebody acted on. What keeps the copy honest is
// TestThePortalsCopyOfTheSDKReadmeIsCurrent, in package public.
//
// It goes through a template rather than out as bytes, which is the difference
// from the admin surface's architecture page: that one is a whole standalone
// html document, and this is markdown that needs the portal's chrome around it
// to be worth reading -- the nav, and a way back.
type referenceHandler struct{ doc string }

// referencePage is markdown in a <pre>, and Doc is escaped by html/template
// like any other field. Nothing here is rendered as html on purpose: the
// service holds no markdown renderer, and README.md is hard-wrapped and reads
// as text, which is what it was written to do.
type referencePage struct {
	chrome
	Doc string
}

// show writes the page. The bytes were read when the surface was built, so
// nothing here can fail.
func (h *referenceHandler) show(c *gin.Context) {
	c.HTML(http.StatusOK, pageReference, referencePage{chrome: wide, Doc: h.doc})
}
