package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis/v8"
	"github.com/ruralpay/backend/internal/hsm"
	"github.com/ruralpay/backend/internal/models"
)

type TransactionService struct {
	db            *sql.DB
	redis         *redis.Client
	hsm           hsm.HSMInterface
	ledger        *DoubleLedgerService
	audit         *hsm.AuditLogger
	validator     *ValidationHelper
	feePercentage float64
	feeFixed      int64
}

type Transaction struct {
	Version    uint8     `json:"version"`
	TxID       string    `json:"txId" validate:"required"`
	Timestamp  int64     `json:"timestamp" validate:"required"`
	CardID     string    `json:"cardId" validate:"required"`
	MerchantID string    `json:"merchantId" validate:"required"`
	Amount     int64     `json:"amount" validate:"required,gt=0"`
	Currency   string    `json:"currency" validate:"required,len=3"`
	Counter    uint32    `json:"counter" validate:"required,gt=0"`
	TxType     string    `json:"txType" validate:"required,oneof=DEBIT CREDIT"`
	Signature  string    `json:"signature" validate:"required"`
	Narration  string    `json:"narration,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

func NewTransactionService(db *sql.DB, redis *redis.Client, hsmInstance hsm.HSMInterface) *TransactionService {
	feePercentage := 0.5
	feeFixed := int64(50)
	if envFeePercentage := os.Getenv("TRANSFER_FEE_PERCENTAGE"); envFeePercentage != "" {
		if val, err := strconv.ParseFloat(envFeePercentage, 64); err == nil {
			feePercentage = val
		}
	}
	if envFeeFixed := os.Getenv("TRANSFER_FEE_FIXED"); envFeeFixed != "" {
		if val, err := strconv.ParseInt(envFeeFixed, 10, 64); err == nil {
			feeFixed = val
		}
	}
	return &TransactionService{
		db:            db,
		redis:         redis,
		hsm:           hsmInstance,
		ledger:        NewDoubleLedgerService(db),
		audit:         hsm.NewAuditLogger(),
		validator:     NewValidationHelper(),
		feePercentage: feePercentage,
		feeFixed:      feeFixed,
	}
}

// CreateTransaction handles single transaction creation
// @Summary Create a new transaction
// @Description Process a single NFC payment transaction
// @Tags transactions
// @Accept json
// @Produce json
// @Param transaction body Transaction true "Transaction data"
// @Success 200 {object} Transaction
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /transactions [post]
func (ts *TransactionService) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Transaction Transaction `json:"transaction"`
	}

	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest, nil)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		SendErrorResponse(w, "Request body must only contain a single JSON object", http.StatusBadRequest, nil)
		return
	}

	tx := req.Transaction

	// Validate transaction struct
	if err := ts.validator.ValidateStruct(&tx); err != nil {
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	// Check for duplicate transaction (idempotency)
	var existingStatus string
	err := ts.db.QueryRow(`SELECT status FROM transactions WHERE transaction_id = $1`, tx.TxID).Scan(&existingStatus)
	if err == nil {
		log.Printf("[TRANSACTION] Duplicate transaction detected: %s, status: %s", tx.TxID, existingStatus)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     existingStatus == "COMPLETED",
			"transaction": tx,
			"message":     "Transaction already processed",
		})
		return
	}

	// Validate transaction
	if err := ts.validateTransaction(&tx); err != nil {
		http.Error(w, fmt.Sprintf("Validation failed: %v", err), http.StatusBadRequest)
		return
	}

	// Verify signature
	if err := ts.verifySignature(&tx); err != nil {
		http.Error(w, fmt.Sprintf("Signature verification failed: %v", err), http.StatusUnauthorized)
		return
	}

	// Begin database transaction
	dbTx, err := ts.db.Begin()
	if err != nil {
		log.Printf("[TRANSACTION] Failed to begin transaction: %v", err)
		http.Error(w, "Failed to process transaction", http.StatusInternalServerError)
		return
	}
	defer dbTx.Rollback()

	// Process ledger transfer
	if err := ts.processLedgerTransferTx(dbTx, &tx); err != nil {
		ts.audit.LogError(tx.TxID, tx.CardID, err)
		http.Error(w, "Failed to process transfer", http.StatusInternalServerError)
		return
	}

	// Store transaction
	if err := ts.storeTransactionTx(dbTx, &tx); err != nil {
		ts.audit.LogError(tx.TxID, tx.CardID, err)
		http.Error(w, "Failed to store transaction", http.StatusInternalServerError)
		return
	}

	// Commit transaction
	if err := dbTx.Commit(); err != nil {
		log.Printf("[TRANSACTION] Failed to commit transaction: %v", err)
		ts.audit.LogError(tx.TxID, tx.CardID, err)
		http.Error(w, "Failed to process transaction", http.StatusInternalServerError)
		return
	}

	// Queue for settlement (after commit)
	if err := ts.queueForSettlement(&tx); err != nil {
		log.Printf("Failed to queue transaction for settlement: %v", err)
	}

	// Send notification
	go ts.notifyTransaction(&tx)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"transaction": tx,
	})
}

// BatchTransactions handles multiple transaction processing
// @Summary Process multiple transactions
// @Description Process a batch of NFC payment transactions
// @Tags transactions
// @Accept json
// @Produce json
// @Param transactions body object{transactions=[]Transaction} true "Batch transaction data"
// @Success 200 {object} object{processed=[]Transaction,failed=[]object,summary=object}
// @Failure 400 {object} map[string]string
// @Router /transactions/batch [post]
func (ts *TransactionService) BatchTransactions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Transactions []Transaction `json:"transactions"`
	}

	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		log.Printf("BatchTransactions: Failed to decode request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		log.Printf("BatchTransactions: Multiple JSON objects detected")
		http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
		return
	}

	if len(req.Transactions) == 0 {
		http.Error(w, "No transactions provided", http.StatusBadRequest)
		return
	}

	if len(req.Transactions) > 100 {
		http.Error(w, "Batch size exceeds limit (100)", http.StatusBadRequest)
		return
	}

	processed := []Transaction{}
	failed := []map[string]interface{}{}

	for _, tx := range req.Transactions {
		// Validate
		if err := ts.validateTransaction(&tx); err != nil {
			failed = append(failed, map[string]interface{}{
				"txId":  tx.TxID,
				"error": err.Error(),
			})
			continue
		}

		// Verify signature
		if err := ts.verifySignature(&tx); err != nil {
			failed = append(failed, map[string]interface{}{
				"txId":  tx.TxID,
				"error": "Signature verification failed",
			})
			continue
		}

		// Check double spending
		if err := ts.checkDoubleSpending(&tx); err != nil {
			failed = append(failed, map[string]interface{}{
				"txId":  tx.TxID,
				"error": "Double spending detected",
			})
			continue
		}

		// Process ledger transfer
		if err := ts.processLedgerTransfer(&tx); err != nil {
			ts.audit.LogError(tx.TxID, tx.CardID, err)
			failed = append(failed, map[string]interface{}{
				"txId":  tx.TxID,
				"error": "Transfer failed",
			})
			continue
		}

		// Store
		if err := ts.storeTransaction(&tx); err != nil {
			failed = append(failed, map[string]interface{}{
				"txId":  tx.TxID,
				"error": "Storage failed",
			})
			continue
		}

		processed = append(processed, tx)
	}

	// Queue all processed transactions for settlement
	if len(processed) > 0 {
		go ts.batchSettlement(processed)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"processed": processed,
		"FAILED":    failed,
		"summary": map[string]int{
			"total":     len(req.Transactions),
			"succeeded": len(processed),
			"FAILED":    len(failed),
		},
	})
}

// GetTransaction retrieves a specific transaction
// @Summary Get transaction by ID
// @Description Retrieve a transaction by its ID
// @Tags transactions
// @Produce json
// @Param txId path string true "Transaction ID"
// @Success 200 {object} Transaction
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /transactions/{txId} [get]
func (ts *TransactionService) GetTransaction(w http.ResponseWriter, r *http.Request) {
	txID := chi.URLParam(r, "txId")

	tx, err := ts.fetchTransaction(txID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Transaction not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to fetch transaction", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tx)
}

// ListTransactions retrieves transactions with optional filters
// @Summary List transactions
// @Description Get a list of transactions with optional filtering
// @Tags transactions
// @Produce json
// @Param id query string false "Filter by transaction ID"
// @Param cardId query string false "Filter by card ID"
// @Param status query string false "Filter by status"
// @Success 200 {object} object{transactions=[]Transaction,count=int}
// @Failure 500 {object} map[string]string
// @Router /transactions [get]
func (ts *TransactionService) ListTransactions(w http.ResponseWriter, r *http.Request) {
	// Check if requesting single transaction by ID
	if txID := r.URL.Query().Get("id"); txID != "" {
		tx, err := ts.fetchTransaction(txID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "Transaction not found", http.StatusNotFound)
			} else {
				http.Error(w, "Failed to fetch transaction", http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tx)
		return
	}

	// Parse query parameters
	cardID := r.URL.Query().Get("cardId")
	status := r.URL.Query().Get("status")
	limit := 50

	transactions, err := ts.fetchTransactions(cardID, status, limit)
	if err != nil {
		http.Error(w, "Failed to fetch transactions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"transactions": transactions,
		"count":        len(transactions),
	})
}

// GetRecentTransactions retrieves recent transactions
// @Summary Get recent transactions
// @Description Get a list of recent transactions with configurable limit
// @Tags transactions
// @Produce json
// @Param limit query int false "Number of transactions to return (default: 10, max: 100)"
// @Success 200 {array} Transaction
// @Failure 500 {object} map[string]string
// @Router /transactions/recent [get]
func (ts *TransactionService) GetRecentTransactions(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value("userID").(string)
	if !ok || userID == "" {
		SendErrorResponse(w, "Unauthorized", http.StatusUnauthorized, nil)
		return
	}

	var req struct {
		Limit int `validate:"omitempty,min=1,max=100"`
	}
	req.Limit = 10

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = l
		}
	}

	if err := ts.validator.ValidateStruct(&req); err != nil {
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	transactions, err := ts.fetchRecentTransactions(userID, req.Limit)
	if err != nil {
		SendErrorResponse(w, "Failed to fetch recent transactions", http.StatusInternalServerError, nil)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(transactions)
}

// AccountNameEnquiry retrieves account name for a card or account ID
// @Summary Get account name
// @Description Retrieve account name for a given account ID and bank code (checks local DB first, then external API)
// @Tags accounts
// @Produce json
// @Param accountId query string true "Account ID"
// @Param bankCode query string false "Bank Code"
// @Success 200 {object} object{responseCode=string,accountId=string,accountName=string,status=string,source=string}
// @Failure 404 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /accounts/name-enquiry [get]
func (ts *TransactionService) AccountNameEnquiry(w http.ResponseWriter, r *http.Request) {
	accountId := strings.TrimSpace(r.URL.Query().Get("accountId"))
	bankCode := strings.TrimSpace(r.URL.Query().Get("bankCode"))
	log.Printf("[ACCOUNT_ENQUIRY] Name enquiry request for accountId: %s, bankCode: %s from IP: %s", accountId, bankCode, r.RemoteAddr)

	if accountId == "" {
		http.Error(w, "accountId is required", http.StatusBadRequest)
		return
	}

	if !isValidAccountId(accountId) {
		http.Error(w, "invalid accountId format", http.StatusBadRequest)
		return
	}

	if bankCode != "" && !isValidBankCode(bankCode) {
		http.Error(w, "invalid bankCode format", http.StatusBadRequest)
		return
	}

	// Try local DB
	log.Printf("[ACCOUNT_ENQUIRY] Attempting local DB lookup for: %s", accountId)
	var accountName string
	var status string
	err := ts.db.QueryRow(`
		SELECT account_name, status FROM accounts 
		WHERE card_id = $1 OR account_id = $1
		LIMIT 1
	`, accountId).Scan(&accountName, &status)

	if err == nil {
		log.Printf("[ACCOUNT_ENQUIRY] Found in local DB for accountId: %s, account: %s, status: %s", accountId, accountName, status)
		if status != "ACTIVE" {
			log.Printf("[ACCOUNT_ENQUIRY] Account not active for accountId: %s, status: %s", accountId, status)
			http.Error(w, "Account not active", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"responseCode": "00",
			"accountId":    accountId,
			"accountName":  accountName,
			"status":       "SUCCESS",
			"source":       "local",
		})
		return
	}

	// Not found locally, try external API
	log.Printf("[ACCOUNT_ENQUIRY] Not found in local DB, attempting external API lookup for: %s", accountId)
	accountName, err = ts.callExternalNameEnquiry(accountId)
	if err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] External API lookup failed for accountId %s: %v", accountId, err)
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	log.Printf("[ACCOUNT_ENQUIRY] Found via external API for accountId: %s, account: %s", accountId, accountName)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"responseCode": "00",
		"accountId":    accountId,
		"accountName":  accountName,
		"status":       "SUCCESS",
		"source":       "external",
	})
}

// AccountBalanceEnquiry retrieves account balance for a card or account ID
// @Summary Get account balance
// @Description Retrieve account balance for a given account ID and bank code (checks local DB first, then external API)
// @Tags accounts
// @Produce json
// @Param accountId query string true "Account ID"
// @Param bankCode query string false "Bank Code"
// @Success 200 {object} object{responseCode=string,accountId=string,availableBalance=int64,status=string,source=string}
// @Failure 404 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /accounts/balance-enquiry [get]
func (ts *TransactionService) AccountBalanceEnquiry(w http.ResponseWriter, r *http.Request) {
	accountId := strings.TrimSpace(r.URL.Query().Get("accountId"))
	bankCode := strings.TrimSpace(r.URL.Query().Get("bankCode"))
	log.Printf("[ACCOUNT_ENQUIRY] Balance enquiry request for accountId: %s, bankCode: %s from IP: %s", accountId, bankCode, r.RemoteAddr)

	if accountId == "" {
		http.Error(w, "accountId is required", http.StatusBadRequest)
		return
	}

	if !isValidAccountId(accountId) {
		http.Error(w, "invalid accountId format", http.StatusBadRequest)
		return
	}

	if bankCode != "" && !isValidBankCode(bankCode) {
		http.Error(w, "invalid bankCode format", http.StatusBadRequest)
		return
	}

	// Try local DB
	log.Printf("[ACCOUNT_ENQUIRY] Attempting local DB lookup for: %s", accountId)
	var balance int64
	var status string
	err := ts.db.QueryRow(`
		SELECT balance, status FROM accounts 
		WHERE card_id = $1 OR account_id = $1
		LIMIT 1
	`, accountId).Scan(&balance, &status)

	if err == nil {
		log.Printf("[ACCOUNT_ENQUIRY] Found in local DB for accountId: %s, balance: %d, status: %s", accountId, balance, status)
		if status != "ACTIVE" {
			log.Printf("[ACCOUNT_ENQUIRY] Account not active for accountId: %s, status: %s", accountId, status)
			http.Error(w, "Account not active", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"responseCode":     "00",
			"accountId":        accountId,
			"availableBalance": balance,
			"status":           "SUCCESS",
			"source":           "local",
		})
		return
	}

	// Not found locally, try external API
	log.Printf("[ACCOUNT_ENQUIRY] Not found in local DB, attempting external API lookup for: %s", accountId)
	balance, err = ts.callExternalBalanceEnquiry(accountId)
	if err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] External API lookup failed for accountId %s: %v", accountId, err)
		http.Error(w, "Account not found", http.StatusNotFound)
		return
	}

	log.Printf("[ACCOUNT_ENQUIRY] Found via external API for accountId: %s, balance: %d", accountId, balance)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"responseCode":     "00",
		"accountId":        accountId,
		"availableBalance": balance,
		"status":           "SUCCESS",
		"source":           "external",
	})
}

// Validation functions

func (ts *TransactionService) validateTransaction(tx *Transaction) error {
	if tx.TxID == "" {
		return errors.New("transaction ID is required")
	}

	if tx.CardID == "" {
		return errors.New("card ID is required")
	}

	// Validate account using existing enquiry method
	if err := ts.validateAccountInternal(tx.CardID); err != nil {
		return err
	}

	if tx.MerchantID == "" {
		return errors.New("merchant ID is required")
	}

	if tx.Amount <= 0 {
		return errors.New("amount must be positive")
	}

	// Check sufficient balance for debit transactions using existing enquiry method
	if tx.TxType == "DEBIT" {
		if err := ts.checkSufficientBalanceInternal(tx.CardID, tx.Amount); err != nil {
			return err
		}
	}

	if tx.Currency == "" {
		return errors.New("currency is required")
	}

	if tx.Counter == 0 {
		return errors.New("counter is required")
	}

	if tx.Signature == "" {
		return errors.New("signature is required")
	}

	// Check timestamp (not too old, not in future)
	now := time.Now().Unix()
	if tx.Timestamp > now+60 {
		return errors.New("transaction timestamp is in the future")
	}

	if tx.Timestamp < now-86400*7 {
		return errors.New("transaction timestamp is too old (>7 days)")
	}

	return nil
}

func (ts *TransactionService) validateAccountInternal(cardID string) error {
	var status string
	err := ts.db.QueryRow(`
		SELECT status FROM accounts 
		WHERE card_id = $1
	`, cardID).Scan(&status)

	if err != nil {
		if err == sql.ErrNoRows {
			return errors.New("account not found")
		}
		return errors.New("account validation failed")
	}

	if status != "ACTIVE" {
		return errors.New("account not active")
	}

	return nil
}

func (ts *TransactionService) checkSufficientBalanceInternal(cardID string, amount int64) error {
	var balance int64
	var status string
	err := ts.db.QueryRow(`
		SELECT balance, status FROM accounts 
		WHERE card_id = $1
	`, cardID).Scan(&balance, &status)

	if err != nil {
		if err == sql.ErrNoRows {
			return errors.New("account not found")
		}
		return errors.New("balance check failed")
	}

	if status != "ACTIVE" {
		return errors.New("account not active")
	}

	if balance < amount {
		return errors.New("insufficient balance")
	}

	return nil
}

func (ts *TransactionService) verifySignature(tx *Transaction) error {
	// Fetch card keys
	cak, err := ts.getCardAuthKey(tx.CardID)
	if err != nil {
		return fmt.Errorf("failed to get card keys: %v", err)
	}

	// Reconstruct signature
	data := ts.serializeTransaction(tx)

	h := hmac.New(sha256.New, cak)
	h.Write(data)
	expectedSig := hex.EncodeToString(h.Sum(nil))

	if expectedSig != tx.Signature {
		return errors.New("signature mismatch")
	}

	return nil
}

func (ts *TransactionService) serializeTransaction(tx *Transaction) []byte {
	data := []byte{}
	data = append(data, tx.Version)
	data = append(data, []byte(tx.TxID)...)
	data = append(data, int64ToBytes(tx.Timestamp)...)
	data = append(data, []byte(tx.CardID)...)
	data = append(data, []byte(tx.MerchantID)...)
	data = append(data, int64ToBytes(tx.Amount)...)
	data = append(data, []byte(tx.Currency)...)
	data = append(data, uint32ToBytes(tx.Counter)...)
	data = append(data, []byte(tx.TxType)...)
	return data
}

func (ts *TransactionService) checkDoubleSpending(tx *Transaction) error {
	// Check if counter has been used
	var exists bool
	err := ts.db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM transactions 
            WHERE card_id = $1 AND counter = $2
        )
    `, tx.CardID, tx.Counter).Scan(&exists)

	if err != nil {
		return err
	}

	if exists {
		return errors.New("counter already used")
	}

	// Check if counter is incrementing
	var maxCounter uint32
	err = ts.db.QueryRow(`
        SELECT COALESCE(MAX(counter), 0) 
        FROM transactions 
        WHERE card_id = $1
    `, tx.CardID).Scan(&maxCounter)

	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if tx.Counter <= maxCounter {
		return errors.New("counter not incrementing")
	}

	return nil
}

