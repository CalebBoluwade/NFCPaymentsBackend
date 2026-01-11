package services

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ruralpay/backend/internal/hsm"
)

type CardProvisioningService struct {
	db        *sql.DB
	hsm       hsm.HSMInterface
	validator *ValidationHelper
}

// ProvisionRequest represents card provisioning request
type ProvisionRequest struct {
	UserID         int     `json:"userId" validate:"required,gt=0"`
	CardType       string  `json:"cardType" validate:"required,oneof=DEBIT CREDIT PREPAID"`
	InitialBalance float64 `json:"initialBalance" validate:"gte=0"`
}

// ActivationRequest represents card activation request
type ActivationRequest struct {
	CardID         string `json:"cardId" validate:"required"`
	ActivationCode string `json:"activationCode" validate:"required,len=6"`
}

func NewCardProvisioningService(db *sql.DB, hsm hsm.HSMInterface) *CardProvisioningService {
	return &CardProvisioningService{
		db:        db,
		hsm:       hsm,
		validator: NewValidationHelper(),
	}
}

// ProvisionCard creates a new NFC card
// @Summary Provision a new card
// @Description Create and provision a new NFC payment card
// @Tags cards
// @Accept json
// @Produce json
// @Param card body object{userId=int,cardType=string,initialBalance=float64} true "Card provisioning data"
// @Success 200 {object} object{cardId=string,status=string}
// @Failure 400 {object} map[string]string
// @Router /cards/provision [post]
func (cps *CardProvisioningService) ProvisionCard(w http.ResponseWriter, r *http.Request) {
	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req ProvisionRequest
	if err := dec.Decode(&req); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest, nil)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		SendErrorResponse(w, "Request body must only contain a single JSON object", http.StatusBadRequest, nil)
		return
	}

	if err := cps.validator.ValidateStruct(&req); err != nil {
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "provisioned"})
}

// ActivateCard activates a provisioned card
// @Summary Activate card
// @Description Activate a provisioned NFC card
// @Tags cards
// @Accept json
// @Produce json
// @Param activation body object{cardId=string,activationCode=string} true "Card activation data"
// @Success 200 {object} object{cardId=string,status=string}
// @Failure 400 {object} map[string]string
// @Router /cards/activate [post]
func (cps *CardProvisioningService) ActivateCard(w http.ResponseWriter, r *http.Request) {
	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req ActivationRequest
	if err := dec.Decode(&req); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest, nil)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		SendErrorResponse(w, "Request body must only contain a single JSON object", http.StatusBadRequest, nil)
		return
	}

	if err := cps.validator.ValidateStruct(&req); err != nil {
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "activated"})
}

// GetCard retrieves card information
// @Summary Get card details
// @Description Retrieve information about a specific card
// @Tags cards
// @Produce json
// @Param cardId path string true "Card ID"
// @Success 200 {object} object{cardId=string,status=string,balance=float64}
// @Failure 404 {object} map[string]string
// @Router /cards/{cardId} [get]
func (cps *CardProvisioningService) GetCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardId")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"cardId": cardID, "status": "active"})
}

// SuspendCard suspends a card
// @Summary Suspend card
// @Description Suspend a card to prevent transactions
// @Tags cards
// @Produce json
// @Param cardId path string true "Card ID"
// @Success 200 {object} object{cardId=string,status=string}
// @Failure 404 {object} map[string]string
// @Router /cards/{cardId}/suspend [put]
func (cps *CardProvisioningService) SuspendCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardId")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"cardId": cardID, "status": "suspended"})
}

// ReinstateCard reactivates a suspended card
// @Summary Reinstate card
// @Description Reactivate a suspended card
// @Tags cards
// @Produce json
// @Param cardId path string true "Card ID"
// @Success 200 {object} object{cardId=string,status=string}
// @Failure 404 {object} map[string]string
// @Router /cards/{cardId}/reinstate [put]
func (cps *CardProvisioningService) ReinstateCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardId")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"cardId": cardID, "status": "active"})
}