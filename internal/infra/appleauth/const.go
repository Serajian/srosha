package appleauth

import "time"

// tokenLifetime is how long a minted token is handed out before a new one is
// made. It is not Apple's expiry, it is a choice inside Apple's two rules.
//
// A provider token must be refreshed at least every hour, and must not be made
// more often than once every twenty minutes -- ask too often and APNs answers
// TooManyProviderTokenUpdates. Forty-five minutes sits between the two with
// room on both sides.
const tokenLifetime = 45 * time.Minute

// maxCachedIdentities bounds the cache, exactly as googleauth's does and for
// the same reason: a rotated key leaves its predecessor behind, and an
// unbounded map of private keys is a leak that grows for the life of the
// process. Past the cap the whole map is dropped, which costs one signature per
// identity and never a wrong answer.
const maxCachedIdentities = 1024