func (ts *TransactionService) storeTransaction(tx *Transaction) error {
	tx.Status = "COMPLETED"
	tx.CreatedAt = time.Now()

	_, err := ts.db.Exec(`
        INSERT INTO transactions 
        (transaction_id, from_card_id, to_card_id, amount, currency, type, signature, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, tx.TxID, tx.CardID, tx.MerchantID, tx.Amount, tx.Currency,
		tx.TxType, tx.Signature, tx.Status, tx.CreatedAt)

	return err
}

func (ts *TransactionService) storeTransactionTx(dbTx *sql.Tx, tx *Transaction) error {
	tx.Status = "COMPLETED"
	tx.CreatedAt = time.Now()

	_, err := dbTx.Exec(`
        INSERT INTO transactions 
        (transaction_id, from_card_id, to_card_id, amount, currency, type, signature, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, tx.TxID, tx.CardID, tx.MerchantID, tx.Amount, tx.Currency,
		tx.TxType, tx.Signature, tx.Status, tx.CreatedAt)

	return err
}

func (ts *TransactionService) processLedgerTransferTx(dbTx *sql.Tx, tx *Transaction) error {
	err := ts.ledger.TransferTx(dbTx, tx.CardID, tx.MerchantID, tx.TxID, tx.Amount)

	if err != nil {
		ts.audit.LogError(tx.TxID, tx.CardID, err)
		return err
	}

	ts.audit.LogTransfer(tx.TxID, tx.CardID, tx.MerchantID, tx.Amount, "SUCCESS")
	return nil
}

func (ts *TransactionService) queueForSettlement(tx *Transaction) error {
	// Push to Redis queue
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}

	return ts.redis.RPush(context.Background(), "settlement_queue", data).Err()
}

