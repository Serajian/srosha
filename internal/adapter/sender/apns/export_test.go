package apns

// PointAt aims a sender at a test server. APNs has two hosts and both are
// Apple's, so there is no configured address to redirect.
func (s *Sender) PointAt(url string) { s.host = url }

// Host is which of Apple's two services this sender will call.
func (s *Sender) Host() string { return s.host }

// NotificationID is the apns-id a delivery id becomes.
var NotificationID = notificationID
