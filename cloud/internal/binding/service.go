package binding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"mindfs-cloud/internal/store"
)

var (
	ErrInvalidCode = errors.New("invalid bind code")
	ErrClaimed     = errors.New("bind claimed")
	ErrExpired     = errors.New("bind expired")
	ErrRevoked     = errors.New("bind revoked")
)

type BindPollResponse struct {
	Status          string `json:"status"`
	NextPollAfterMS int64  `json:"next_poll_after_ms,omitempty"`
	DeviceToken     string `json:"device_token,omitempty"`
	NodeID          string `json:"node_id,omitempty"`
	NodeName        string `json:"node_name,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
}

type BindPageStatus struct {
	Status     string `json:"status"`
	DeviceSeen bool   `json:"device_seen"`
	NodeName   string `json:"node_name,omitempty"`
}

type BindConfirmation struct {
	Status  string `json:"status"`
	NodeID  string `json:"node_id,omitempty"`
	NodeURL string `json:"node_url,omitempty"`
}

type BindingService interface {
	Poll(context.Context, string, string) (BindPollResponse, error)
	Status(context.Context, string) (BindPageStatus, error)
	Confirm(context.Context, string, string, string) (BindConfirmation, error)
	Revoke(context.Context, string) error
}

type Service struct {
	store      store.Store
	tokens     DeviceTokenService
	bindTTL    time.Duration
	endpoint   string
	publicBase string
	now        func() time.Time
}

func NewService(st store.Store, tokens DeviceTokenService, bindTTL time.Duration, endpoint, publicBase string) *Service {
	return &Service{
		store:      st,
		tokens:     tokens,
		bindTTL:    bindTTL,
		endpoint:   endpoint,
		publicBase: strings.TrimRight(publicBase, "/"),
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Poll(ctx context.Context, code, deviceID string) (BindPollResponse, error) {
	codeHash, err := hashBindCode(code)
	if err != nil || strings.TrimSpace(deviceID) == "" || len(deviceID) > 256 {
		return BindPollResponse{}, ErrInvalidCode
	}
	now := s.now()
	challenge, claimed, err := s.store.ObserveChallenge(ctx, codeHash, strings.TrimSpace(deviceID), now, now.Add(s.bindTTL))
	if err != nil {
		return BindPollResponse{}, err
	}
	if claimed {
		return BindPollResponse{Status: string(store.BindClaimed)}, nil
	}

	switch challenge.Status {
	case store.BindPending:
		return BindPollResponse{Status: string(store.BindPending), NextPollAfterMS: 3000}, nil
	case store.BindConfirmed:
		plain, _, err := s.tokens.Derive(challenge)
		if err != nil {
			return BindPollResponse{}, err
		}
		node, err := s.store.GetNode(ctx, challenge.NodeID)
		if err != nil {
			return BindPollResponse{}, err
		}
		return BindPollResponse{
			Status:      string(store.BindConfirmed),
			DeviceToken: plain,
			NodeID:      node.ID,
			NodeName:    node.Name,
			Endpoint:    s.endpoint,
		}, nil
	case store.BindClaimed:
		return BindPollResponse{Status: string(store.BindClaimed)}, nil
	case store.BindExpired:
		return BindPollResponse{Status: string(store.BindExpired)}, nil
	case store.BindRevoked:
		return BindPollResponse{Status: string(store.BindRevoked)}, nil
	default:
		return BindPollResponse{}, errors.New("unknown bind status")
	}
}

func (s *Service) Status(ctx context.Context, code string) (BindPageStatus, error) {
	codeHash, err := hashBindCode(code)
	if err != nil {
		return BindPageStatus{}, ErrInvalidCode
	}
	challenge, err := s.store.GetChallenge(ctx, codeHash)
	if errors.Is(err, store.ErrNotFound) {
		return BindPageStatus{Status: "waiting_for_device", DeviceSeen: false}, nil
	}
	if err != nil {
		return BindPageStatus{}, err
	}
	if s.now().After(challenge.ExpiresAt) && challenge.Status == store.BindPending {
		return BindPageStatus{Status: string(store.BindExpired), DeviceSeen: true, NodeName: challenge.RequestedNodeName}, nil
	}
	return BindPageStatus{Status: string(challenge.Status), DeviceSeen: true, NodeName: challenge.RequestedNodeName}, nil
}

var _ BindingService = (*Service)(nil)

func (s *Service) Confirm(ctx context.Context, ownerUserID, code, nodeName string) (BindConfirmation, error) {
	codeHash, err := hashBindCode(code)
	if err != nil {
		return BindConfirmation{}, ErrInvalidCode
	}
	challenge, err := s.store.GetChallenge(ctx, codeHash)
	if errors.Is(err, store.ErrNotFound) {
		return BindConfirmation{}, ErrInvalidCode
	}
	if err != nil {
		return BindConfirmation{}, err
	}
	name := strings.TrimSpace(nodeName)
	if name == "" {
		name = strings.TrimSpace(challenge.RequestedNodeName)
	}
	if name == "" {
		name = "MindFS Node"
	}
	now := s.now()
	nodeID, err := newNodeID()
	if err != nil {
		return BindConfirmation{}, err
	}
	node := store.Node{
		ID:          nodeID,
		DeviceID:    challenge.DeviceID,
		OwnerUserID: ownerUserID,
		Name:        name,
		Status:      "active",
		AccessMode:  "node_auth",
		CreatedAt:   now,
	}
	derivationChallenge := challenge
	derivationChallenge.NodeID = node.ID
	_, tokenHash, err := s.tokens.Derive(derivationChallenge)
	if err != nil {
		return BindConfirmation{}, err
	}
	tokenID, err := newOpaqueID("tok_", 18)
	if err != nil {
		return BindConfirmation{}, err
	}
	token := store.DeviceTokenRecord{
		ID:        tokenID,
		NodeID:    node.ID,
		TokenHash: tokenHash,
		Status:    "active",
		CreatedAt: now,
	}
	confirmed, storedNode, err := s.store.ConfirmChallenge(ctx, codeHash, node, token, now)
	if errors.Is(err, store.ErrExpired) {
		return BindConfirmation{}, ErrExpired
	}
	if errors.Is(err, store.ErrClaimed) {
		return BindConfirmation{}, ErrClaimed
	}
	if errors.Is(err, store.ErrConflict) {
		return BindConfirmation{}, ErrRevoked
	}
	if err != nil {
		return BindConfirmation{}, err
	}
	return BindConfirmation{
		Status:  string(confirmed.Status),
		NodeID:  storedNode.ID,
		NodeURL: s.publicBase + "/n/" + storedNode.ID + "/",
	}, nil
}

func (s *Service) Revoke(ctx context.Context, code string) error {
	codeHash, err := hashBindCode(code)
	if err != nil {
		return ErrInvalidCode
	}
	if err := s.store.RevokeChallenge(ctx, codeHash); errors.Is(err, store.ErrConflict) {
		return ErrRevoked
	} else {
		return err
	}
}

func hashBindCode(code string) ([]byte, error) {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "pc_") || len(code) < 8 || len(code) > 128 {
		return nil, ErrInvalidCode
	}
	if _, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "pc_")); err != nil {
		return nil, ErrInvalidCode
	}
	hash := sha256.Sum256([]byte(code))
	return hash[:], nil
}

func newNodeID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "n" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])), nil
}

func newOpaqueID(prefix string, size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
