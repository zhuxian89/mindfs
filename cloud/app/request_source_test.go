package app

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRequestSourceUsesOnlyTrustedProxyChain(t *testing.T) {
	a := &App{}
	a.config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8:1::/48")}
	for _, test := range []struct{ name, peer, xff, want string }{
		{"untrusted", "192.0.2.1:123", "198.51.100.1", "192.0.2.1"},
		{"trusted", "10.0.0.1:123", "198.51.100.1", "198.51.100.1"},
		{"spoofed left hop", "10.0.0.1:123", "198.51.100.1, 192.0.2.1", "192.0.2.1"},
		{"multiple proxies", "10.0.0.1:123", "192.0.2.1, 10.0.0.2", "192.0.2.1"},
		{"malformed", "10.0.0.1:123", "not-an-ip", "10.0.0.1"},
		{"missing", "10.0.0.1:123", "", "10.0.0.1"},
		{"IPv6", "[2001:db8:1::1]:123", "2001:db8:2::2", "2001:db8:2::2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/auth/login", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("X-Forwarded-For", test.xff)
			r.Header.Set("CF-Connecting-IP", "203.0.113.1")
			if got := a.requestSource(r); got != test.want {
				t.Fatalf("source=%q want=%q", got, test.want)
			}
		})
	}
}
