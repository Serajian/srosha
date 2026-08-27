package whatsapp

// WithBaseURL points a sender at a test server. This file is compiled only into
// tests, so a real one always calls the address its constant says.
func (s *Sender) WithBaseURL(u string) *Sender {
	s.baseURL = u
	return s
}
