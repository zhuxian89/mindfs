package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"mindfs-cloud/internal/binding"
	"mindfs-cloud/internal/identity"
	"mindfs-cloud/internal/store"
)

const userSessionCookie = identity.SessionCookieName

func (a *App) handleBindPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := bindPage.Execute(w, nil); err != nil {
		return
	}
}

func (a *App) handleBindPoll(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.URL.Query().Get("purpose")) != "" {
		respondError(w, r, http.StatusBadRequest, "invalid_request", "binding purpose is not supported")
		return
	}
	if err := a.identity.LimitBindingPoll(r.Context(), a.requestSource(r)); err != nil {
		if errors.Is(err, store.ErrRateLimited) {
			w.Header().Set("Retry-After", "60")
			respondError(w, r, http.StatusTooManyRequests, "bind_rate_limited", "binding requests are too frequent")
		} else {
			respondError(w, r, http.StatusInternalServerError, "internal_error", "binding rate limit failed")
		}
		return
	}
	response, err := a.binding.Poll(r.Context(), r.URL.Query().Get("code"), r.Header.Get("X-MindFS-Device-ID"))
	if errors.Is(err, store.ErrRateLimited) {
		w.Header().Set("Retry-After", "60")
		respondError(w, r, http.StatusTooManyRequests, "bind_rate_limited", "binding capacity reached; retry later")
		return
	}
	if errors.Is(err, binding.ErrInvalidCode) {
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code or device ID")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding poll failed")
		return
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleBindStatus(w http.ResponseWriter, r *http.Request) {
	if _, _, err := a.authenticateUser(r); err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondError(w, r, http.StatusUnauthorized, "auth_required", "authentication required")
		} else {
			respondError(w, r, http.StatusInternalServerError, "internal_error", "authentication failed")
		}
		return
	}
	response, err := a.binding.Status(r.Context(), r.URL.Query().Get("code"))
	if errors.Is(err, binding.ErrInvalidCode) {
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding status failed")
		return
	}
	if response.NodeName == "" {
		response.NodeName = strings.TrimSpace(r.URL.Query().Get("node_name"))
	}
	respondJSON(w, http.StatusOK, response)
}

func (a *App) handleBindConfirm(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondError(w, r, http.StatusUnauthorized, "auth_required", "authentication required")
		} else {
			respondError(w, r, http.StatusInternalServerError, "internal_error", "authentication failed")
		}
		return
	}
	if !a.requireSameOrigin(w, r) {
		return
	}
	var input struct {
		Code     string `json:"code"`
		Action   string `json:"action"`
		NodeName string `json:"node_name"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return
	}
	action := strings.TrimSpace(input.Action)
	if action == "" {
		action = "confirm"
	}
	nodeName := input.Name
	if strings.TrimSpace(nodeName) == "" {
		nodeName = input.NodeName
	}
	switch action {
	case "confirm":
		response, err := a.binding.Confirm(r.Context(), user.ID, input.Code, nodeName)
		if handleBindingDecisionError(w, r, err) {
			return
		}
		respondJSON(w, http.StatusOK, response)
	case "reject":
		if handleBindingDecisionError(w, r, a.binding.Revoke(r.Context(), input.Code)) {
			return
		}
		respondJSON(w, http.StatusOK, map[string]string{"status": string(store.BindRevoked)})
	default:
		respondError(w, r, http.StatusBadRequest, "invalid_request", "action must be confirm or reject")
	}
}

func handleBindingDecisionError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, binding.ErrInvalidCode):
		respondError(w, r, http.StatusBadRequest, "invalid_bind_code", "invalid binding code")
	case errors.Is(err, binding.ErrClaimed):
		respondError(w, r, http.StatusConflict, "bind_claimed", "binding code already claimed")
	case errors.Is(err, binding.ErrExpired):
		respondError(w, r, http.StatusConflict, "bind_expired", "binding code expired")
	case errors.Is(err, binding.ErrRevoked):
		respondError(w, r, http.StatusConflict, "bind_revoked", "binding code revoked")
	default:
		respondError(w, r, http.StatusInternalServerError, "internal_error", "binding operation failed")
	}
	return true
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values")
	}
	return nil
}