func (ts *TransactionService) notifyTransaction(tx *Transaction) {
	// Send notification (SMS, push, etc.)
	log.Printf("Notification: Transaction %s completed for card %s", tx.TxID, tx.CardID)
}

func (ts *TransactionService) batchSettlement(transactions []Transaction) {
	// Convert to ISO 20022 format
	iso20022Service := NewISO20022Service()

	for _, tx := range transactions {
		// Convert local Transaction to models.Transaction
		modelTx := &models.Transaction{
			TransactionID: tx.TxID,
			ReferenceID:   tx.TxID, // Use TxID as reference
			FromCardID:    tx.CardID,
			ToCardID:      tx.MerchantID,
			Amount:        float64(tx.Amount) / 100, // Convert from cents
			Currency:      tx.Currency,
			Status:        tx.Status,
		}

		// Create pacs.008 message
		doc, err := iso20022Service.ConvertTransaction(modelTx)
		if err != nil {
			log.Printf("Failed to convert transaction %s: %v", tx.TxID, err)
			continue
		}

		// Send to settlement system
		if err := iso20022Service.SendToSettlement(doc); err != nil {
			log.Printf("Failed to send transaction %s to settlement: %v", tx.TxID, err)
			continue
		}

		log.Printf("Transaction %s queued for settlement", tx.TxID)
	}
}

