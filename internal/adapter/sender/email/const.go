package email

// contentTypes is what a source may say its body is.
//
// One body and not two: the core carries a single Body, so a
// multipart/alternative would mean inventing the other half here -- and a plain
// text version made by stripping tags is a guess at what somebody meant to say.
const (
	TypePlain = "text/plain"
	TypeHTML  = "text/html"
)
