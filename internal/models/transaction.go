package models

import (
	"time"
)

// Location represents geographical location data
type Location struct {
	Latitude  float64 `json:"latitude" db:"latitude"`
	Longitude float64 `json:"longitude" db:"longitude"`
	Address   string  `json:"address" db:"address"`
}

type ExternalBankTransfer struct {
	FromAccount string    `json:"fromAccount" validate:"required,max=10"`
	ToAccount   string    `json:"toAccount" validate:"required,max=10"`
	ToBankCode  string    `json:"toBankCode" validate:"required,max=3"`
	Amount      float64   `json:"amount" validate:"required,gt=100,max=1000000"`
	Currency    string    `json:"currency" validate:"required,len=3"`
	Reference   string    `json:"reference"`
	Narration   string    `json:"narration" validate:"max=200"`
	Location    *Location `json:"location"`
}

// Transaction represents a payment transaction
type Transaction struct {
	ID            int        `json:"id" db:"id"`
	TransactionID string     `json:"transaction_id" db:"transaction_id"`
	ReferenceID   string     `json:"reference_id" db:"reference_id"`
	FromCardID    string     `json:"from_card_id" db:"from_card_id"`
	ToCardID      string     `json:"to_card_id" db:"to_card_id"`
	Amount        float64    `json:"amount" db:"amount"`
	Fee           float64    `json:"fee" db:"fee"`
	TotalAmount   float64    `json:"total_amount" db:"total_amount"`
	Currency      string     `json:"currency" db:"currency"`
	Status        string     `json:"status" db:"status"`
	Type          string     `json:"type" db:"type"`
	Signature     string     `json:"signature" db:"signature"`
	DeviceID      string     `json:"device_id" db:"device_id"`
	Location      Location   `json:"location" db:"location"`
	SyncStatus    string     `json:"sync_status" db:"sync_status"`
	ErrorMessage  string     `json:"error_message" db:"error_message"`
	Metadata      Metadata   `json:"metadata" db:"metadata"`
	ToBankCode    string     `json:"to_bank_code,omitempty"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:" updated_at"`
	SettledAt     *time.Time `json:"settled_at" db:"settled_at"`
	ProcessedAt   *time.Time `json:"processed_at" db:"processed_at"`
}
