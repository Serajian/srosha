package web

import (
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/internal/core/domain/user"
	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/gin-gonic/gin"
)

// peopleHandler is the roster and the two things a super_admin does to it:
// change a role, switch off sign-in. Both routes it serves are behind the
// second guard in NewAdmin -- an admin has no page that shows this, and no
// reason to have one.
type peopleHandler struct {
	ops Operators
	log *slog.Logger
}

type (
	peoplePage struct {
		adminChrome
		People  []user.User
		Problem string
	}
	// personPage carries IsSelf so the page can say why the two forms are
	// missing rather than just omitting them -- the use case refuses SetRole
	// and SetPersonActive against the viewer's own account either way, but a
	// missing form with no explanation reads as broken.
	personPage struct {
		adminChrome
		Person  *user.User
		IsSelf  bool
		Problem string
	}
)

func (h *peopleHandler) list(c *gin.Context) {
	actor := signedInUser(c)

	people, err := h.ops.People(c.Request.Context(), actor)
	if err != nil {
		h.log.ErrorContext(c.Request.Context(), "could not list people", "error", err)
		c.HTML(http.StatusOK, pagePeople,
			peoplePage{adminChrome: chromeFor(actor), Problem: message(err)})
		return
	}
	c.HTML(http.StatusOK, pagePeople, peoplePage{adminChrome: chromeFor(actor), People: people})
}

func (h *peopleHandler) show(c *gin.Context) {
	actor := signedInUser(c)
	id := shared.ID(c.Param("id"))

	person, ok := h.readPerson(c, id)
	if !ok {
		return
	}
	c.HTML(http.StatusOK, pagePerson, personPage{
		adminChrome: chromeFor(actor), Person: person, IsSelf: person.ID == actor.ID,
	})
}

// setRole changes what somebody may do. The use case is what actually refuses
// a super_admin naming their own account -- this only reads the form and
// reports what came back.
func (h *peopleHandler) setRole(c *gin.Context) {
	id := shared.ID(c.Param("id"))
	role := user.Role(formValue(h.log, c, fieldRole))
	note := formValue(h.log, c, fieldNote)

	err := h.ops.SetRole(c.Request.Context(), signedInUser(c), id, role, note)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "role not changed", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathPeople+"/"+id.String())
}

// setActive switches whether somebody may sign in at all.
func (h *peopleHandler) setActive(c *gin.Context) {
	id := shared.ID(c.Param("id"))
	on := formValue(h.log, c, fieldActive) == "true"
	note := formValue(h.log, c, fieldNote)

	err := h.ops.SetPersonActive(c.Request.Context(), signedInUser(c), id, on, note)
	if err != nil {
		h.log.WarnContext(c.Request.Context(), "account not changed", "error", err)
		h.showWith(c, id, message(err))
		return
	}
	c.Redirect(http.StatusSeeOther, pathPeople+"/"+id.String())
}

// readPerson is the one read every route above needs, and the one answer for
// somebody who is not there.
func (h *peopleHandler) readPerson(c *gin.Context, id shared.ID) (*user.User, bool) {
	person, err := h.ops.Person(c.Request.Context(), signedInUser(c), id)
	if err != nil {
		personNotFound(c)
		return nil, false
	}
	return person, true
}

// showWith renders the person page again with something gone wrong on it.
func (h *peopleHandler) showWith(c *gin.Context, id shared.ID, problem string) {
	actor := signedInUser(c)

	person, ok := h.readPerson(c, id)
	if !ok {
		return
	}
	c.HTML(http.StatusOK, pagePerson, personPage{
		adminChrome: chromeFor(actor), Person: person, IsSelf: person.ID == actor.ID,
		Problem: problem,
	})
}

// personNotFound is the one answer for an account that is not there, the same
// way notFound is for a source.
func personNotFound(c *gin.Context) { c.String(http.StatusNotFound, "no such person") }
