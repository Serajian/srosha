package notifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// sign produces the value of the signature header.
//
// HMAC-SHA256 over the timestamp and the body together, in that order:
//
//	v1=hex(hmac(secret, "<unix seconds>.<body>"))
//
// The timestamp is signed rather than merely sent, which is the whole point. A
// signature over the body alone is valid for ever, so anybody who once saw a
// callback can replay it whenever they like and it verifies. Signed together,
// a receiver refuses one that is too old and a replay has to forge the pair.
//
// hex rather than base64: it is what every webhook a customer has integrated
// before uses, and a constant-time compare is what they will do with it either
// way.
func sign(secret string, at time.Time, body []byte) (signature, timestamp string) {
	timestamp = strconv.FormatInt(at.Unix(), 10)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte(signedSeparator))
	mac.Write(body)

	return signatureVersion + "=" + hex.EncodeToString(mac.Sum(nil)), timestamp
}
