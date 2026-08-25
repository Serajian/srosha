package crypto

// KeySize is the key length this package accepts, and it is the only one:
// AES-256, so that a shorter key is a configuration error caught at startup
// rather than a quietly weaker cipher.
const KeySize = 32

const (
	// version prefixes every sealed value. It names the algorithm, not the key
	// -- the key names itself in the next field. It exists for the day
	// AES-256-GCM is replaced, so old values can still be read while new ones
	// are written differently.
	version = "v1"

	// separator splits the four fields. A key id containing it would make the
	// value unparseable, so one is refused when the keyring is built, and the
	// two encoded fields use an alphabet that cannot produce it.
	separator = "."

	// fields is how many parts a sealed value has: version, key id, nonce,
	// ciphertext.
	fields = 4
)
