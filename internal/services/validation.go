package services

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// ErrorResponse represents error response structure
type ErrorResponse struct {
	Error   string            `json:"error"`             // Error message
	Details map[string]string `json:"details,omitempty"` // Validation details
}

// ValidationHelper provides shared validation functionality
type ValidationHelper struct {
	validator *validator.Validate
}

// NewValidationHelper creates a new validation helper
func NewValidationHelper() *ValidationHelper {
	return &ValidationHelper{
		validator: validator.New(),
	}
}

// ValidateStruct validates a struct and returns validation errors
func (vh *ValidationHelper) ValidateStruct(s any) error {
	return vh.validator.Struct(s)
}

// SendErrorResponse sends a JSON error response
func SendErrorResponse(w http.ResponseWriter, message string, statusCode int, validationErr error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorResp := ErrorResponse{Error: message}
	if validationErr != nil {
		errorResp.Details = make(map[string]string)
		for _, err := range validationErr.(validator.ValidationErrors) {
			errorResp.Details[err.Field()] = fmt.Sprintf("Field Validation Failed on '%s' tag", err.Tag())
		}
	}

	json.NewEncoder(w).Encode(errorResp)
}
