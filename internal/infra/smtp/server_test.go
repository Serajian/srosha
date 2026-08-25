package smtp_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// certificate makes one for 127.0.0.1, valid for an hour.
//
// A test server that spoke no TLS could not reach the authentication path at
// all: no auth mechanism will send a password over a connection in the clear,
// which is the library behaving correctly and the reason this exists.
func certificate(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "srosha test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// server is enough SMTP to be talked to, and no more.
//
// A real server, or a library's own, would answer what it thinks is right. The
// whole point here is to answer what the test says: a 550 on RCPT and a 451 on
// DATA are different conclusions, and nothing else can produce both on demand.
type server struct {
	mu   sync.Mutex
	sess []string // what was said to it, in order

	// replies overrides the answer to one verb: "RCPT TO" -> "550 no such user".
	replies map[string]string
	tlsCfg  *tls.Config

	// noTLS makes the server offer no STARTTLS, which a client must refuse to
	// talk to rather than continue in the clear.
	noTLS bool
}

func newServer(t *testing.T, replies map[string]string) (*server, string, int) {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	s := &server{replies: replies, tlsCfg: &tls.Config{
		Certificates: []tls.Certificate{certificate(t)},
		MinVersion:   tls.VersionTLS12,
	}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return s, addr.IP.String(), addr.Port
}

func (s *server) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	// upgrade rebinds the reader and writer onto the encrypted connection, which
	// is what STARTTLS is: the same session, continued.
	upgrade := func() bool {
		tc := tls.Server(conn, s.tlsCfg)
		if tc.HandshakeContext(context.Background()) != nil {
			return false
		}
		conn = tc
		r, w = bufio.NewReader(tc), bufio.NewWriter(tc)
		return true
	}

	say := func(format string, args ...any) bool {
		_, _ = fmt.Fprintf(w, format+"\r\n", args...)
		return w.Flush() == nil
	}

	if !say("220 test.srosha ESMTP") {
		return
	}

	inData := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false
				if !s.answer(say, "DATA-END", "250 2.0.0 Ok: queued") {
					return
				}
			}
			continue
		}

		s.record(line)
		verb := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(verb, "EHLO"):
			// STARTTLS first, then AUTH -- the order a client sees them in.
			// PIPELINING is deliberately not offered, so the conversation stays
			// lockstep and readable.
			caps := "250-test.srosha\r\n250-STARTTLS\r\n250 AUTH PLAIN LOGIN\r\n"
			if s.noTLS {
				caps = "250-test.srosha\r\n250 AUTH PLAIN LOGIN\r\n"
			}
			_, _ = fmt.Fprintf(w, "%s", caps)
			if w.Flush() != nil {
				return
			}
		case strings.HasPrefix(verb, "STARTTLS"):
			if !say("220 2.0.0 Ready to start TLS") {
				return
			}
			if !upgrade() {
				return
			}
		case strings.HasPrefix(verb, "HELO"):
			if !say("250 test.srosha") {
				return
			}
		case strings.HasPrefix(verb, "AUTH"):
			if !s.answer(say, "AUTH", "235 2.7.0 Authentication successful") {
				return
			}
		case strings.HasPrefix(verb, "MAIL FROM"):
			if !s.answer(say, "MAIL FROM", "250 2.1.0 Ok") {
				return
			}
		case strings.HasPrefix(verb, "RCPT TO"):
			if !s.answer(say, "RCPT TO", "250 2.1.5 Ok") {
				return
			}
		case strings.HasPrefix(verb, "DATA"):
			if reply, ok := s.replies["DATA"]; ok {
				if !say("%s", reply) {
					return
				}
				continue
			}
			inData = true
			if !say("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
		case strings.HasPrefix(verb, "QUIT"):
			_ = say("221 2.0.0 Bye")
			return
		default:
			if !say("250 2.0.0 Ok") {
				return
			}
		}
	}
}

func (s *server) answer(say func(string, ...any) bool, verb, ok string) bool {
	if reply, has := s.replies[verb]; has {
		return say("%s", reply)
	}
	return say("%s", ok)
}

func (s *server) record(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sess = append(s.sess, line)
}

func (s *server) said() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sess...)
}
