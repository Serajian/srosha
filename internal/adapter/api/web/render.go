package web

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/Serajian/srosha/public"
)

// renderer turns a page and its data into html. It is the only thing in this
// package that knows templates exist.
type renderer struct {
	pages map[string]*template.Template
	log   *slog.Logger
}

// newRenderer parses one template set per page, not one for everything.
//
// Every page defines "content", so a single set would let only the last one
// parsed win -- silently, with no error and a blank page at the end of it.
func newRenderer(log *slog.Logger, names ...string) (*renderer, error) {
	pages := make(map[string]*template.Template, len(names))

	for _, name := range names {
		t, err := template.ParseFS(
			public.Files,
			"templates/portal/layout.html",
			"templates/portal/"+name+".html",
		)
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	return &renderer{pages: pages, log: log}, nil
}

// page writes one.
//
// A template that fails halfway has already written part of a body, so the
// failure only reaches the log: there is nothing left to say to the browser
// that would not land in the middle of the html it already has.
func (rn *renderer) page(w http.ResponseWriter, r *http.Request, name string, data any) {
	t, ok := rn.pages[name]
	if !ok {
		rn.log.ErrorContext(r.Context(), "no such page", "page", name)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		rn.log.ErrorContext(r.Context(), "page failed to render", "page", name, "error", err)
	}
}
