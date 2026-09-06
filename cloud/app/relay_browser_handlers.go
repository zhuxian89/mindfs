package app

import (
	"errors"
	"net/http"
	"net/url"

	"mindfs-cloud/internal/identity"
)

type browserLoginPageData struct {
	Next string
}

func (a *App) handleBrowserLogin(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	target := safeRelayRedirect(r.URL.Query().Get("next"))
	if target == "" {
		target = "/nodes"
	}
	if _, _, err := a.authenticateUser(r); err == nil {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	} else if !errors.Is(err, identity.ErrAuthRequired) {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = browserLoginPage.Execute(w, browserLoginPageData{Next: target})
}

func (a *App) handleBrowserNodes(w http.ResponseWriter, r *http.Request) {
	setBrowserNoStoreHeaders(w)
	if _, _, err := a.authenticateUser(r); errors.Is(err, identity.ErrAuthRequired) {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	} else if err != nil {
		http.Error(w, "request failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	_ = browserNodesPage.Execute(w, nil)
}

func (a *App) handleBrowserAuthStatus(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"id": user.ID, "email": user.Email, "name": user.Email})
}

func (a *App) handleBrowserLogout(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	if cookie, err := r.Cookie(userSessionCookie); err == nil {
		if err := a.identity.Logout(r.Context(), cookie.Value); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
			return
		}
	}
	a.clearUserSessionCookie(w)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}
