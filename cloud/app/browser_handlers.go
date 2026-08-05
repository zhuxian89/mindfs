package app

import (
	"net/http"
	"net/url"
	"strings"
)

func (a *App) handleBrowserRoot(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	http.Redirect(w, r, "/nodes", http.StatusSeeOther)
}

func safeNodeRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") {
		return ""
	}
	target, err := url.ParseRequestURI(raw)
	if err != nil || target.IsAbs() || target.Host != "" || strings.Contains(target.Path, "\\") || !strings.HasPrefix(target.Path, "/n/") {
		return ""
	}
	remainder := strings.TrimPrefix(target.Path, "/n/")
	nodeID := strings.TrimSpace(strings.SplitN(remainder, "/", 2)[0])
	if nodeID == "" || nodeID == "." || nodeID == ".." {
		return ""
	}
	return target.RequestURI()
}

func safeRelayRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") {
		return ""
	}
	target, err := url.ParseRequestURI(raw)
	if err != nil || target.IsAbs() || target.Host != "" || strings.Contains(target.Path, "\\") {
		return ""
	}
	if target.Path == "/nodes" {
		return target.RequestURI()
	}
	if target.Path == "/bind" {
		return target.RequestURI()
	}
	return safeNodeRedirect(raw)
}

func setBrowserNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
