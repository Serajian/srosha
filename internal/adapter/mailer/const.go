package mailer

// What a person sees.
//
// The code is deliberately not in the subject: a subject shows in a
// notification on a locked screen, and the whole point of the code is that
// having the mailbox is what proves who you are.
const (
	subject = "Your srosha sign-in code"

	// Plain text, because six digits need no markup and a client that will not
	// render html still shows this.
	body = `Your sign-in code is:

    %s

It can be used once, and expires shortly.

If you did not ask for this, somebody typed your address. Nothing has
happened to your account and there is nothing you need to do.
`

	contentType = "text/plain"
)
