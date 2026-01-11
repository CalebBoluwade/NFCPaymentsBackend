package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

type TestStruct struct {
	Name  string `validate:"required,min=2"`
	Email string `validate:"required,email"`
	Age   int    `validate:"required,gte=18"`
}

func TestValidationHelper_ValidateStruct(t *testing.T) {
	vh := NewValidationHelper()

	t.Run("valid struct", func(t *testing.T) {
		valid := TestStruct{
			Name:  "John Doe",
			Email: "john@example.com",
			Age:   25,
		}

		err := vh.ValidateStruct(&valid)
		assert.NoError(t, err)
	})

	t.Run("invalid struct - missing required fields", func(t *testing.T) {
		invalid := TestStruct{
			Name: "J", // Too short
			// Email missing
			Age: 16, // Too young
		}

		err := vh.ValidateStruct(&invalid)
		assert.Error(t, err)

		validationErrors, ok := err.(validator.ValidationErrors)
		assert.True(t, ok)
		assert.Len(t, validationErrors, 3) // Name, Email, Age errors
	})

	t.Run("invalid email format", func(t *testing.T) {
		invalid := TestStruct{
			Name:  "John Doe",
			Email: "invalid-email",
			Age:   25,
		}

		err := vh.ValidateStruct(&invalid)
		assert.Error(t, err)

		validationErrors, ok := err.(validator.ValidationErrors)
		assert.True(t, ok)
		assert.Len(t, validationErrors, 1)
		assert.Equal(t, "Email", validationErrors[0].Field())
		assert.Equal(t, "email", validationErrors[0].Tag())
	})
}

func TestSendErrorResponse(t *testing.T) {
	t.Run("error response without validation errors", func(t *testing.T) {
		w := httptest.NewRecorder()
		
		SendErrorResponse(w, "Something went wrong", http.StatusInternalServerError, nil)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Something went wrong", response.Error)
		assert.Nil(t, response.Details)
	})

	t.Run("error response with validation errors", func(t *testing.T) {
		vh := NewValidationHelper()
		invalid := TestStruct{
			Name:  "J",
			Email: "invalid-email",
			Age:   16,
		}

		validationErr := vh.ValidateStruct(&invalid)
		assert.Error(t, validationErr)

		w := httptest.NewRecorder()
		SendErrorResponse(w, "Validation failed", http.StatusBadRequest, validationErr)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Validation failed", response.Error)
		assert.NotNil(t, response.Details)
		assert.Contains(t, response.Details, "Name")
		assert.Contains(t, response.Details, "Email")
		assert.Contains(t, response.Details, "Age")
	})

	t.Run("bad request error", func(t *testing.T) {
		w := httptest.NewRecorder()
		
		SendErrorResponse(w, "Invalid request", http.StatusBadRequest, nil)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Invalid request", response.Error)
	})

	t.Run("unauthorized error", func(t *testing.T) {
		w := httptest.NewRecorder()
		
		SendErrorResponse(w, "Unauthorized access", http.StatusUnauthorized, nil)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		
		var response ErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "Unauthorized access", response.Error)
	})
}

func TestNewValidationHelper(t *testing.T) {
	vh := NewValidationHelper()
	assert.NotNil(t, vh)
	assert.NotNil(t, vh.validator)
}

func TestErrorResponse_Structure(t *testing.T) {
	t.Run("error response structure", func(t *testing.T) {
		errorResp := ErrorResponse{
			Error: "Test error",
			Details: map[string]string{
				"field1": "validation error 1",
				"field2": "validation error 2",
			},
		}

		jsonData, err := json.Marshal(errorResp)
		assert.NoError(t, err)

		var unmarshaled ErrorResponse
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, "Test error", unmarshaled.Error)
		assert.Equal(t, "validation error 1", unmarshaled.Details["field1"])
		assert.Equal(t, "validation error 2", unmarshaled.Details["field2"])
	})

	t.Run("error response without details", func(t *testing.T) {
		errorResp := ErrorResponse{
			Error: "Simple error",
		}

		jsonData, err := json.Marshal(errorResp)
		assert.NoError(t, err)

		var unmarshaled ErrorResponse
		err = json.Unmarshal(jsonData, &unmarshaled)
		assert.NoError(t, err)
		assert.Equal(t, "Simple error", unmarshaled.Error)
		assert.Nil(t, unmarshaled.Details)
	})
}