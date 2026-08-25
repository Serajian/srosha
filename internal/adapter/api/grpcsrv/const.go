package grpcsrv

// How a key is presented. These are the wire's vocabulary, not a policy, which
// is why they are here rather than in config: changing either one changes what
// every existing client has to send.
const (
	// authHeader is lower case because gRPC metadata keys are, always. Reading
	// md.Get("Authorization") finds nothing and would look like a caller who
	// sent no key at all.
	authHeader = "authorization"

	// bearerPrefix is matched without regard to case. RFC 7235 says the scheme
	// is case-insensitive, and a client sending "bearer" is not making a
	// mistake we should answer with "invalid credentials".
	bearerPrefix = "bearer "
)
