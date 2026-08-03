package app

import (
	"errors"
	"net/http"

	"github.com/gorilla/websocket"

	"mindfs-cloud/internal/gateway"
)

func (a *App) handleGateway(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		if err := a.gateway.ServeWebSocket(w, r, a.config.MaxWSMessageBytes); err != nil {
			a.respondGatewayError(w, r, err)
		}
		return
	}
	if err := a.gateway.ServeHTTP(w, r); err != nil {
		a.respondGatewayError(w, r, err)
	}
}

func (a *App) respondGatewayError(w http.ResponseWriter, r *http.Request, err error) {
	var gatewayErr *gateway.Error
	if errors.As(err, &gatewayErr) {
		respondError(w, r, gatewayErr.Status, gatewayErr.Code, gatewayErr.Message)
		return
	}
	respondError(w, r, http.StatusInternalServerError, "internal_error", "gateway request failed")
}
