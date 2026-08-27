package fcm

// PointAt aims a sender at a test server. FCM has one host and it is Google's,
// so unlike Matrix there is no configured address to redirect.
func (s *Sender) PointAt(url string) { s.url = url }

// URL is what the sender will call.
func (s *Sender) URL() string { return s.url }
