package googleauth

import "time"

// ScopeFCM is what a token for Firebase Cloud Messaging must be allowed to do.
// Named here rather than in the sender, because the sender is not the thing
// that talks to Google about permissions.
const ScopeFCM = "https://www.googleapis.com/auth/firebase.messaging"

// maxCachedAccounts bounds the cache. It is a cap on entries and not on memory:
// each entry is a parsed key and the last token it minted.
//
// It exists because a rotated service account leaves its predecessor behind --
// nothing tells us the old one is finished with -- and an unbounded map of
// private keys is a leak that grows for as long as the process lives. Past the
// cap the whole map is dropped, which costs one exchange per account and never
// a wrong answer.
const maxCachedAccounts = 1024

// refreshTimeout bounds one exchange with Google.
//
// A bound of its own rather than the caller's: this call is not the message, and
// a source waiting on a push should not be the reason a token refresh is given
// up on halfway.
const refreshTimeout = 30 * time.Second