// Database helper functions

func (ts *TransactionService) getCardAuthKey(cardID string) ([]byte, error) {
	var cakHex string
	err := ts.db.QueryRow(`
        SELECT cak FROM cards WHERE card_id = $1
    `, cardID).Scan(&cakHex)

	if err != nil {
		return nil, err
	}

	return hex.DecodeString(cakHex)
}

func (ts *TransactionService) fetchTransaction(txID string) (*Transaction, error) {
	tx := &Transaction{}
	var dbType, amountStr string
	err := ts.db.QueryRow(`
        SELECT transaction_id, from_card_id, to_card_id, amount::text, currency, 
               EXTRACT(EPOCH FROM created_at)::bigint as timestamp,
               COALESCE(signature, '') as signature, COALESCE(type, 'transfer') as type, status, created_at
        FROM transactions
        WHERE transaction_id = $1
    `, txID).Scan(
		&tx.TxID, &tx.CardID, &tx.MerchantID, &amountStr, &tx.Currency,
		&tx.Timestamp, &tx.Signature, &dbType, &tx.Status, &tx.CreatedAt,
	)

	if err != nil {
		log.Printf("[TRANSACTION] Failed to fetch transaction %s: %v", txID, err)
		return nil, err
	}

	amount, _ := strconv.ParseFloat(amountStr, 64)
	tx.Amount = int64(amount)
	tx.Counter = 0
	if dbType == "DEBIT" || dbType == "CREDIT" {
		tx.TxType = dbType
	} else {
		tx.TxType = "DEBIT"
	}
	tx.Version = 1
	return tx, nil
}

