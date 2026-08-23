package auth

// What a key looks like. These are a format, not a policy, which is why they
// are here and not in config: changing them changes what an already-issued key
// is, and every key in every customer's deployment would stop matching.
const (
	// prefix makes a found string identifiable. A secret scanner can be taught
	// one pattern, and a person staring at an unknown value in a log can tell
	// what they are holding -- neither is possible with 43 random characters.
	prefix = "srosha_"

	// randomBytes is the entropy behind the key. 32 bytes is 256 bits: the same
	// as the hash it is stored under, so neither half is the weak one.
	randomBytes = 32

	// bodyLen is what randomBytes becomes in unpadded base64url: ceil(32/3)*4
	// minus the padding. Written out so length checks do not have to recompute
	// it, and so a change to randomBytes fails a test rather than passing
	// quietly.
	bodyLen = 43
)
