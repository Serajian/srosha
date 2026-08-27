package srosha

import "time"

// The headers srosha signs a callback with. They are its public contract with
// every receiver, which is why they are constants here and not something a
// caller passes in.
const (
	SignatureHeader = "X-Srosha-Signature"
	TimestampHeader = "X-Srosha-Timestamp"
)

// signatureVersion prefixes the signature, so the day the algorithm changes a
// receiver can accept both while it moves.
const signatureVersion = "v1"

// signedSeparator joins the timestamp to the body before signing.
//
// A separator that cannot appear in the timestamp is what stops the two fields
// being confused: without one, a timestamp of 1 and a body of "23…" sign the
// same bytes as a timestamp of 12 and a body of "3…".
const signedSeparator = "."

// defaultTolerance is how far a callback's timestamp may be from now.
//
// It is what stops a replay: an attacker who captured a callback can post it
// again for exactly this long and no longer. Five minutes is enough slack for
// two machines whose clocks were never synchronized to each other, and short
// enough that a captured callback is stale before anybody could use it.
const defaultTolerance = 5 * time.Minute