func (ts *TransactionService) fetchTransactions(cardID, status string, limit int) ([]Transaction, error) {
	var conditions []string
	var args []interface{}
	argIndex := 1

	baseQuery := `
        SELECT transaction_id, from_card_id, to_card_id, amount, currency, 
               0 as counter, EXTRACT(EPOCH FROM created_at)::bigint as timestamp,
               COALESCE(signature, '') as signature, COALESCE(type, 'DEBIT') as type, status, created_at
        FROM transactions
    `

	if cardID != "" {
		conditions = append(conditions, fmt.Sprintf("card_id = $%d", argIndex))
		args = append(args, cardID)
		argIndex++
	}

	if status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIndex))
		args = append(args, status)
		argIndex++
	}

	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d", argIndex)
	args = append(args, limit)

	rows, err := ts.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := []Transaction{}
	for rows.Next() {
		tx := Transaction{Version: 1}
		var dbType string
		err := rows.Scan(
			&tx.TxID, &tx.CardID, &tx.MerchantID, &tx.Amount, &tx.Currency,
			&tx.Counter, &tx.Timestamp, &tx.Signature, &dbType, &tx.Status, &tx.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if dbType == "DEBIT" || dbType == "CREDIT" {
			tx.TxType = dbType
		} else {
			tx.TxType = "DEBIT"
		}
		transactions = append(transactions, tx)
	}

	return transactions, nil
}

