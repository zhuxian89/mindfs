package gateway

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mindfs-cloud/internal/connector"
	"mindfs-cloud/internal/store"
)

type NodeStore interface {
	GetNode(context.Context, string) (store.Node, error)
}

type StreamRegistry interface {
	OpenStream(context.Context, string) (net.Conn, error)
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code }

type Handler struct {
	store             NodeStore
	registry          StreamRegistry
	streamOpenTimeout time.Duration
	headerTimeout     time.Duration
	forwardedProto    string
}

func NewHandler(st NodeStore, registry StreamRegistry, streamOpenTimeout, headerTimeout time.Duration, forwardedProto string) *Handler {
	return &Handler{
		store:             st,
		registry:          registry,
		streamOpenTimeout: streamOpenTimeout,
		headerTimeout:     headerTimeout,
		forwardedProto:    forwardedProto,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	nodeID, path, err := parseNodeRoute(r.URL.EscapedPath())
	if err != nil {
		return &Error{Status: http.StatusNotFound, Code: "node_not_found", Message: "node does not exist"}
	}
	stream, err := h.openNodeStream(r.Context(), nodeID)
	if err != nil {
		return err
	}
	defer stream.Close()

	done := closeStreamOnCancel(r.Context(), stream)
	defer close(done)

	outbound := cloneRequest(r, path, h.forwardedProto, false)
	if err := outbound.Write(stream); err != nil {
		return &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "request forwarding failed"}
	}
	if h.headerTimeout > 0 {
		_ = stream.SetReadDeadline(time.Now().Add(h.headerTimeout))
	}
	response, err := http.ReadResponse(bufio.NewReader(stream), outbound)
	if err != nil {
		if isTimeout(err) {
			return &Error{Status: http.StatusGatewayTimeout, Code: "node_timeout", Message: "node response timed out"}
		}
		return &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "node response failed"}
	}
	defer response.Body.Close()
	_ = stream.SetReadDeadline(time.Time{})
	removeHopHeaders(response.Header)
	sanitizeNodeResponseHeaders(response.Header, nodeID)
	writeHTTPResponse(w, r, response)
	return nil
}

func writeHTTPResponse(w http.ResponseWriter, r *http.Request, response *http.Response) {
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		// Headers may already be on the wire. A normal return would finalize a
		// truncated body as a successful response; abort the HTTP stream instead.
		log.Printf("relay response interrupted method=%s", r.Method)
		panic(http.ErrAbortHandler)
	}
}

func (h *Handler) openNodeStream(ctx context.Context, nodeID string) (net.Conn, error) {
	node, err := h.store.GetNode(ctx, nodeID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &Error{Status: http.StatusNotFound, Code: "node_not_found", Message: "node does not exist"}
	}
	if err != nil {
		return nil, &Error{Status: http.StatusInternalServerError, Code: "internal_error", Message: "node lookup failed"}
	}
	if node.Status != "active" {
		return nil, &Error{Status: http.StatusForbidden, Code: "node_disabled", Message: "node is disabled"}
	}
	stream, err := h.openStream(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (h *Handler) openStream(parent context.Context, nodeID string) (net.Conn, error) {
	ctx := parent
	cancel := func() {}
	if h.streamOpenTimeout > 0 {
		ctx, cancel = context.WithTimeout(parent, h.streamOpenTimeout)
	}
	defer cancel()
	stream, err := h.registry.OpenStream(ctx, nodeID)
	if errors.Is(err, connector.ErrNodeOffline) {
		return nil, &Error{Status: http.StatusServiceUnavailable, Code: "node_offline", Message: "node is not connected"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, &Error{Status: http.StatusGatewayTimeout, Code: "node_timeout", Message: "node connection timed out"}
	}
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "unable to open relay stream"}
	}
	return stream, nil
}

// parseNodeRoute takes an escaped URL path and returns a decoded node ID and
// an escaped Node path. Decoding before splitting would change signed paths.
func parseNodeRoute(path string) (string, string, error) {
	if !strings.HasPrefix(path, "/n/") {
		return "", "", errors.New("invalid node route")
	}
	remainder := strings.TrimPrefix(path, "/n/")
	parts := strings.SplitN(remainder, "/", 2)
	nodeID, err := url.PathUnescape(parts[0])
	if err != nil || strings.Contains(nodeID, "/") {
		return "", "", errors.New("invalid node ID")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return "", "", errors.New("missing node ID")
	}
	stripped := "/"
	if len(parts) == 2 && parts[1] != "" {
		stripped += parts[1]
	}
	if _, err := url.PathUnescape(stripped); err != nil {
		return "", "", err
	}
	return nodeID, stripped, nil
}

func cloneRequest(r *http.Request, path, forwardedProto string, keepUpgrade bool) *http.Request {
	outbound := r.Clone(r.Context())
	// path is the validated escaped suffix from parseNodeRoute. Keep its exact
	// spelling: Node E2EE proofs distinguish %3A from : (and %3a from %3A).
	outbound.URL.Path, _ = url.PathUnescape(path)
	outbound.URL.RawPath = path
	outbound.RequestURI = ""
	outbound.Header = r.Header.Clone()
	originalHost := r.Host
	removeHopHeaders(outbound.Header)
	removeControlCookies(outbound.Header)
	if keepUpgrade {
		copyUpgradeHeaders(outbound.Header, r.Header)
	}
	outbound.Header.Del("X-MindFS-Relayed")
	outbound.Header.Del("X-MindFS-Relay-Service-Slug")
	outbound.Header.Set("X-MindFS-Relayed", "1")
	outbound.Header.Set("X-Forwarded-Host", originalHost)
	outbound.Header.Set("X-Forwarded-Proto", forwardedProto)
	return outbound
}

func removeHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyUpgradeHeaders(target, source http.Header) {
	for _, name := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Protocol", "Sec-WebSocket-Extensions"} {
		if values := source.Values(name); len(values) > 0 {
			target[name] = append([]string(nil), values...)
		}
	}
}

func copyHeaders(target, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func closeStreamOnCancel(ctx context.Context, stream net.Conn) chan struct{} {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-done:
		}
	}()
	return done
}
