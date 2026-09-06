package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrExpired     = errors.New("expired")
	ErrClaimed     = errors.New("claimed")
	ErrRateLimited = errors.New("rate limited")
	ErrCodeInvalid = errors.New("verification code invalid")
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
	ClaimedByUserID        string
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
	ID          string
	DeviceID    string
	OwnerUserID string
	Name        string
	Status      string
	AccessMode  string
	CreatedAt   time.Time
	LastSeenAt  *time.Time
}

type DeviceTokenRecord struct {
	ID         string
	NodeID     string
	TokenHash  []byte
	Status     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type User struct {
	ID                string
	Email             string
	PasswordHash      string
	Status            string
	CreatedAt         time.Time
	PasswordChangedAt *time.Time
	LastLoginAt       *time.Time
}

type VerificationCode struct {
	Email             string
	Purpose           string
	Nonce             []byte
	CodeHash          []byte
	SourceHash        []byte
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
	AttemptsRemaining int
	CreatedAt         time.Time
	ConsumedAt        *time.Time
}

type UserSession struct {
	SessionHash []byte
	UserID      string
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
	ListNodesByOwner(context.Context, string) ([]Node, error)
	RenameNodeByOwner(context.Context, string, string, string) (Node, error)
	DeleteNodeByOwner(context.Context, string, string) error
	AuthenticateDeviceToken(context.Context, []byte, time.Time) (Node, error)
	TakeRateLimit(context.Context, string, []byte, time.Time, time.Duration, int) error
	SaveVerificationCode(context.Context, VerificationCode, time.Time) error
	GetVerificationCode(context.Context, string, string) (VerificationCode, error)
	DecrementVerificationAttempts(context.Context, string, string, []byte, time.Time) error
	DeleteVerificationCode(context.Context, string, string, []byte) error
	RegisterUser(context.Context, User, []byte, UserSession, time.Time) (User, error)
	CreateUserSession(context.Context, string, string, UserSession, time.Time) (User, error)
	ResetUserPassword(context.Context, string, []byte, string, time.Time) error
	ChangeUserPassword(context.Context, string, string, string, UserSession, time.Time) error
	GetUserByEmail(context.Context, string) (User, error)
	GetUserBySession(context.Context, []byte, time.Time) (User, UserSession, error)
	DeleteUserSession(context.Context, []byte) error
	DeleteExpired(context.Context, time.Time) error
}
