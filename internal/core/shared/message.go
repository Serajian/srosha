package shared

// DispatchEvent is one delivery waiting to be sent.
//
// It carries identifiers only. The address is deliberately absent: it may be
// someone's phone number, and an event sits in the broker's storage and its
// logs. Channel and Priority are here because the subject is built from them.
type DispatchEvent struct {
	DeliveryID ID       `json:"delivery_id"`
	SourceID   string   `json:"source_id"`
	Channel    Channel  `json:"channel"`
	Priority   Priority `json:"priority"`
}

// Message is everything a sender needs and nothing more. It has no tags: it
// never goes on a wire as it is, and each sender builds its provider's own
// payload from it.
type Message struct {
	// DeliveryID names this send, and is the same value on every attempt at it.
	//
	// It is here because some providers deduplicate on an id the caller chooses
	// -- Matrix will not create a second event for a transaction it has already
	// seen -- and a value made up per attempt throws that away. srosha already
	// has exactly the right value: a delivery is one message to one recipient,
	// and it keeps its id across every retry.
	//
	// A sender that has nothing to do with it ignores it.
	DeliveryID ID

	Recipient Recipient
	Title     string
	Body      string

	// Metadata is the source's own, carried through untouched.
	//
	// srosha does not read it and never will: it is the source's key-values,
	// stored with the message and handed to whoever sends it. A provider adapter
	// may look in it for what its own API needs -- which template to use, which
	// tag a message carries -- and no other provider is affected by that, because
	// nothing here defines what the keys mean.
	//
	// It is how a channel whose API wants more than a title and a body gets it,
	// without every channel's needs becoming fields on this struct.
	Metadata map[string]string
}
