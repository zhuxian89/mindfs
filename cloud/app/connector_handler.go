package app

import (
	"errors"
	"net/http"

	"mindfs-cloud/internal/store"
)

func (a *App) handleConnector(w http.ResponseWriter, r *http.Request) {
	node, err := a.connector.Authenticate(r)
	if errors.Is(err, store.ErrNotFound) {
		respondError(w, r, http.StatusUnauthorized, "device_token_invalid", "device token is invalid")
		return
	}
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "internal_error", "device token authentication failed")
		return
	}
	a.connector.ServeAuthenticated(w, r, node)
}
