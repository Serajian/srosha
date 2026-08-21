// The address check is the security control this package exists for, so it is
// tested directly rather than through whatever a dial happens to normalize.
package httpclient

import (
	"strings"
	"testing"
)

func TestPrivateAddressesAreRefused(t *testing.T) {
	refused := []struct {
		name    string
		address string
	}{
		{"loopback", "127.0.0.1:443"},
		{"loopback v6", "[::1]:443"},
		{"rfc1918 ten", "10.0.0.1:443"},
		{"rfc1918 172", "172.16.0.1:443"},
		{"rfc1918 192.168", "192.168.1.1:443"},
		{"the cloud metadata endpoint", "169.254.169.254:80"},
		{"unspecified", "0.0.0.0:443"},
		{"unique local v6", "[fd00::1]:443"},
		{"multicast", "224.0.0.1:443"},
		{"an rfc1918 address wearing an ipv6 coat", "[::ffff:10.0.0.1]:443"},
		{"loopback wearing an ipv6 coat", "[::ffff:127.0.0.1]:443"},
	}

	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			if err := denyPrivate("tcp", tt.address, nil); err == nil {
				t.Fatalf("%s was allowed", tt.address)
			}
		})
	}
}

func TestPublicAddressesAreAllowed(t *testing.T) {
	allowed := []string{"1.1.1.1:443", "93.184.216.34:80", "[2606:4700:4700::1111]:443"}

	for _, address := range allowed {
		t.Run(address, func(t *testing.T) {
			if err := denyPrivate("tcp", address, nil); err != nil {
				t.Fatalf("%s was refused: %v", address, err)
			}
		})
	}
}

// Control is handed an already-resolved address. Anything else means the
// assumption this check rests on is wrong, and refusing is the safe answer.
func TestAnUnresolvedAddressIsRefused(t *testing.T) {
	for _, address := range []string{"example.com:443", "not-an-address", ""} {
		t.Run(address, func(t *testing.T) {
			err := denyPrivate("tcp", address, nil)
			if err == nil {
				t.Fatalf("%q was allowed", address)
			}
			if !strings.Contains(err.Error(), "httpclient") {
				t.Errorf("error does not name the package: %v", err)
			}
		})
	}
}
