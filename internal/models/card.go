package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// Card represents an NFC card
type Card struct {
	ID                int        `json:"id" db:"id"`
	CardID            string     `json:"card_id" db:"card_id"`
	UserID            int        `json:"user_id" db:"user_id"`
	SerialNumber      string     `json:"serial_number" db:"serial_number"`
	Balance           float64    `json:"balance" db:"balance"`
	Currency          string     `json:"currency" db:"currency"`
	Status            string     `json:"status" db:"status"`
	CardType          string     `json:"card_type" db:"card_type"`
	LastSyncAt        *time.Time `json:"last_sync_at" db:"last_sync_at"`
	LastTransactionAt *time.Time `json:"last_transaction_at" db:"last_transaction_at"`
	TxCounter         int        `json:"tx_counter" db:"tx_counter"`
	MaxBalance        float64    `json:"max_balance" db:"max_balance"`
	DailySpent        float64    `json:"daily_spent" db:"daily_spent"`
	Metadata          Metadata   `json:"metadata" db:"metadata"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
	ExpiresAt         *time.Time `json:"expires_at" db:"expires_at"`
}

// CardSyncRequest represents card sync data from mobile
type CardSyncRequest struct {
	CardID       string        `json:"card_id" binding:"required"`
	Balance      float64       `json:"balance" binding:"required"`
	TxCounter    int           `json:"tx_counter" binding:"required"`
	LastSyncAt   time.Time     `json:"last_sync_at"`
	Transactions []Transaction `json:"transactions"`
	Signature    string        `json:"signature" binding:"required"`
}

// CardSyncResponse represents server response to card sync
type CardSyncResponse struct {
	CardID         string                `json:"card_id"`
	Balance        float64               `json:"balance"`
	Currency       string                `json:"currency"`
	LastSyncAt     time.Time             `json:"last_sync_at"`
	PendingUpdates []TransactionUpdate   `json:"pending_updates,omitempty"`
	Conflicts      []TransactionConflict `json:"conflicts,omitempty"`
	DailyLimit     float64               `json:"daily_limit"`
	DailySpent     float64               `json:"daily_spent"`
	IsActive       bool                  `json:"is_active"`
}

// TransactionUpdate represents pending transaction updates
type TransactionUpdate struct {
	TransactionID string     `json:"transaction_id"`
	Status        string     `json:"status"`
	SettledAt     *time.Time `json:"settled_at,omitempty"`
}

// TransactionConflict represents conflicting transactions
type TransactionConflict struct {
	LocalTransaction  Transaction `json:"local_transaction"`
	ServerTransaction Transaction `json:"server_transaction"`
	Resolution        string      `json:"resolution"` // "keep_local", "use_server", "manual"
}

// CardIssueRequest represents new card issuance
type CardIssueRequest struct {
	UserID         int     `json:"user_id" binding:"required"`
	CardType       string  `json:"card_type"`
	InitialBalance float64 `json:"initial_balance"`
	MaxBalance     float64 `json:"max_balance"`
}

// CardStatus represents card status
const (
	CardStatusActive   = "active"
	CardStatusInactive = "inactive"
	CardStatusBlocked  = "blocked"
	CardStatusLost     = "lost"
	CardStatusExpired  = "expired"
)

// Metadata type for JSONB fields
type Metadata map[string]any

// Value implements driver.Valuer for Metadata
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for Metadata
func (m *Metadata) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}

	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(b, m)
}
