package services

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/ruralpay/backend/internal/models"
)

type DoubleLedgerService struct {
	db              *sql.DB
	systemFeeAccount string
}

func NewDoubleLedgerService(db *sql.DB) *DoubleLedgerService {
	systemFeeAccount := "0000000001"
	if envAccount := os.Getenv("SYSTEM_FEE_ACCOUNT"); envAccount != "" {
		systemFeeAccount = envAccount
	}
	return &DoubleLedgerService{
		db:              db,
		systemFeeAccount: systemFeeAccount,
	}
}

func (s *DoubleLedgerService) Transfer(fromAccountID, toAccountID, transactionID string, amount int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	return s.TransferTx(tx, fromAccountID, toAccountID, transactionID, amount)
}

func (s *DoubleLedgerService) TransferTx(tx *sql.Tx, fromAccountID, toAccountID, transactionID string, amount int64) error {
	// Lock accounts in consistent order to prevent deadlocks
	if fromAccountID > toAccountID {
		fromAccountID, toAccountID = toAccountID, fromAccountID
		amount = -amount
	}

	// Lock and get from account
	fromAccount, err := s.lockAccount(tx, fromAccountID)
	if err != nil {
		return err
	}

	// Lock and get to account
	toAccount, err := s.lockAccount(tx, toAccountID)
	if err != nil {
		return err
	}

	// Check sufficient balance
	if fromAccount.Balance < amount {
		return fmt.Errorf("insufficient balance")
	}

	// Create debit entry
	if err := s.createLedgerEntry(tx, transactionID, fromAccountID, -amount, "DEBIT", fromAccount.Balance-amount); err != nil {
		return err
	}

	// Create credit entry
	if err := s.createLedgerEntry(tx, transactionID, toAccountID, amount, "CREDIT", toAccount.Balance+amount); err != nil {
		return err
	}

	// Update account balances
	if err := s.updateAccountBalance(tx, fromAccountID, fromAccount.Balance-amount, fromAccount.Version); err != nil {
		return err
	}

	if err := s.updateAccountBalance(tx, toAccountID, toAccount.Balance+amount, toAccount.Version); err != nil {
		return err
	}

	return nil
}

func (s *DoubleLedgerService) lockAccount(tx *sql.Tx, accountID string) (*models.Account, error) {
	var account models.Account
	err := tx.QueryRow(`
		SELECT id, balance, version, updated_at 
		FROM accounts 
		WHERE card_id = $1 OR account_id = $1 OR id = $1
		LIMIT 1
		FOR UPDATE`, accountID).Scan(&account.ID, &account.Balance, &account.Version, &account.UpdatedAt)
	
	return &account, err
}

func (s *DoubleLedgerService) createLedgerEntry(tx *sql.Tx, transactionID, accountID string, amount int64, entryType string, balance int64) error {
	var actualAccountID string
	err := tx.QueryRow(`SELECT id FROM accounts WHERE card_id = $1 OR account_id = $1 OR id = $1 LIMIT 1`, accountID).Scan(&actualAccountID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO ledger_entries (transaction_id, account_id, amount, entry_type, balance, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		transactionID, actualAccountID, amount, entryType, balance, time.Now())
	return err
}

func (s *DoubleLedgerService) updateAccountBalance(tx *sql.Tx, accountID string, newBalance int64, version int) error {
	result, err := tx.Exec(`
		UPDATE accounts 
		SET balance = $1, version = version + 1, updated_at = $2 
		WHERE id = $3 AND version = $4`,
		newBalance, time.Now(), accountID, version)
	
	if err != nil {
		return err
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if rowsAffected == 0 {
		return fmt.Errorf("optimistic lock failed for account %s", accountID)
	}
	
	return nil
}