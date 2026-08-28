package web

import (
	"html/template"
	"net/http"

	"github.com/Serajian/srosha/public"

	"github.com/gin-gonic/gin/render"
)

// pageRender is a surface's gin.HTMLRender.
//
// Gin's own loaders put every template in one set, and every page defines
// "content" -- so one set would let only the last one parsed win, silently,
// with a blank page at the end of it. This keeps one set per page and hands gin
// the right one by name.
type pageRender struct{ pages map[string]*template.Template }

// newPageRender parses one template set per page, from one surface's directory.
//
// The surface is a parameter rather than a constant because that is what makes
// this shared: the portal reads templates/portal, the admin surface reads
// templates/admin, and neither can reach the other's.
func newPageRender(surface string, names ...string) (pageRender, error) {
	pages := make(map[string]*template.Template, len(names))
	dir := "templates/" + surface + "/"

	for _, name := range names {
		t, err := template.ParseFS(public.Files, dir+"layout.html", dir+name+".html")
		if err != nil {
			return pageRender{}, err
		}
		pages[name] = t
	}
	return pageRender{pages: pages}, nil
}

// Instance is what gin calls for c.HTML(status, name, data). Every page is
// rendered through its layout, which is why the name gin executes is always
// "layout" and never the page's own.
func (p pageRender) Instance(name string, data any) render.Render {
	t, ok := p.pages[name]
	if !ok {
		return missingPage{name: name}
	}
	return render.HTML{Template: t, Name: "layout", Data: data}
}

// missingPage is unreachable unless a handler names a page newPageRender was
// not given. gin's Instance cannot return an error, so this fails visibly
// rather than dereferencing a nil template.
type missingPage struct{ name string }

func (missingPage) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
}

func (m missingPage) Render(w http.ResponseWriter) error {
	m.WriteContentType(w)
	_, err := w.Write([]byte("no such page: " + m.name))
	return err
}
