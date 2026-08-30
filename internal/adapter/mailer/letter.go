package mailer

import (
	"bytes"
	"html/template"

	"github.com/Serajian/srosha/pkg/errs"
	"github.com/Serajian/srosha/public"
)

// letters is the mail this service sends, parsed once at startup.
//
// One template set per letter rather than one set for all of them, for the same
// reason the pages are parsed that way: every letter defines "content", so a
// single set would let only the last one parsed win, silently, and the letters
// before it would go out empty.
type letters struct{ set map[string]*template.Template }

// newLetters parses one set per named letter from templates/email.
//
// A letter that will not parse stops the binary here rather than at the moment
// somebody asks to sign in, which is the only useful time to find out.
func newLetters(names ...string) (letters, error) {
	set := make(map[string]*template.Template, len(names))

	for _, name := range names {
		t, err := template.ParseFS(
			public.Files, emailDir+"layout.html", emailDir+name+".html",
		)
		if err != nil {
			return letters{}, errs.InternalErr("a letter could not be read").
				WithStr(name).
				WithErr(err)
		}
		set[name] = t
	}
	return letters{set: set}, nil
}

// render returns one letter as html. The layout is what is executed, never the
// letter's own file, because a letter only ever defines "content".
func (l letters) render(name string, data any) (string, error) {
	t, ok := l.set[name]
	if !ok {
		return "", errs.InternalErr("no such letter").WithStr(name)
	}

	var out bytes.Buffer
	if err := t.ExecuteTemplate(&out, "layout", data); err != nil {
		return "", errs.InternalErr("a letter could not be written").
			WithStr(name).
			WithErr(err)
	}
	return out.String(), nil
}

// signInCode is what templates/email/signin_code.html reads.
type signInCode struct{ Code string }
