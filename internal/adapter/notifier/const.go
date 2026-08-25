package notifier

// The callback's headers. They are this service's public contract with every
// source that verifies a signature, so they are constants here rather than
// config: changing one changes what every existing receiver has to check.
const (
	signatureHeader = "X-Srosha-Signature"
	timestampHeader = "X-Srosha-Timestamp"
)

// signatureVersion prefixes the signature, so the day the algorithm changes a
// receiver can accept both while it moves. Without it, changing the algorithm
// means every source has to cut over in the same minute as us.
const signatureVersion = "v1"

// signedSeparator joins the timestamp to the body before signing.
//
// A separator that cannot appear in the timestamp is what stops the two fields
// from being confused: without one, a timestamp of 1 and a body of "23..." sign
// the same bytes as a timestamp of 12 and a body of "3...".
const signedSeparator = "."

// maxResponseBytes bounds what is read back from a callback.
//
// The body is not used at all -- only the status matters -- but it has to be
// drained for the connection to be reusable, and the address belongs to
// somebody else: an endpoint answering with an endless body would otherwise
// hold a goroutine and grow memory until the timeout.
const maxResponseBytes = 4 << 10
