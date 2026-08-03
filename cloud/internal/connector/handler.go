package connector

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"mindfs-cloud/internal/store"
)

type DeviceAuthenticator interface {
	Authenticate(context.Context, string) (store.Node, error)
}

type Handler struct {
	auth              DeviceAuthenticator
	registry          SessionRegistry
	streamOpenTimeout time.Duration
	upgrader          websocket.Upgrader
}

func NewHandler(auth DeviceAuthenticator, registry SessionRegistry, openTimeout ...time.Duration) *Handler {
	handler := &Handler{
		auth:     auth,
		registry: registry,
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
	if len(openTimeout) > 0 {
		handler.streamOpenTimeout = openTimeout[0]
	}
	return handler
}

func (h *Handler) Authenticate(r *http.Request) (store.Node, error) {
	const prefix = "Bearer "
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, prefix) {
		return store.Node{}, store.ErrNotFound
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	if token == "" {
		return store.Node{}, store.ErrNotFound
	}
	return h.auth.Authenticate(r.Context(), token)
}

func (h *Handler) ServeAuthenticated(w http.ResponseWriter, r *http.Request, node store.Node) {
	responseHeaders := http.Header{}
	responseHeaders.Set("X-MindFS-Relay-Node-Name", node.Name)
	ws, err := h.upgrader.Upgrade(w, r, responseHeaders)
	if err != nil {
		return
	}
	transport := NewWebSocketNetConn(ws)
	config := yamux.DefaultConfig()
	config.ConnectionWriteTimeout = 60 * time.Second
	config.EnableKeepAlive = true
	config.KeepAliveInterval = 30 * time.Second
	if h.streamOpenTimeout > 0 {
		config.StreamOpenTimeout = h.streamOpenTimeout
	}
	session, err := yamux.Server(transport, config)
	if err != nil {
		_ = transport.Close()
		return
	}
	connectionID, err := newConnectionID()
	if err != nil {
		_ = session.Close()
		_ = transport.Close()
		return
	}
	if replaced := h.registry.Register(node.ID, connectionID, session); replaced != nil {
		_ = replaced.Close()
	}
	log.Printf("connector online node_id=%s connection_id=%s", node.ID, connectionID)
	defer func() {
		h.registry.Unregister(node.ID, connectionID)
		_ = session.Close()
		_ = transport.Close()
		log.Printf("connector offline node_id=%s connection_id=%s", node.ID, connectionID)
	}()

	select {
	case <-r.Context().Done():
	case <-session.CloseChan():
	}
}

func newConnectionID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "cn_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