func (ts *TransactionService) fetchRecentTransactions(userID string, limit int) ([]Transaction, error) {
	query := `
		SELECT t.transaction_id, t.from_card_id, t.to_card_id, t.amount, t.currency, 
		       0 as counter, EXTRACT(EPOCH FROM t.created_at)::bigint as timestamp,
		       COALESCE(t.signature, '') as signature, COALESCE(t.type, 'DEBIT') as type, t.status, t.created_at
		FROM transactions t
		INNER JOIN cards c1 ON t.from_card_id = c1.card_id
		INNER JOIN users u1 ON c1.user_id = u1.id
		WHERE u1.id = $1::integer
		   OR EXISTS (
		       SELECT 1 FROM cards c2 
		       INNER JOIN users u2 ON c2.user_id = u2.id
		       WHERE c2.card_id = t.to_card_id AND u2.id = $1::integer
		   )
		ORDER BY t.created_at DESC
		LIMIT $2
	`

	rows, err := ts.db.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	transactions := []Transaction{}
	for rows.Next() {
		tx := Transaction{Version: 1}
		var amountStr, dbType string
		err := rows.Scan(
			&tx.TxID, &tx.CardID, &tx.MerchantID, &amountStr, &tx.Currency,
			&tx.Counter, &tx.Timestamp, &tx.Signature, &dbType, &tx.Status, &tx.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		amount, _ := strconv.ParseFloat(amountStr, 64)
		tx.Amount = int64(amount)
		if dbType == "DEBIT" || dbType == "CREDIT" {
			tx.TxType = dbType
		} else {
			tx.TxType = "DEBIT"
		}
		transactions = append(transactions, tx)
	}

	return transactions, nil
}

// Utility functions

func (ts *TransactionService) callExternalNameEnquiry(identifier string) (string, error) {
	externalURL := fmt.Sprintf("https://external-api.example.com/accounts/%s/name", identifier)
	log.Printf("[ACCOUNT_ENQUIRY] Calling external API: %s", externalURL)
	resp, err := http.Get(externalURL)
	if err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] External API request failed: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ACCOUNT_ENQUIRY] External API returned non-OK status: %d", resp.StatusCode)
		return "", fmt.Errorf("external API returned status %d", resp.StatusCode)
	}

	var result struct {
		AccountName string `json:"accountName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] Failed to decode external API response: %v", err)
		return "", err
	}

	log.Printf("[ACCOUNT_ENQUIRY] External API returned account name: %s", result.AccountName)
	return result.AccountName, nil
}

func (ts *TransactionService) callExternalBalanceEnquiry(identifier string) (int64, error) {
	externalURL := fmt.Sprintf("https://external-api.example.com/accounts/%s/balance", identifier)
	log.Printf("[ACCOUNT_ENQUIRY] Calling external API: %s", externalURL)
	resp, err := http.Get(externalURL)
	if err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] External API request failed: %v", err)
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[ACCOUNT_ENQUIRY] External API returned non-OK status: %d", resp.StatusCode)
		return 0, fmt.Errorf("external API returned status %d", resp.StatusCode)
	}

	var result struct {
		Balance int64 `json:"balance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[ACCOUNT_ENQUIRY] Failed to decode external API response: %v", err)
		return 0, err
	}

	log.Printf("[ACCOUNT_ENQUIRY] External API returned balance: %d", result.Balance)
	return result.Balance, nil
}

func int64ToBytes(n int64) []byte {
	bytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		bytes[i] = byte(n & 0xff)
		n >>= 8
	}
	return bytes
}

func uint32ToBytes(n uint32) []byte {
	bytes := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		bytes[i] = byte(n & 0xff)
		n >>= 8
	}
	return bytes
}

func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
}

var (
	accountIdRegex = regexp.MustCompile(`^[0-9]{10,20}$`)
	bankCodeRegex  = regexp.MustCompile(`^[0-9A-Za-z]{3,6}$`)
)

func isValidAccountId(s string) bool {
	return accountIdRegex.MatchString(s)
}

func isValidBankCode(s string) bool {
	return bankCodeRegex.MatchString(s)
}
func (ts *TransactionService) processLedgerTransfer(tx *Transaction) error {
	// Transfer from card account to merchant account
	err := ts.ledger.Transfer(tx.CardID, tx.MerchantID, tx.TxID, tx.Amount)

	if err != nil {
		ts.audit.LogError(tx.TxID, tx.CardID, err)
		return err
	}

	ts.audit.LogTransfer(tx.TxID, tx.CardID, tx.MerchantID, tx.Amount, "SUCCESS")
	return nil
}

// ExternalBankTransfer handles bank-to-bank transfers using ISO 20022
// @Summary Send external bank transfer
// @Description Process bank-to-bank transfer using ISO 20022 messaging
// @Tags transactions
// @Accept json
// @Produce json
// @Param transfer body object{fromAccount=string,toAccount=string,toBankCode=string,amount=float64,currency=string,reference=string} true "Transfer details"
// @Success 200 {object} object{success=bool,transactionId=string,status=string}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /transactions/external [post]
func (ts *TransactionService) calculateFee(amount int64) int64 {
	fee := int64(float64(amount) * ts.feePercentage / 100)
	return fee + ts.feeFixed
}

