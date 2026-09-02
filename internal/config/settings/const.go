package settings

// minRetentionMultiple is how much longer a message is kept than a delivery is
// tried for.
//
// Retention deletes by age alone -- no check that the deliveries settled -- and
// that reasoning holds only while the two numbers are far apart: a delivery
// gives up in minutes, so one still pending a month later is a row recovery
// never saw rather than work waiting to happen. Bring them close and the sweep
// starts deleting messages that would still have gone out.
//
// With the defaults the real ratio is over a thousand, so this is never in the
// way. It is here for the day somebody changes one number without the other.
const minRetentionMultiple = 24

// maxDiskFloorGB bounds NOTIF_ALERT_DISK_FLOOR_GB. Not a real limit on disks,
// just far enough above any of them that a value past it is a typo rather than
// an intention -- and low enough that the shift to bytes cannot overflow.
const maxDiskFloorGB = 1 << 20 // a petabyte, in gigabytes

// maxDiskFloor is the same bound in bytes.
const maxDiskFloor = uint64(maxDiskFloorGB) << 30

// minCredentialLen is the shortest password production accepts inside a
// connection url. Not a strength calculation: it is far enough below the
// `openssl rand -hex 24` the documentation asks for that following the
// documentation always passes, and far enough above the eight characters found
// in production on 2026-09-01 that the thing which went unnoticed cannot.
const minCredentialLen = 16

// A NATS user seed: "S" for seed, "U" for user, then base32.
const (
	nkeyUserSeedPrefix = "SU"
	nkeySeedLen        = 58
)
