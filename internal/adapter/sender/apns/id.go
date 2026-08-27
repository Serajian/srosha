package apns

import (
	"encoding/hex"

	"github.com/Serajian/srosha/internal/core/shared"

	"github.com/oklog/ulid/v2"
)

// notificationID turns a delivery id into the apns-id header.
//
// Both are 128 bits, so this is a rewriting and not a mapping: the same value
// in the shape Apple asks for. It matters because apns-id is what Apple's own
// reports and error responses name a notification by -- send none and Apple
// invents one, and then the row in deliveries and the thing Apple can tell you
// about cannot be lined up at all.
//
// This is not deduplication and APNs offers none: a redelivered message is
// pushed again. What it buys is that "did this arrive" has one id, not two.
func notificationID(id shared.ID) (string, bool) {
	parsed, err := ulid.ParseStrict(id.String())
	if err != nil {
		// Nothing is lost by sending no header: Apple makes an id up. So a
		// delivery id that is somehow not a ULID costs correlation, not a
		// message.
		return "", false
	}

	// 8-4-4-4-12, which is the canonical form and the only one Apple documents.
	var out [36]byte
	hex.Encode(out[0:8], parsed[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], parsed[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], parsed[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], parsed[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], parsed[10:16])

	return string(out[:]), true
}
