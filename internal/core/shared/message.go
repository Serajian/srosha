package shared

// DispatchEvent is one delivery waiting to be sent.
//
// It carries identifiers only. The address is deliberately absent: it may be
// someone's phone number, and an event sits in the broker's storage and its
// logs. Channel and Priority are here because the subject is built from them.
type DispatchEvent struct {
	DeliveryID ID
	SourceID   string
	Channel    Channel
	Priority   Priority
}

// Message is everything a sender needs and nothing more.
type Message struct {
	Recipient Recipient
	Title     string
	Body      string
}
