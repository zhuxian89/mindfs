package gateway

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

const (
	wsFrameData          byte = 1
	wsFrameClose         byte = 2
	wsBridgeWriteTimeout      = 15 * time.Second
)

var (
	errWSProtocol = errors.New("invalid websocket stream frame")
	errWSTooLarge = errors.New("websocket message too large")
)

type bridgeResult struct {
	err       error
	closeCode int
	reason    string
}

func (h *Handler) ServeWebSocket(w http.ResponseWriter, r *http.Request, maxMessageBytes int64) error {
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

	outbound := cloneRequest(r, path, h.forwardedProto, true)
	if err := outbound.Write(stream); err != nil {
		return &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "WebSocket request forwarding failed"}
	}
	if h.headerTimeout > 0 {
		_ = stream.SetReadDeadline(time.Now().Add(h.headerTimeout))
	}
	reader := bufio.NewReader(stream)
	response, err := http.ReadResponse(reader, outbound)
	if err != nil {
		if isTimeout(err) {
			return &Error{Status: http.StatusGatewayTimeout, Code: "node_timeout", Message: "node response timed out"}
		}
		return &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "node WebSocket response failed"}
	}
	_ = stream.SetReadDeadline(time.Time{})
	sanitizeNodeResponseHeaders(response.Header, nodeID)
	if response.StatusCode != http.StatusSwitchingProtocols {
		defer response.Body.Close()
		removeHopHeaders(response.Header)
		writeHTTPResponse(w, r, response)
		return nil
	}

	selectedProtocol := strings.TrimSpace(response.Header.Get("Sec-WebSocket-Protocol"))
	if selectedProtocol != "" && !headerContainsToken(r.Header, "Sec-WebSocket-Protocol", selectedProtocol) {
		return &Error{Status: http.StatusBadGateway, Code: "relay_stream_failed", Message: "node selected an invalid WebSocket protocol"}
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	if selectedProtocol != "" {
		upgrader.Subprotocols = []string{selectedProtocol}
	}
	responseHeaders := response.Header.Clone()
	removeHopHeaders(responseHeaders)
	responseHeaders.Del("Sec-WebSocket-Protocol")
	responseHeaders.Del("Sec-WebSocket-Accept")
	responseHeaders.Del("Sec-WebSocket-Extensions")
	publicWS, err := upgrader.Upgrade(w, r, responseHeaders)
	if err != nil {
		return nil
	}
	defer publicWS.Close()
	publicWS.SetReadLimit(maxMessageBytes)

	results := make(chan bridgeResult, 2)
	go bridgePublicToStream(publicWS, stream, maxMessageBytes, results)
	go bridgeStreamToPublic(reader, publicWS, maxMessageBytes, results)
	result := <-results
	_ = stream.Close()
	if result.closeCode != 0 {
		_ = writePublicClose(publicWS, result.closeCode, result.reason)
	}
	if result.err != nil {
		log.Printf("websocket bridge closed node_id=%s error=%v", nodeID, result.err)
	}
	_ = publicWS.Close()
	<-results
	return nil
}

func bridgePublicToStream(publicWS *websocket.Conn, stream io.Writer, maxMessageBytes int64, results chan<- bridgeResult) {
	for {
		opcode, payload, err := publicWS.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				_ = writeWSCloseFrame(stream, sanitizeCloseCode(closeErr.Code), closeErr.Text)
				results <- bridgeResult{}
				return
			}
			if strings.Contains(err.Error(), "read limit") {
				_ = writeWSCloseFrame(stream, websocket.CloseMessageTooBig, "message_too_big")
				results <- bridgeResult{err: errWSTooLarge}
				return
			}
			results <- bridgeResult{err: err}
			return
		}
		if opcode != websocket.TextMessage && opcode != websocket.BinaryMessage {
			results <- bridgeResult{err: errWSProtocol, closeCode: websocket.CloseProtocolError, reason: "invalid_opcode"}
			return
		}
		if int64(len(payload)) > maxMessageBytes {
			_ = writeWSCloseFrame(stream, websocket.CloseMessageTooBig, "message_too_big")
			results <- bridgeResult{err: errWSTooLarge, closeCode: websocket.CloseMessageTooBig, reason: "message_too_big"}
			return
		}
		if err := writeWSDataFrame(stream, opcode, payload); err != nil {
			results <- bridgeResult{err: err}
			return
		}
	}
}

func bridgeStreamToPublic(stream io.Reader, publicWS *websocket.Conn, maxMessageBytes int64, results chan<- bridgeResult) {
	for {
		frameType, opcode, payload, closeCode, reason, err := readWSFrame(stream, maxMessageBytes)
		if err != nil {
			result := bridgeResult{err: err}
			if errors.Is(err, errWSTooLarge) {
				result.closeCode = websocket.CloseMessageTooBig
				result.reason = "message_too_big"
			} else if errors.Is(err, errWSProtocol) {
				result.closeCode = websocket.CloseProtocolError
				result.reason = "invalid_frame"
			}
			results <- result
			return
		}
		switch frameType {
		case wsFrameData:
			if opcode != websocket.TextMessage && opcode != websocket.BinaryMessage {
				results <- bridgeResult{err: errWSProtocol, closeCode: websocket.CloseProtocolError, reason: "invalid_opcode"}
				return
			}
			if err := writePublicMessage(publicWS, opcode, payload); err != nil {
				results <- bridgeResult{err: err}
				return
			}
		case wsFrameClose:
			if !validCloseCode(closeCode) {
				results <- bridgeResult{err: errWSProtocol, closeCode: websocket.CloseProtocolError, reason: "invalid_close_code"}
				return
			}
			results <- bridgeResult{closeCode: closeCode, reason: reason}
			return
		default:
			results <- bridgeResult{err: errWSProtocol, closeCode: websocket.CloseProtocolError, reason: "invalid_frame"}
			return
		}
	}
}

func writePublicClose(conn *websocket.Conn, code int, reason string) error {
	payload := websocket.FormatCloseMessage(sanitizeCloseCode(code), truncateCloseReason(reason))
	return conn.WriteControl(websocket.CloseMessage, payload, time.Now().Add(2*time.Second))
}

func truncateCloseReason(reason string) string {
	bytes := []byte(reason)
	if len(bytes) <= 123 {
		return reason
	}
	bytes = bytes[:123]
	for !utf8.Valid(bytes) {
		bytes = bytes[:len(bytes)-1]
	}
	return string(bytes)
}

func validCloseCode(code int) bool {
	if code >= 3000 && code <= 4999 {
		return true
	}
	return code >= 1000 && code <= 1014 && code != 1004 && code != 1005 && code != 1006
}

func sanitizeCloseCode(code int) int {
	if !validCloseCode(code) {
		return websocket.CloseNormalClosure
	}
	return code
}

func setWriteDeadline(w io.Writer, timeout time.Duration) error {
	deadlineWriter, ok := w.(interface{ SetWriteDeadline(time.Time) error })
	if !ok {
		return nil
	}
	return deadlineWriter.SetWriteDeadline(time.Now().Add(timeout))
}

func headerContainsToken(header http.Header, name, target string) bool {
	for _, value := range header.Values(name) {
		for _, token := range strings.Split(value, ",") {
			if strings.TrimSpace(token) == target {
				return true
			}
		}
	}
	return false
}
