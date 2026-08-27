package srosha

import "time"

// defaultTimeout bounds a call whose context carries no deadline of its own. It
// is a backstop and not a policy: a caller who cares passes a context that
// says so.
const defaultTimeout = 30 * time.Second

// idempotencyKeyBytes is the entropy behind a generated key. Sixteen bytes is
// 128 bits, which is a collision nobody will see, and it is hex on the wire so
// it reads as an ordinary string wherever it is stored or logged.
const idempotencyKeyBytes = 16
