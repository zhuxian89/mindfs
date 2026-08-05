package app

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"mindfs-cloud/internal/identity"
	"mindfs-cloud/internal/store"
)

type RelayNodePayload struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	BaseURL    string     `json:"base_url"`
	createdAt  time.Time
}

func (a *App) handleRelayNodesList(w http.ResponseWriter, r *http.Request) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		} else {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		}
		return
	}
	nodes, err := a.store.ListNodesByOwner(r.Context(), user.ID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	payload := make([]RelayNodePayload, 0, len(nodes))
	for _, node := range nodes {
		payload = append(payload, a.relayNodePayload(node))
	}
	sort.Slice(payload, func(i, j int) bool {
		left, right := payload[i], payload[j]
		if left.Status != right.Status {
			return left.Status == "online"
		}
		if compared := compareOptionalTimesDesc(left.LastSeenAt, right.LastSeenAt); compared != 0 {
			return compared < 0
		}
		if !left.createdAt.Equal(right.createdAt) {
			return left.createdAt.After(right.createdAt)
		}
		return left.ID < right.ID
	})
	respondJSON(w, http.StatusOK, payload)
}

func (a *App) handleRelayNodeRename(w http.ResponseWriter, r *http.Request) {
	user, ok := a.authorizeRelayWrite(w, r)
	if !ok {
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	node, err := a.store.RenameNodeByOwner(r.Context(), user.ID, strings.TrimSpace(r.PathValue("id")), name)
	if errors.Is(err, store.ErrNotFound) {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
		return
	}
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	respondJSON(w, http.StatusOK, a.relayNodePayload(node))
}

func (a *App) handleRelayNodeDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := a.authorizeRelayWrite(w, r)
	if !ok {
		return
	}
	nodeID := strings.TrimSpace(r.PathValue("id"))
	if err := a.store.DeleteNodeByOwner(r.Context(), user.ID, nodeID); errors.Is(err, store.ErrNotFound) {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "node_not_found"})
		return
	} else if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		return
	}
	if err := a.registry.Disconnect(nodeID); err != nil {
		log.Printf("deleted node session close failed: %v", err)
	}
	respondJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (a *App) authorizeRelayWrite(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	user, _, err := a.authenticateUser(r)
	if err != nil {
		if errors.Is(err, identity.ErrAuthRequired) {
			respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		} else {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "request_failed"})
		}
		return store.User{}, false
	}
	return user, a.requireSameOrigin(w, r)
}

func (a *App) relayNodePayload(node store.Node) RelayNodePayload {
	status := "offline"
	if a.registry.Status(node.ID).Online {
		status = "online"
	}
	return RelayNodePayload{
		ID:         node.ID,
		Name:       node.Name,
		Status:     status,
		LastSeenAt: node.LastSeenAt,
		BaseURL:    a.publicNodeURL(node.ID),
		createdAt:  node.CreatedAt,
	}
}

func (a *App) publicNodeURL(nodeID string) string {
	target := *a.config.PublicURL
	target.Path = "/n/" + url.PathEscape(strings.TrimSpace(nodeID)) + "/"
	target.RawPath = ""
	target.RawQuery = ""
	target.Fragment = ""
	return target.String()
}

func compareOptionalTimesDesc(left, right *time.Time) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return 1
	case right == nil:
		return -1
	case left.Equal(*right):
		return 0
	case left.After(*right):
		return -1
	default:
		return 1
	}
}
