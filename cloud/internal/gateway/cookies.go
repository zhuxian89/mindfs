package gateway

import (
	"net/http"
	"net/url"
	"strings"

	"mindfs-cloud/internal/identity"
)

func isControlCookie(name string) bool {
	return name == identity.SessionCookieName || name == "__Host-"+identity.SessionCookieName
}

func removeControlCookies(header http.Header) {
	var kept []string
	for _, value := range header.Values("Cookie") {
		for _, part := range strings.Split(value, ";") {
			part = strings.TrimSpace(part)
			name, _, _ := strings.Cut(part, "=")
			if part != "" && !isControlCookie(strings.TrimSpace(name)) {
				kept = append(kept, part)
			}
		}
	}
	header.Del("Cookie")
	if len(kept) > 0 {
		header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func sanitizeNodeResponseHeaders(header http.Header, nodeID string) {
	values := header.Values("Set-Cookie")
	header.Del("Set-Cookie")
	prefix := "/n/" + url.PathEscape(nodeID)
	for _, value := range values {
		cookie, err := http.ParseSetCookie(value)
		if err != nil || isControlCookie(cookie.Name) {
			continue
		}
		cookie.Domain = ""
		if cookie.Path == "" || !strings.HasPrefix(cookie.Path, "/") {
			cookie.Path = "/"
		}
		cookie.Path = prefix + cookie.Path
		header.Add("Set-Cookie", cookie.String())
	}
	// A node cannot clear the whole Relay origin or widen its service-worker
	// scope into the control plane through response headers.
	header.Del("Clear-Site-Data")
	header.Del("Service-Worker-Allowed")
}
