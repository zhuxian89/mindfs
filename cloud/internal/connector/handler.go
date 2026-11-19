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
	yamux "github.com/libp2p/go-yamux/v5"

	"mindfs-cloud/internal/ops"
	"mindfs-cloud/internal/store"
)

type sessionLog struct {
	nodeID, connectionID string
}

func (l sessionLog) Write(p []byte) (int, error) {
	log.Printf("yamux node_id=%s connection_id=%s detail=%q", l.nodeID, l.connectionID, strings.TrimSpace(string(p)))
	return len(p), nil
}

type DeviceAuthenticator interface {
	Authenticate(context.Context, string) (store.Node, error)
}

type Handler struct {
	auth     DeviceAuthenticator
	registry SessionRegistry
	upgrader websocket.Upgrader
}

func NewHandler(auth DeviceAuthenticator, registry SessionRegistry) *Handler {
	return &Handler{
		auth:     auth,
		registry: registry,
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
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
	ops.WebSocketUpgraded(w)
	connectionID, err := newConnectionID()
	if err != nil {
		_ = ws.Close()
		return
	}
	transport := &sessionTransport{Conn: NewWebSocketNetConn(ws)}
	config := yamux.DefaultConfig()
	config.LogOutput = sessionLog{node.ID, connectionID}
	config.InitialStreamWindowSize = 4 << 20
	config.MaxStreamWindowSize = config.InitialStreamWindowSize
	config.ConnectionWriteTimeout = 60 * time.Second
	session, err := yamux.Server(transport, config, nil)
	if err != nil {
		_ = transport.Close()
		return
	}
	started := time.Now()
	if replaced := h.registry.Register(node.ID, connectionID, session); replaced != nil {
		_ = replaced.Close()
	}
	log.Printf("connector online node_id=%s connection_id=%s", node.ID, connectionID)
	reason := "session_closed"
	defer func() {
		current := h.registry.Status(node.ID)
		switch {
		case !current.Online:
			reason = "relay_disconnect"
		case current.ConnectionID != connectionID:
			reason = "replaced"
		}
		h.registry.Unregister(node.ID, connectionID)
		_ = session.Close()
		_ = transport.Close()
		direction, code := transport.failure()
		log.Printf("connector offline node_id=%s connection_id=%s reason=%s transport=%s ws_code=%d lifetime_ms=%d", node.ID, connectionID, reason, direction, code, time.Since(started).Milliseconds())
	}()

	select {
	case <-r.Context().Done():
		reason = "request_canceled"
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
