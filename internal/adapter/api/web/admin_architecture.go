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
// super_admin only, for the same kind of reason /audit is: the diagram names
// the hosts, the ports, the stores and the network they sit on. That is the
// shape of the deployment, and an operator approving sources has no call to
// read it.
type architectureHandler struct{ page []byte }

// show writes the document. The bytes were read when the surface was built,
// so nothing here can fail.
func (h *architectureHandler) show(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", h.page)
}
