package compat_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type cipherEnvelope struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type e2eeSession struct {
	ClientID string
	NodeID   string
	Key      []byte
}

func (s *e2eeSession) close() {
	for i := range s.Key {
		s.Key[i] = 0
	}
	s.Key = nil
}

func (r *compatibilityRun) openE2EESession() (*e2eeSession, error) {
	nodeID, err := r.fetchE2EENodeID()
	if err != nil {
		return nil, err
	}
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	clientPublic := base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	clientNonce, err := randomBase64(16)
	if err != nil {
		return nil, err
	}
	clientID := "compat-e2ee-client"
	openRequest := map[string]string{
		"client_id":     clientID,
		"node_id":       nodeID,
		"client_eph_pk": clientPublic,
		"client_nonce":  clientNonce,
		"proof":         buildOpenProof(r.bind.PairingSecret, nodeID, clientPublic, clientNonce),
	}
	var response struct {
		OK          bool   `json:"ok"`
		NodeEphPK   string `json:"node_eph_pk"`
		ServerNonce string `json:"server_nonce"`
		ServerProof string `json:"server_proof"`
	}
	if err := r.doJSON(http.MethodPost, r.publicNodeURL("/api/e2ee/open"), openRequest, nil, &response); err != nil {
		return nil, fmt.Errorf("e2ee open: %w", err)
	}
	if !response.OK {
		return nil, errors.New("e2ee open response was not ok")
	}
	expectedProof := buildAcceptProof(r.bind.PairingSecret, nodeID, clientPublic, response.NodeEphPK, clientNonce, response.ServerNonce)
	if !hmac.Equal([]byte(expectedProof), []byte(response.ServerProof)) {
		return nil, errors.New("e2ee server proof mismatch")
	}
	nodePublicBytes, err := base64.StdEncoding.DecodeString(response.NodeEphPK)
	if err != nil {
		return nil, errors.New("invalid node ephemeral public key")
	}
	nodePublic, err := ecdh.P256().NewPublicKey(nodePublicBytes)
	if err != nil {
		return nil, errors.New("invalid node ephemeral public key")
	}
	key, err := deriveTransportKey(r.bind.PairingSecret, nodeID, clientPublic, response.NodeEphPK, clientNonce, response.ServerNonce, privateKey, nodePublic)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("transport key length = %d", len(key))
	}
	return &e2eeSession{ClientID: clientID, NodeID: nodeID, Key: key}, nil
}

