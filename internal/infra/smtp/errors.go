package smtp

import (
	"errors"
	"fmt"
	"strings"
)

// Error is a failure that carries the server's own reply code.
//
// SMTP answers "is this worth another attempt" itself, in the first digit of
// every reply -- 4xx says try later, 5xx says never. That digit is the one part
// of an SMTP failure that is not free text, and the only reason this type
// exists: whoever decides about retries needs it, and would otherwise be reading
// the server's English.
type Error struct {
	// Code is the three-digit reply, or zero when it never got that far: dns,
	// dial, tls, a connection dropped mid-session.
	Code int

	Op  string
	Err error
}

func (e *Error) Error() string {
	if e.Code == 0 {
		return fmt.Sprintf("smtp: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("smtp: %s: %d: %v", e.Op, e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// ReplyCode is how a caller reads the code without importing this package: any
// error with this method carries one.
func (e *Error) ReplyCode() int { return e.Code }

// wrap reads the reply code out of whatever the client returned.
//
// There is no typed error underneath -- the SMTP client formats the server's
// line into a message -- so the digits are read from the text. Normally the
// thing never to do, but here the digits ARE the protocol: RFC 5321 fixes them,
// and nothing else in the line is defined.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: replyCode(err.Error()), Op: op, Err: errors.New(redact(err.Error()))}
}

func replyCode(msg string) int {
	for i := 0; i+3 <= len(msg); i++ {
		if i > 0 && !isBoundary(msg[i-1]) {
			continue
		}
		if !isDigit(msg[i]) || !isDigit(msg[i+1]) || !isDigit(msg[i+2]) {
			continue
		}
		// A code is followed by a space, a hyphen in a multiline reply, or the
		// end of the line. Anything else is a number that happens to be three
		// digits long.
		if i+3 < len(msg) && !isBoundary(msg[i+3]) && msg[i+3] != '-' {
			continue
		}

		if code := int(msg[i]-'0')*100 + int(msg[i+1]-'0')*10 + int(msg[i+2]-'0'); code >= 200 &&
			code <= 599 {
			return code
		}
	}
	return 0
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isBoundary(b byte) bool {
	switch b {
	case ' ', ':', '\t', '\n', '"', '(':
		return true
	}
	return false
}

// redact keeps a password out of an error. A client that fails to authenticate
// quotes what it sent, and what it sent includes the credential.
func redact(s string) string {
	if i := strings.Index(strings.ToLower(s), "password"); i >= 0 {
		return s[:i] + "[REDACTED]"
	}
	return s
}
