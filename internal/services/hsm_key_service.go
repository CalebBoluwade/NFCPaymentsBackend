package services

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/ruralpay/backend/internal/hsm"
)

type HSMKeyService struct {
	db  *sql.DB
	hsm hsm.HSMInterface
}

func NewHSMKeyService(db *sql.DB, hsm hsm.HSMInterface) *HSMKeyService {
	return &HSMKeyService{
		db:  db,
		hsm: hsm,
	}
}

// SyncKeysToDatabase syncs HSM keys to the database
func (s *HSMKeyService) SyncKeysToDatabase() error {
	// Get all key IDs that should exist
	keyIDs := []string{"card_signing", "transaction_signing", "user_encryption"}

	for _, keyID := range keyIDs {
		if err := s.syncKeyToDatabase(keyID); err != nil {
			return fmt.Errorf("failed to sync key %s: %w", keyID, err)
		}
	}

	return nil
}

func (s *HSMKeyService) syncKeyToDatabase(keyID string) error {
	// Get public key from HSM
	publicKeyPEM, err := s.hsm.GetPublicKey(keyID)
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	// Determine key type and size
	keyType, keySize := s.getKeyTypeAndSize(keyID, publicKeyPEM)

	// Call the database upsert function
	_, err = s.db.Exec(`
		SELECT upsert_hsm_key($1, $2, $3, $4, $5, $6, $7, $8)
	`, keyID, keyType, keyID, keySize, publicKeyPEM, "ENCRYPTED_BY_HSM", 
		time.Now().Add(365*24*time.Hour), `{"synced_from_hsm": true}`)

	if err != nil {
		return fmt.Errorf("failed to upsert key to database: %w", err)
	}

	return nil
}

func (s *HSMKeyService) getKeyTypeAndSize(keyID, publicKeyPEM string) (string, int) {
	if keyID == "user_encryption" {
		return "AES", 256
	}

	// Parse RSA public key to get size
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block != nil {
		if pubKey, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
			if rsaKey, ok := pubKey.(*rsa.PublicKey); ok {
				return "RSA", rsaKey.Size() * 8
			}
		}
	}

	// Default fallback
	return "RSA", 2048
}