func (ts *TransactionService) ExternalBankTransfer(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalBankTransfer

	ipAddress := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ipAddress = strings.Split(forwarded, ",")[0]
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		ipAddress = realIP
	}
	log.Printf("[EXTERNAL_TRANSFER] Request from IP: %s", ipAddress)

	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&req); err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Invalid request: %v", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		log.Printf("[EXTERNAL_TRANSFER] Multiple JSON objects detected")
		http.Error(w, "Request body must only contain a single JSON object", http.StatusBadRequest)
		return
	}

	// Validate request
	if err := ts.validator.ValidateStruct(&req); err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Validation failed: %v", err)
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	// Security validations
	if req.FromAccount == req.ToAccount {
		log.Printf("[EXTERNAL_TRANSFER] Same account transfer attempt: %s", req.FromAccount)
		http.Error(w, "Cannot transfer to same account", http.StatusBadRequest)
		return
	}

	log.Printf("[EXTERNAL_TRANSFER] Transfer request: from=%s, to=%s, bank=%s, amount=%.2f %s",
		req.FromAccount, req.ToAccount, req.ToBankCode, req.Amount, req.Currency)

	// Generate transaction ID
	txID := fmt.Sprintf("EXT-%d", time.Now().UnixNano())
	if req.Reference != "" {
		txID = req.Reference
	}
	log.Printf("[EXTERNAL_TRANSFER] Transaction ID: %s", txID)

	// Check for duplicate transaction (idempotency)
	var existingStatus string
	err := ts.db.QueryRow(`SELECT status FROM transactions WHERE transaction_id = $1`, txID).Scan(&existingStatus)
	if err == nil {
		log.Printf("[EXTERNAL_TRANSFER] Duplicate transaction detected: %s, status: %s", txID, existingStatus)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       existingStatus == "PENDING" || existingStatus == "COMPLETED",
			"transactionId": txID,
			"status":        existingStatus,
			"message":       "Transaction already processed",
		})
		return
	}

	// Begin database transaction
	tx, err := ts.db.Begin()
	if err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Failed to begin transaction: %v", err)
		http.Error(w, "Failed to process transfer", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Validate source account
	var balance int64
	var status string
	err = tx.QueryRow(`
		SELECT balance, status FROM accounts 
		WHERE account_id = $1 OR card_id = $1
		LIMIT 1 FOR UPDATE
	`, req.FromAccount).Scan(&balance, &status)

	if err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Source account not found: %s", req.FromAccount)
		amount := int64(req.Amount)
		fee := ts.calculateFee(amount)
		var locationJSON []byte
		if req.Location != nil {
			locationJSON, _ = json.Marshal(req.Location)
		}
		metadata := map[string]any{"ip_address": ipAddress}
		metadataJSON, _ := json.Marshal(metadata)
		_, _ = tx.Exec(`
			INSERT INTO transactions 
			(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
		`, txID, req.FromAccount, req.ToAccount, amount, fee, amount+fee, req.Currency, req.Narration, "FAILED_ACCOUNT_NOT_FOUND", locationJSON, metadataJSON)
		tx.Commit()
		ts.audit.LogError(txID, req.FromAccount, errors.New("source account not found"))
		http.Error(w, "Source account not found", http.StatusNotFound)
		return
	}

	if status != "ACTIVE" {
		log.Printf("[EXTERNAL_TRANSFER] Source account not active: %s", req.FromAccount)
		amount := int64(req.Amount)
		fee := ts.calculateFee(amount)
		var locationJSON []byte
		if req.Location != nil {
			locationJSON, _ = json.Marshal(req.Location)
		}
		metadata := map[string]interface{}{"ip_address": ipAddress}
		metadataJSON, _ := json.Marshal(metadata)
		_, _ = tx.Exec(`
			INSERT INTO transactions 
			(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
		`, txID, req.FromAccount, req.ToAccount, amount, fee, amount+fee, req.Currency, req.Narration, "FAILED_ACCOUNT_NOT_ACTIVE", locationJSON, metadataJSON)
		tx.Commit()
		ts.audit.LogError(txID, req.FromAccount, errors.New("account not active"))
		http.Error(w, "Source account not active", http.StatusForbidden)
		return
	}

	amount := int64(req.Amount)
	fee := ts.calculateFee(amount)
	totalAmount := amount + fee

	if balance < totalAmount {
		log.Printf("[EXTERNAL_TRANSFER] Insufficient balance: %d < %d", balance, totalAmount)
		var locationJSON []byte
		if req.Location != nil {
			locationJSON, _ = json.Marshal(req.Location)
		}
		metadata := map[string]interface{}{"ip_address": ipAddress}
		metadataJSON, _ := json.Marshal(metadata)
		_, _ = tx.Exec(`
			INSERT INTO transactions 
			(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
		`, txID, req.FromAccount, req.ToAccount, amount, fee, totalAmount, req.Currency, req.Narration, "FAILED_INSUFFICIENT_BALANCE", locationJSON, metadataJSON)
		tx.Commit()
		ts.audit.LogError(txID, req.FromAccount, errors.New("insufficient balance"))
		http.Error(w, "Insufficient balance", http.StatusBadRequest)
		return
	}

	// Debit source account
	log.Printf("[EXTERNAL_TRANSFER] Debiting source account: %s, amount: %d, fee: %d, total: %d", req.FromAccount, amount, fee, totalAmount)
	result, err := tx.Exec(`
		UPDATE accounts 
		SET balance = balance - $1, updated_at = NOW() 
		WHERE (account_id = $2 OR card_id = $2) AND balance >= $1
	`, totalAmount, req.FromAccount)

	if err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Failed to debit account: %v", err)
		var locationJSON []byte
		if req.Location != nil {
			locationJSON, _ = json.Marshal(req.Location)
		}
		metadata := map[string]interface{}{"ip_address": ipAddress}
		metadataJSON, _ := json.Marshal(metadata)
		_, _ = tx.Exec(`
			INSERT INTO transactions 
			(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
		`, txID, req.FromAccount, req.ToAccount, amount, fee, totalAmount, req.Currency, req.Narration, "FAILED_DEBIT_ERROR", locationJSON, metadataJSON)
		tx.Commit()
		ts.audit.LogError(txID, req.FromAccount, err)
		http.Error(w, "Failed to process transfer", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		log.Printf("[EXTERNAL_TRANSFER] No rows affected, insufficient balance")
		var locationJSON []byte
		if req.Location != nil {
			locationJSON, _ = json.Marshal(req.Location)
		}
		metadata := map[string]interface{}{"ip_address": ipAddress}
		metadataJSON, _ := json.Marshal(metadata)
		_, _ = tx.Exec(`
			INSERT INTO transactions 
			(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
		`, txID, req.FromAccount, req.ToAccount, amount, fee, totalAmount, req.Currency, req.Narration, "FAILED_INSUFFICIENT_BALANCE", locationJSON, metadataJSON)
		tx.Commit()
		ts.audit.LogError(txID, req.FromAccount, errors.New("insufficient balance"))
		http.Error(w, "Insufficient balance", http.StatusBadRequest)
		return
	}

	// Store transaction
	log.Printf("[EXTERNAL_TRANSFER] Storing transaction record")
	var locationJSON []byte
	if req.Location != nil {
		locationJSON, _ = json.Marshal(req.Location)
	}
	metadata := map[string]interface{}{"ip_address": ipAddress}
	metadataJSON, _ := json.Marshal(metadata)
	_, err = tx.Exec(`
		INSERT INTO transactions 
		(transaction_id, from_card_id, to_card_id, amount, fee, total_amount, currency, narration, type, status, location, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'transfer', $9, $10, $11, NOW())
	`, txID, req.FromAccount, req.ToAccount, amount, fee, totalAmount, req.Currency, req.Narration, "PENDING", locationJSON, metadataJSON)

	if err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Failed to store transaction: %v", err)
		ts.audit.LogError(txID, req.FromAccount, err)
		http.Error(w, "Failed to store transaction", http.StatusInternalServerError)
		return
	}

	// Credit fee to system account
	if fee > 0 {
		systemFeeAccount := ts.ledger.systemFeeAccount
		_, err = tx.Exec(`
			UPDATE accounts 
			SET balance = balance + $1, updated_at = NOW() 
			WHERE account_id = $2
		`, fee, systemFeeAccount)
		if err != nil {
			log.Printf("[EXTERNAL_TRANSFER] Failed to credit fee to system account: %v", err)
			ts.audit.LogError(txID, req.FromAccount, err)
			http.Error(w, "Failed to process transfer", http.StatusInternalServerError)
			return
		}
		log.Printf("[EXTERNAL_TRANSFER] Credited fee %d to system account %s", fee, systemFeeAccount)
		ts.audit.LogOperation(txID, systemFeeAccount, "FEE_CREDIT", fmt.Sprintf("Fee credited: %d", fee))
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Failed to commit transaction: %v", err)
		ts.audit.LogError(txID, req.FromAccount, err)
		http.Error(w, "Failed to process transfer", http.StatusInternalServerError)
		return
	}

	// Create ISO 20022 transaction (after DB commit)
	modelTx := &models.Transaction{
		TransactionID: txID,
		ReferenceID:   req.Reference,
		FromCardID:    req.FromAccount,
		ToCardID:      req.ToAccount,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        "PENDING",
		ToBankCode:    req.ToBankCode,
	}

	log.Printf("[EXTERNAL_TRANSFER] Converting to ISO 20022 format")
	iso20022Service := NewISO20022Service()
	doc, err := iso20022Service.ConvertTransaction(modelTx)
	if err != nil {
		log.Printf("[EXTERNAL_TRANSFER] ISO conversion failed: %v", err)
		_, _ = ts.db.Exec(`UPDATE transactions SET status = $1 WHERE transaction_id = $2`, "FAILED_ISO_CONVERSION", txID)
		ts.audit.LogError(txID, req.FromAccount, err)
		http.Error(w, "Failed to create transfer message", http.StatusInternalServerError)
		return
	}

	// Send to external settlement
	log.Printf("[EXTERNAL_TRANSFER] Sending to settlement system")
	ts.audit.LogOperation(txID, req.FromAccount, "ISO20022_SEND", fmt.Sprintf("Sending to bank: %s", req.ToBankCode))
	if err := iso20022Service.SendToSettlement(doc); err != nil {
		log.Printf("[EXTERNAL_TRANSFER] Settlement send failed: %v", err)
		_, _ = ts.db.Exec(`UPDATE transactions SET status = $1 WHERE transaction_id = $2`, "FAILED_SETTLEMENT_ERROR", txID)
		ts.audit.LogError(txID, req.FromAccount, err)
		http.Error(w, "Failed to send to settlement", http.StatusInternalServerError)
		return
	}

	ts.audit.LogTransfer(txID, req.FromAccount, req.ToAccount, amount, "PENDING")
	log.Printf("[EXTERNAL_TRANSFER] Transfer successful: %s", txID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"transactionId": txID,
		"status":        "PENDING",
	})
}
