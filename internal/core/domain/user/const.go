package user

// maxEmailLen is a bound of our own. RFC 5321 allows 254 for the whole
// address, and anything near it is a paste that went wrong rather than a
// mailbox.
const maxEmailLen = 254
