package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mindfs-cloud/internal/identity"
	"mindfs-cloud/internal/store"
)

func (a *App) handleRegistrationCodeRequest(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	seconds, err := a.identity.RequestRegistrationCode(r.Context(), input.Email, a.requestSource(r))
	if handleIdentityError(w, err) {
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"resend_after_seconds": seconds})
}

func (a *App) handleRegistration(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	user, token, err := a.identity.Register(r.Context(), input.Email, input.Password, input.Code)
	if handleIdentityError(w, err) {
		return
	}
	a.setUserSessionCookie(w, token)
	respondJSON(w, http.StatusOK, userPayload(user))
}

func (a *App) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	user, token, err := a.identity.Login(r.Context(), input.Email, input.Password, a.requestSource(r))
	if handleIdentityError(w, err) {
		return
	}
	a.setUserSessionCookie(w, token)
	respondJSON(w, http.StatusOK, userPayload(user))
}

func (a *App) handlePasswordResetCodeRequest(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	seconds, err := a.identity.RequestPasswordResetCode(r.Context(), input.Email, a.requestSource(r))
	if handleIdentityError(w, err) {
		return
	}
	respondJSON(w, http.StatusOK, map[string]int{"resend_after_seconds": seconds})
}

func (a *App) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	if handleIdentityError(w, a.identity.ResetPassword(r.Context(), input.Email, input.Code, input.NewPassword)) {
		return
	}
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (a *App) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		} else {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		}
		return
	}
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	token, err := a.identity.ChangePassword(r.Context(), user, input.CurrentPassword, input.NewPassword)
	if handleIdentityError(w, err) {
		return
	}
	a.setUserSessionCookie(w, token)
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (a *App) authenticateUser(r *http.Request) (store.User, store.UserSession, error) {
	cookie, err := r.Cookie(userSessionCookie)
	if err != nil {
		return store.User{}, store.UserSession{}, identity.ErrAuthRequired
	}
	return a.identity.Authenticate(r.Context(), cookie.Value)
}

func (a *App) setUserSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.PublicURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(identity.UserSessionTTL.Seconds()),
	})
}

func (a *App) clearUserSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.PublicURL.Scheme == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
}

func (a *App) requireSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin, err := url.Parse(strings.TrimSpace(r.Header.Get("Origin")))
	if err != nil || origin.Scheme != a.config.PublicURL.Scheme || !strings.EqualFold(origin.Host, a.config.PublicURL.Host) {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return false
	}
	return true
}

func userPayload(user store.User) map[string]any {
	return map[string]any{"user": map[string]string{
		"id": user.ID, "email": user.Email, "name": user.Email,
	}}
}

func handleIdentityError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, identity.ErrEmailNotAllowed):
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "email_not_allowed"})
	case errors.Is(err, identity.ErrEmailTaken):
		respondJSON(w, http.StatusConflict, map[string]string{"error": "email_taken"})
	case errors.Is(err, identity.ErrInvalidPassword):
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_password"})
	case errors.Is(err, identity.ErrCodeInvalid):
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "email_code_invalid"})
	case errors.Is(err, identity.ErrRateLimited):
		respondJSON(w, http.StatusTooManyRequests, map[string]string{"error": "email_code_rate_limited"})
	case errors.Is(err, identity.ErrInvalidCredentials):
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
	case errors.Is(err, identity.ErrMailUnavailable):
		respondJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "email_sender_not_configured"})
	default:
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
	}
	return true
}