func (r *compatibilityRun) fetchE2EENodeID() (string, error) {
	response, body, err := r.request(http.MethodGet, r.publicNodeURL("/api/relay/status"), nil, nil)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("relay status returned %s", response.Status)
	}
	var status struct {
		Required bool   `json:"e2ee_required"`
		NodeID   string `json:"e2ee_node_id"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return "", err
	}
	if !status.Required || strings.TrimSpace(status.NodeID) == "" {
		return "", errors.New("relay status did not expose the E2EE node ID")
	}
	return status.NodeID, nil
}

func (r *compatibilityRun) verifyProtectedHTTP(session *e2eeSession) error {
	extraDir := filepath.Join(r.runDir, "extra-root")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		return err
	}
	canonicalPath := "/api/dirs?compat=method-query-body"
	payload, err := json.Marshal(map[string]any{"path": extraDir, "create": false})
	if err != nil {
		return err
	}
	envelope, err := encryptBytes(session.Key, payload)
	if err != nil {
		return err
	}
	encryptedBody, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	headers := map[string]string{
		"Content-Type":       "application/json",
		"X-MindFS-E2EE":      "1",
		"X-MindFS-Client-ID": session.ClientID,
		"X-MindFS-TS":        timestamp,
		"X-MindFS-Proof":     buildRequestProof(session.Key, http.MethodPost, canonicalPath, timestamp, session.ClientID),
	}
	response, responseBody, err := r.request(http.MethodPost, r.publicNodeURL(canonicalPath), encryptedBody, headers)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("X-MindFS-E2EE") != "1" {
		return fmt.Errorf("protected HTTP returned %s", response.Status)
	}
	var responseEnvelope cipherEnvelope
	if err := json.Unmarshal(responseBody, &responseEnvelope); err != nil {
		return errors.New("protected HTTP response was not an envelope")
	}
	plaintext, err := decryptBytes(session.Key, &responseEnvelope)
	if err != nil {
		return fmt.Errorf("decrypt protected HTTP response: %w", err)
	}
	var added struct {
		RootPath string `json:"root_path"`
	}
	if err := json.Unmarshal(plaintext, &added); err != nil {
		return err
	}
	if added.RootPath != extraDir {
		return fmt.Errorf("protected POST body mismatch")
	}
	return nil
}

func (r *compatibilityRun) verifyEncryptedWebSocket(session *e2eeSession) error {
	values := url.Values{
		"client_id": []string{session.ClientID},
		"compat":    []string{"ws"},
	}
	canonicalPath := "/ws?" + values.Encode()
	timestamp := time.Now().UTC().Format(time.RFC3339)
	values.Set("e2ee_ts", timestamp)
	values.Set("e2ee_proof", buildRequestProof(session.Key, http.MethodGet, canonicalPath, timestamp, session.ClientID))
	endpoint := r.publicNodeURL("/ws") + "?" + values.Encode()
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	parsed.Scheme = "ws"
	dialer := websocket.Dialer{HandshakeTimeout: stageTimeout}
	conn, response, err := dialer.DialContext(r.ctx, parsed.String(), nil)
	if err != nil {
		if response != nil {
			return fmt.Errorf("WebSocket handshake returned %s", response.Status)
		}
		return errors.New("WebSocket handshake failed")
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(stageTimeout))
	_ = conn.SetWriteDeadline(time.Now().Add(stageTimeout))
	requestID := "compat-ping"
	payload, err := json.Marshal(map[string]any{"id": requestID, "type": "ping", "payload": map[string]any{}})
	if err != nil {
		return err
	}
	envelope, err := encryptBytes(session.Key, payload)
	if err != nil {
		return err
	}
	if err := conn.WriteJSON(envelope); err != nil {
		return err
	}
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var encrypted cipherEnvelope
		if err := json.Unmarshal(message, &encrypted); err != nil {
			return errors.New("WebSocket response exposed plaintext")
		}
		plaintext, err := decryptBytes(session.Key, &encrypted)
		if err != nil {
			return fmt.Errorf("decrypt WebSocket response: %w", err)
		}
		var result struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(plaintext, &result); err != nil {
			return err
		}
		if result.ID == requestID {
			if result.Type != "pong" {
				return fmt.Errorf("WebSocket response type = %q", result.Type)
			}
			return nil
		}
	}
}

func buildOpenProof(secret, nodeID, clientPublic, clientNonce string) string {
	return buildHMACProof(secret, "mindfs-e2ee-open", nodeID, clientPublic, clientNonce)
}

func buildAcceptProof(secret, nodeID, clientPublic, nodePublic, clientNonce, serverNonce string) string {
	return buildHMACProof(secret, "mindfs-e2ee-accept", nodeID, clientPublic, nodePublic, clientNonce, serverNonce)
}

func buildHMACProof(secret, label string, parts ...string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	digest := sha256.Sum256([]byte(joinProofParts(label, parts...)))
	_, _ = mac.Write(digest[:])
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func buildRequestProof(key []byte, method, path, timestamp, clientID string) string {
	mac := hmac.New(sha256.New, key)
	digest := sha256.Sum256([]byte(joinProofParts("mindfs-request-proof", method, path, timestamp, clientID)))
	_, _ = mac.Write(digest[:])
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func deriveTransportKey(secret, nodeID, clientPublic, nodePublic, clientNonce, serverNonce string, privateKey *ecdh.PrivateKey, publicKey *ecdh.PublicKey) ([]byte, error) {
	sharedSecret, err := privateKey.ECDH(publicKey)
	if err != nil {
		return nil, err
	}
	infoHash := sha256.Sum256([]byte(joinProofParts("", nodeID, clientPublic, nodePublic, clientNonce, serverNonce)))
	salt := sha256.Sum256([]byte(secret))
	sessionMaster, err := hkdf.Key(sha256.New, sharedSecret, salt[:], string(infoHash[:]), 32)
	if err != nil {
		return nil, err
	}
	return hkdf.Key(sha256.New, sessionMaster, nil, "transport", 32)
}

func joinProofParts(label string, parts ...string) string {
	values := make([]string, 0, len(parts)+1)
	if label != "" {
		values = append(values, label)
	}
	values = append(values, parts...)
	return strings.Join(values, "\x1f")
}

func randomBase64(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(value), nil
}

func encryptBytes(key, plaintext []byte) (*cipherEnvelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	return &cipherEnvelope{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptBytes(key []byte, envelope *cipherEnvelope) ([]byte, error) {
	if envelope == nil {
		return nil, errors.New("cipher envelope required")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}
