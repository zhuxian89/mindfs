package app

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"mindfs-cloud/internal/connector"
)

func TestEndToEndBindingConnectorHTTPAndWebSocket(t *testing.T) {
	publicURL, _ := url.Parse("http://relay.example.com")
	var tokenKey [32]byte
	copy(tokenKey[:], []byte("01234567890123456789012345678901"))
	application, err := newTestApp(t, testConfig(t, publicURL, tokenKey))
	if err != nil {
		t.Fatal(err)
	}
	defer application.Close()
	server := httptest.NewServer(application.Handler())
	defer server.Close()

	code := "pc_" + base64.RawURLEncoding.EncodeToString([]byte("full-protocol-123456"))
	_ = pollBind(t, server.URL, code, "device-1")
	sessionCookie := sessionCookieHeader(registerTestSession(t, application, "user@qq.com", "relay-password"))
	_ = requestJSON(t, server.URL+"/api/bind/confirm", http.MethodPost, map[string]string{
		"code": code, "action": "confirm", "node_name": "Protocol Node",
	}, map[string]string{"Cookie": sessionCookie, "Origin": application.config.PublicURL.String()}, http.StatusOK)
	credentials := pollBind(t, server.URL, code, "device-1")
	deviceToken := credentials["device_token"].(string)
	nodeID := credentials["node_id"].(string)

	invalidHeaders := http.Header{"Authorization": []string{"Bearer dt_invalid"}}
	_, invalidResponse, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/ws/connector",
		invalidHeaders,
	)
	if err == nil || invalidResponse == nil {
		t.Fatalf("invalid connector dial err=%v response=%#v", err, invalidResponse)
	}
	var invalidBody map[string]any
	if err := json.NewDecoder(invalidResponse.Body).Decode(&invalidBody); err != nil {
		t.Fatal(err)
	}
	invalidResponse.Body.Close()
	if invalidResponse.StatusCode != http.StatusUnauthorized || invalidBody["error"] != "device_token_invalid" {
		t.Fatalf("invalid connector response = %d %#v", invalidResponse.StatusCode, invalidBody)
	}

	headers := http.Header{"Authorization": []string{"Bearer " + deviceToken}}
	connectorWS, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/ws/connector",
		headers,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connectorWS.Close()
	clientSession, err := yamux.Client(connector.NewWebSocketNetConn(connectorWS), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	nodeRequests := make(chan *http.Request, 4)
	nodeErrors := make(chan error, 1)
	go serveProtocolNode(clientSession, nodeRequests, nodeErrors)

	deadline := time.Now().Add(2 * time.Second)
	for !application.registry.Status(nodeID).Online && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !application.registry.Status(nodeID).Online {
		t.Fatal("node did not become online")
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/n/"+nodeID+"/api/echo?q=one", bytes.NewBufferString("payload"))
	request.Header.Set("X-MindFS-E2EE-Proof", "proof-value")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "node-response" {
		t.Fatalf("HTTP response = %d %q", response.StatusCode, body)
	}
	httpSeen := <-nodeRequests
	if httpSeen.URL.Path != "/api/echo" || httpSeen.URL.RawQuery != "q=one" || httpSeen.Header.Get("X-MindFS-E2EE-Proof") != "proof-value" {
		t.Fatalf("node HTTP request = %s %#v", httpSeen.URL.String(), httpSeen.Header)
	}

	publicWS, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(server.URL, "http")+"/n/"+nodeID+"/ws",
		http.Header{"X-MindFS-E2EE-Proof": []string{"ws-proof"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer publicWS.Close()
	if err := publicWS.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := publicWS.ReadMessage()
	if err != nil || opcode != websocket.TextMessage || string(payload) != "hello" {
		t.Fatalf("WebSocket echo = opcode=%d payload=%q err=%v", opcode, payload, err)
	}
	wsSeen := <-nodeRequests
	if wsSeen.URL.Path != "/ws" || wsSeen.Header.Get("X-MindFS-E2EE-Proof") != "ws-proof" {
		t.Fatalf("node WebSocket request = %s %#v", wsSeen.URL.String(), wsSeen.Header)
	}
	select {
	case err := <-nodeErrors:
		if err != nil {
			t.Fatal(err)
		}
	default:
	}
}

func serveProtocolNode(session *yamux.Session, requests chan<- *http.Request, errorsCh chan<- error) {
	for {
		stream, err := session.Accept()
		if err != nil {
			return
		}
		go func() {
			defer stream.Close()
			request, err := http.ReadRequest(bufio.NewReader(stream))
			if err != nil {
				errorsCh <- err
				return
			}
			body, _ := io.ReadAll(request.Body)
			request.Body = io.NopCloser(bytes.NewReader(body))
			requests <- request
			if websocket.IsWebSocketUpgrade(request) {
				if _, err := io.WriteString(stream, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
					errorsCh <- err
					return
				}
				opcode, payload, err := readProtocolDataFrame(stream)
				if err != nil {
					errorsCh <- err
					return
				}
				if err := writeProtocolDataFrame(stream, opcode, payload); err != nil {
					errorsCh <- err
					return
				}
				_ = writeProtocolCloseFrame(stream, websocket.CloseNormalClosure, "done")
				return
			}
			_, err = io.WriteString(stream, "HTTP/1.1 200 OK\r\nContent-Length: 13\r\nContent-Type: text/plain\r\n\r\nnode-response")
			if err != nil {
				errorsCh <- err
			}
		}()
	}
}

func readProtocolDataFrame(r io.Reader) (int, []byte, error) {
	var header [6]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[2:])
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return int(header[1]), payload, nil
}

func writeProtocolDataFrame(w io.Writer, opcode int, payload []byte) error {
	header := make([]byte, 6)
	header[0] = 1
	header[1] = byte(opcode)
	binary.BigEndian.PutUint32(header[2:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeProtocolCloseFrame(w io.Writer, code int, reason string) error {
	header := make([]byte, 7)
	header[0] = 2
	binary.BigEndian.PutUint16(header[1:], uint16(code))
	binary.BigEndian.PutUint32(header[3:], uint32(len(reason)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := io.WriteString(w, reason)
	return err
}
