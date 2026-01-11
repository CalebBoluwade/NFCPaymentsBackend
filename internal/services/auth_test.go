package services

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestAuthService_Register(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	// Setup viper config
	viper.Set("argon2.salt_length", 16)
	viper.Set("argon2.time", 1)
	viper.Set("argon2.memory", 64*1024)
	viper.Set("argon2.threads", 4)
	viper.Set("argon2.key_length", 32)
	viper.Set("jwt.secret_key", "test-secret")
	viper.Set("jwt.expiry_hours", 24)

	service := NewAuthService(db)

	t.Run("successful registration", func(t *testing.T) {
		req := RegisterRequest{
			Email:     "test@example.com",
			Password:  "password123",
			FirstName: "John",
			LastName:  "Doe",
		}

		mock.ExpectQuery("INSERT INTO users").
			WithArgs(req.Email, sqlmock.AnyArg(), req.FirstName, req.LastName).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

		body, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/auth/register", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		service.Register(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		var response AuthResponse
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.NotEmpty(t, response.Token)
		assert.Equal(t, req.Email, response.User.Email)
	})

	t.Run("invalid request body", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/auth/register", bytes.NewBuffer([]byte("invalid")))
		w := httptest.NewRecorder()

		service.Register(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestAuthService_Login(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	viper.Set("jwt.secret_key", "test-secret")
	viper.Set("jwt.expiry_hours", 24)

	service := NewAuthService(db)

	t.Run("successful login", func(t *testing.T) {
		hashedPassword, _ := hashPassword("password123")
		
		mock.ExpectQuery("SELECT id, email, first_name, last_name, password FROM users").
			WithArgs("test@example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "email", "first_name", "last_name", "password"}).
				AddRow(1, "test@example.com", "John", "Doe", hashedPassword))

		req := LoginRequest{
			Email:    "test@example.com",
			Password: "password123",
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/auth/login", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		service.Login(w, r)

		assert.Equal(t, http.StatusOK, w.Code)
		var response AuthResponse
		json.Unmarshal(w.Body.Bytes(), &response)
		assert.NotEmpty(t, response.Token)
	})

	t.Run("user not found", func(t *testing.T) {
		mock.ExpectQuery("SELECT id, email, first_name, last_name, password FROM users").
			WithArgs("nonexistent@example.com").
			WillReturnError(sql.ErrNoRows)

		req := LoginRequest{
			Email:    "nonexistent@example.com",
			Password: "password123",
		}

		body, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/auth/login", bytes.NewBuffer(body))
		w := httptest.NewRecorder()

		service.Login(w, r)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestPasswordHashing(t *testing.T) {
	viper.Set("argon2.salt_length", 16)
	viper.Set("argon2.time", 1)
	viper.Set("argon2.memory", 64*1024)
	viper.Set("argon2.threads", 4)
	viper.Set("argon2.key_length", 32)

	password := "testpassword"
	
	hashed, err := hashPassword(password)
	assert.NoError(t, err)
	assert.NotEmpty(t, hashed)
	
	assert.True(t, verifyPassword(password, hashed))
	assert.False(t, verifyPassword("wrongpassword", hashed))
}

func TestGenerateJWT(t *testing.T) {
	viper.Set("jwt.secret_key", "test-secret")
	viper.Set("jwt.expiry_hours", 24)

	token, err := generateJWT(123)
	assert.NoError(t, err)
	assert.NotEmpty(t, token)
}