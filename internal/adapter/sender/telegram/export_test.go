package telegram

// WithBaseURL points a sender at a test server. This file is compiled only into
// tests, so the address stays a constant everywhere else.
func (s *Sender) WithBaseURL(u string) *Sender {
	s.baseURL = u
	return s
}
