package source

// maxKeyLabelLen bounds what a customer names a key. It is shown back to them
// and stored, and nothing about telling two keys apart needs more than this.
const maxKeyLabelLen = 64

// maxNameLen is a bound of our own. A name only the customer sees, and anything
// near this is a paste that went wrong rather than a name.
const maxNameLen = 64

// maxDescriptionLen bounds what a customer writes about a source. Long enough
// for a sentence saying what it is for, short enough that it stays a label
// rather than becoming documentation nobody reads.
const maxDescriptionLen = 280

// maxReviewNoteLen bounds an operator's reason. A sentence or two, because the
// customer reads it on a page and a refusal is not a support ticket.
const maxReviewNoteLen = 500
