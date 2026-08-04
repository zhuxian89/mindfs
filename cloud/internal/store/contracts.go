package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrExpired  = errors.New("expired")
	ErrClaimed  = errors.New("claimed")
)

type BindStatus string

const (
	BindPending   BindStatus = "pending"
	BindConfirmed BindStatus = "confirmed"
	BindClaimed   BindStatus = "claimed"
	BindExpired   BindStatus = "expired"
	BindRevoked   BindStatus = "revoked"
)

type BindChallenge struct {
	CodeHash               []byte
	DeviceID               string
	RequestedNodeName      string
	RootHint               string
	Status                 BindStatus
	NodeID                 string
	TokenDerivationVersion int
	ExpiresAt              time.Time
	CreatedAt              time.Time
	ConfirmedAt            *time.Time
}

type Node struct {
	ID         string
	DeviceID   string
	Name       string
	Status     string
	AccessMode string
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

type DeviceTokenRecord struct {
	ID         string
	NodeID     string
	TokenHash  []byte
	Status     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type AdminSession struct {
	SessionHash []byte
	CSRFHash    []byte
	ExpiresAt   time.Time
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

type Store interface {
	Close() error
	Ping(context.Context) error
	ObserveChallenge(context.Context, []byte, string, time.Time, time.Time) (BindChallenge, bool, error)
	GetChallenge(context.Context, []byte) (BindChallenge, error)
	ConfirmChallenge(context.Context, []byte, Node, DeviceTokenRecord, time.Time) (BindChallenge, Node, error)
	RevokeChallenge(context.Context, []byte) error
	GetNode(context.Context, string) (Node, error)
	ListNodes(context.Context) ([]Node, error)
	RenameNode(context.Context, string, string) (Node, error)
	DeleteNode(context.Context, string) error
	AuthenticateDeviceToken(context.Context, []byte, time.Time) (Node, error)
	SaveAdminSession(context.Context, AdminSession) error
	GetAdminSession(context.Context, []byte, time.Time) (AdminSession, error)
	DeleteExpired(context.Context, time.Time) error
}
