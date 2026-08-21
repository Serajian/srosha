package env

// Secret is a string that does not print itself. Every token, password and DSN
// uses it, so logging a whole config struct cannot leak one by accident and
// only a deliberate Reveal gets the value out.
type Secret string

func (s Secret) String() string   { return "[REDACTED]" }
func (s Secret) GoString() string { return "[REDACTED]" }
func (s Secret) Reveal() string   { return string(s) }
func (s Secret) IsZero() bool     { return s == "" }
