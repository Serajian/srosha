package source

// maxKeyLabelLen bounds what a customer names a key. It is shown back to them
// and stored, and nothing about telling two keys apart needs more than this.
const maxKeyLabelLen = 64

// maxNameLen is a bound of our own. A name only the customer sees, and anything
// near this is a paste that went wrong rather than a name.
const maxNameLen = 64
