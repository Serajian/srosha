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
	Recipient Recipient
	Title     string
	Body      string
}
