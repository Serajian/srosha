package mailer

// Where the letters live. The trailing slash is part of it: this is joined to a
// filename, not to a path.
const emailDir = "templates/email/"

// letterSignInCode names both the file and the entry in the parsed set.
const letterSignInCode = "signin_code"

// What a person sees.
//
// The code is deliberately not in the subject: a subject shows in a
// notification on a locked screen, and the whole point of the code is that
// having the mailbox is what proves who you are.
const (
	subject = "Your srosha sign-in code"

	// The plain half of the letter. It is not a leftover: it is what a client
	// that will not render html shows, and it goes out alongside the html
	// every time.
	//
	// It says the same thing as signin_code.html, in the same words, on
	// purpose. Two halves that drift apart are two different messages, and the
	// person reading the wrong one is the person who cannot sign in.
	body = `Your sign-in code is:

    %s

Type it into the page that asked for it.
Ten minutes. One use. Three guesses.

If you didn't ask for this, somebody typed your address. Nothing has
happened to your account and there's nothing you need to do.
`

	contentType = "text/plain"
)
