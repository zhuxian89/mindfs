package app

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Forwarded headers are meaningful only across explicitly trusted proxy hops.
// CF-Connecting-IP is deliberately ignored: the edge must sanitize its input
// and produce X-Forwarded-For before forwarding to this service.
func (a *App) requestSource(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	peer = peer.Unmap()
	if !a.trustedProxy(peer) {
		return peer.String()
	}
	forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if forwarded == "" || strings.Count(forwarded, ",") >= 32 {
		return peer.String()
	}
	hops := strings.Split(forwarded, ",")
	current := peer
	for i := len(hops) - 1; i >= 0 && a.trustedProxy(current); i-- {
		address, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil || address.Zone() != "" {
			return peer.String()
		}
		current = address.Unmap()
	}
	return current.String()
}

func (a *App) trustedProxy(address netip.Addr) bool {
	for _, prefix := range a.config.TrustedProxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
