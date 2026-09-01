package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// architectureHandler hands back the diagram of this service: what runs, what
// it talks to, and what is inside the private network.
//
// It is the one thing this surface serves that is a document rather than a
// page. There is no layout around it and no nav in it -- it is a whole
// standalone html file, generated from docs/assets/brand/srosha.architecture.json
// and rendered elsewhere -- so it goes out as bytes and never through
// pageRender.
//
// Every operator, not only a super_admin. It was the other way for a day, on
// the argument that naming the hosts, the ports and the stores is the shape of
// the deployment -- and the owner widened it, because an operator judging
// whether a source may send is being asked about a system they could not see
// the shape of. `operator` still stands in front of it, so no customer reaches
// it.
type architectureHandler struct{ page []byte }

// show writes the document. The bytes were read when the surface was built,
// so nothing here can fail.
func (h *architectureHandler) show(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", h.page)
}
