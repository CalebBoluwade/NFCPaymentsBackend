package services

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	"golang.org/x/crypto/argon2"
)

type AuthService struct {
	db        *sql.DB
	redis     *redis.Client
	validator *validator.Validate
}

// LoginRequest represents the login request payload
// @Description Login request structure
type LoginRequest struct {
	PhoneNumber string `json:"phoneNumber" validate:"required" example:"+2348012345678"` // User phone number
	Password    string `json:"password" validate:"required,min=6" example:"password123"` // User password
}

// RegisterRequest represents the registration request payload
// @Description Registration request structure
type RegisterRequest struct {
	Email       string `json:"Email" validate:"required,email" example:"user@example.com"` // User email address
	Password    string `json:"Password" validate:"required,min=6" example:"password123"`   // User password
	FirstName   string `json:"FirstName" validate:"required,min=2" example:"John"`         // User first name
	LastName    string `json:"LastName" validate:"required,min=2" example:"Doe"`           // User last name
	BVN         string `json:"BVN" validate:"required,len=11" example:"12345678901"`       // Bank Verification Number
	PhoneNumber string `json:"PhoneNumber" validate:"required" example:"+2348012345678"`   // Phone number
}

// AuthResponse represents the authentication response
// @Description Authentication response structure
type AuthResponse struct {
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."` // JWT token
	User  User   `json:"user"`                                                    // User information
}

// User represents user information
// @Description User structure
type User struct {
	ID          int    `json:"id" example:"1"`                       // User ID
	Email       string `json:"email" example:"user@example.com"`     // User email
	FirstName   string `json:"FirstName" example:"John"`             // User first name
	LastName    string `json:"LastName" example:"Doe"`               // User last name
	AccountId   string `json:"AccountId" example:"1234567890"`       // User account ID`
	PhoneNumber string `json:"PhoneNumber" example:"+2348012345678"` // User phone number
	BVN         string `json:"BVN" example:"12345678901"`            // User BVN
	DeviceID    string `json:"device_id"`
}

func NewAuthService(db *sql.DB, redisClient *redis.Client) *AuthService {
	return &AuthService{
		db:        db,
		redis:     redisClient,
		validator: validator.New(),
	}
}

func (s *AuthService) sendErrorResponse(w http.ResponseWriter, message string, statusCode int, validationErr error) {
	SendErrorResponse(w, message, statusCode, validationErr)
}

// Register handles user registration
// @Summary Register a new user
// @Description Register a new user with email, password, and name
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration request"
// @Success 200 {object} AuthResponse "Registration successful"
// @Failure 400 {string} string "Invalid request"
// @Failure 409 {string} string "Email already exists"
// @Failure 500 {string} string "Internal server error"
// @Router /auth/register [post]
func (s *AuthService) Register(w http.ResponseWriter, r *http.Request) {
	log.Printf("[AUTH] Registration attempt from IP: %s", r.RemoteAddr)

	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req RegisterRequest
	if err := dec.Decode(&req); err != nil {
		log.Printf("[AUTH] Registration failed - invalid request: %v", err)
		s.sendErrorResponse(w, "Invalid request", http.StatusBadRequest, nil)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		log.Printf("[AUTH] Multiple JSON objects detected")
		s.sendErrorResponse(w, "Request body must only contain a single JSON object", http.StatusBadRequest, nil)
		return
	}

	if err := s.validator.Struct(&req); err != nil {
		log.Printf("[AUTH] Registration validation failed: %v", err)
		s.sendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	log.Printf("[AUTH] Registration request for email: %s", req.Email)

	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		log.Printf("[AUTH] Password hashing failed for %s: %v", req.Email, err)
		s.sendErrorResponse(w, "An Internal Error Occurred", http.StatusInternalServerError, nil)
		return
	}

	// Generate 10-digit account ID
	accountID := generateAccountID()

	// Start transaction
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("[AUTH] Transaction start failed for %s: %v", req.Email, err)
		s.sendErrorResponse(w, "Failed to create user", http.StatusInternalServerError, nil)
		return
	}
	defer tx.Rollback()

	// Insert user with account_id
	var userID int
	err = tx.QueryRow("INSERT INTO users (email, password, first_name, last_name, account_id, bvn, phone_number) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id",
		strings.ToLower(req.Email), hashedPassword, req.FirstName, req.LastName, accountID, req.BVN, req.PhoneNumber).Scan(&userID)
	if err != nil {
		log.Printf("[AUTH] User creation failed for %s: %v", req.Email, err)
		s.sendErrorResponse(w, "Email Already Exists", http.StatusConflict, nil)
		return
	}

	// Create account record
	accountName := fmt.Sprintf("%s %s", req.FirstName, req.LastName)
	_, err = tx.Exec("INSERT INTO accounts (account_name, account_id, balance, version, updated_at) VALUES ($1, $2, $3, $4, NOW())",
		accountName, accountID, 0, 1)
	if err != nil {
		log.Printf("[AUTH] Account creation failed for %s: %v", req.Email, err)
		s.sendErrorResponse(w, "Failed to create account", http.StatusInternalServerError, nil)
		return
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		log.Printf("[AUTH] Transaction commit failed for %s: %v", req.Email, err)
		s.sendErrorResponse(w, "Failed to create user", http.StatusInternalServerError, nil)
		return
	}

	log.Printf("[AUTH] User created successfully - ID: %d, Email: %s", userID, req.Email)

	token, err := generateJWT(userID)
	if err != nil {
		log.Printf("[AUTH] JWT generation failed for user %d: %v", userID, err)
		s.sendErrorResponse(w, "Failed to generate token", http.StatusInternalServerError, nil)
		return
	}

	response := AuthResponse{
		Token: token,
		User:  User{ID: userID, Email: req.Email, FirstName: req.FirstName, LastName: req.LastName, AccountId: accountID},
	}

	log.Printf("[AUTH] Registration successful for user %d", userID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Login handles user authentication
// @Summary Login user
// @Description Authenticate user with email and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login request"
// @Success 200 {object} AuthResponse "Login successful"
// @Failure 400 {string} string "Invalid request"
// @Failure 401 {string} string "Invalid credentials"
// @Failure 500 {string} string "Internal server error"
// @Router /auth/login [post]
func (s *AuthService) Login(w http.ResponseWriter, r *http.Request) {
	log.Printf("[AUTH] Login attempt from IP: %s", r.RemoteAddr)

	maxBytes := 1_048_576 // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req LoginRequest
	if err := dec.Decode(&req); err != nil {
		log.Printf("[AUTH] Login failed - invalid request: %v", err)
		s.sendErrorResponse(w, "Invalid request", http.StatusBadRequest, nil)
		return
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		log.Printf("[AUTH] Multiple JSON objects detected")
		s.sendErrorResponse(w, "Request body must only contain a single JSON object", http.StatusBadRequest, nil)
		return
	}

	if err := s.validator.Struct(&req); err != nil {
		log.Printf("[AUTH] Login validation failed: %v", err)
		s.sendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	log.Printf("[AUTH] Login request for phone number: %s", req.PhoneNumber)

	var user User
	var hashedPassword string
	err := s.db.QueryRow("SELECT id, email, first_name, last_name, password, account_id FROM users WHERE phone_number = $1",
		req.PhoneNumber).Scan(&user.ID, &user.Email, &user.FirstName, &user.LastName, &hashedPassword, &user.AccountId)
	if err != nil {
		log.Printf("[AUTH] User not found for phone number: %s", req.PhoneNumber)
		s.sendErrorResponse(w, "Invalid credentials", http.StatusUnauthorized, nil)
		return
	}

	if !verifyPassword(req.Password, hashedPassword) {
		log.Printf("[AUTH] Invalid password for user: %s", req.PhoneNumber)
		s.sendErrorResponse(w, "Invalid credentials", http.StatusUnauthorized, nil)
		return
	}

	log.Printf("[AUTH] Password verified for user ID: %d", user.ID)

	token, err := generateJWT(user.ID)
	if err != nil {
		log.Printf("[AUTH] JWT generation failed for user %d: %v", user.ID, err)
		s.sendErrorResponse(w, "Failed to generate token", http.StatusInternalServerError, nil)
		return
	}

	response := AuthResponse{
		Token: token,
		User:  user,
	}

	log.Printf("[AUTH] Login successful for user %d", user.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Logout handles user logout
// @Summary Logout user
// @Description Logout user and blacklist token
// @Tags auth
// @Produce json
// @Success 200 {object} map[string]string "Logout successful"
// @Router /auth/logout [post]
func (s *AuthService) Logout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if token != "" && len(token) > 7 {
		token = token[7:] // Remove "Bearer " prefix

		if s.redis != nil {
			ctx := context.Background()
			key := fmt.Sprintf("blacklist:%s", token)
			// Blacklist token until its expiration
			expiry := time.Duration(viper.GetInt("jwt.expiry_hours")) * time.Hour
			if err := s.redis.Set(ctx, key, "1", expiry).Err(); err != nil {
				log.Printf("[AUTH] Failed to blacklist token: %v", err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Logout successful"})
}

// ValidateBVN validates a BVN number and sends OTP
// @Summary Validate BVN
// @Description Validate a Bank Verification Number and send OTP
// @Tags accounts
// @Accept json
// @Produce json
// @Param request body map[string]string true "BVN validation request"
// @Success 200 {object} map[string]interface{} "OTP sent successfully"
// @Failure 400 {string} string "Invalid request"
// @Router /accounts/validate-bvn [post]
func (s *AuthService) ValidateBVN(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BVN         string `json:"bvn" validate:"required,len=11"`
		PhoneNumber string `json:"phoneNumber" validate:"required"`
		Email       string `json:"email" validate:"required,email"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendErrorResponse(w, "Invalid request", http.StatusBadRequest, nil)
		return
	}

	if err := s.validator.Struct(&req); err != nil {
		s.sendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	otp := generateOTP()
	key := fmt.Sprintf("bvn_otp:%s", req.BVN)

	if s.redis != nil {
		ctx := context.Background()
		if err := s.redis.Set(ctx, key, otp, 10*time.Minute).Err(); err != nil {
			log.Printf("[AUTH] Failed to store OTP in Redis: %v", err)
			s.sendErrorResponse(w, "Failed to generate OTP", http.StatusInternalServerError, nil)
			return
		}
	}

	log.Printf("[AUTH] OTP generated for BVN %s: %s (Phone: %s, Email: %s)", req.BVN, otp, req.PhoneNumber, req.Email)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "OTP Sent Successfully",
		"valid":   true,
	})
}

// VerifyOTP verifies the OTP for BVN validation
// @Summary Verify OTP
// @Description Verify OTP sent for BVN validation
// @Tags accounts
// @Accept json
// @Produce json
// @Param request body map[string]string true "OTP verification request"
// @Success 200 {object} map[string]interface{} "OTP verified successfully"
// @Failure 400 {string} string "Invalid request"
// @Failure 401 {string} string "Invalid or expired OTP"
// @Router /accounts/verify-otp [post]
func (s *AuthService) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BVN string `json:"bvn" validate:"required,len=11"`
		OTP string `json:"otp" validate:"required,len=8"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendErrorResponse(w, "Invalid request", http.StatusBadRequest, nil)
		return
	}

	if err := s.validator.Struct(&req); err != nil {
		s.sendErrorResponse(w, "Validation failed", http.StatusBadRequest, err)
		return
	}

	key := fmt.Sprintf("bvn_otp:%s", req.BVN)

	if s.redis != nil {
		ctx := context.Background()
		storedOTP, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			log.Printf("[AUTH] OTP not found or expired for BVN %s", req.BVN)
			s.sendErrorResponse(w, "Invalid or expired OTP", http.StatusUnauthorized, nil)
			return
		}

		if storedOTP != req.OTP {
			log.Printf("[AUTH] Invalid OTP for BVN %s", req.BVN)
			s.sendErrorResponse(w, "Invalid or expired OTP", http.StatusUnauthorized, nil)
			return
		}

		s.redis.Del(ctx, key)
	}

	log.Printf("[AUTH] OTP verified successfully for BVN %s", req.BVN)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "OTP Verified Successfully",
		"valid":   true,
	})
}

// GetUserAccount retrieves user account details from auth token
// @Summary Get user account details
// @Description Get authenticated user's account information
// @Tags auth
// @Produce json
// @Success 200 {object} User "User account details"
// @Failure 401 {string} string "Unauthorized"
// @Failure 500 {string} string "Internal server error"
// @Router /auth/account [get]
func (s *AuthService) GetUserAccount(w http.ResponseWriter, r *http.Request) {
	log.Printf("[AUTH] User account request from IP: %s", r.RemoteAddr)

	userID := r.Context().Value("userID")
	if userID == nil {
		log.Printf("[AUTH] Unauthorized account request - no user ID in context")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	log.Printf("[AUTH] Fetching account details for user ID: %v", userID)
	var user User
	err := s.db.QueryRow("SELECT users.id, email, first_name, last_name, phone_number, users.account_id FROM users LEFT JOIN accounts ON users.id = accounts.user_id WHERE users.id = $1",
		userID).Scan(&user.ID, &user.Email, &user.FirstName, &user.LastName, &user.PhoneNumber, &user.AccountId)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("[AUTH] User not found for ID: %v", userID)
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			log.Printf("[AUTH] Failed to fetch user details for ID %v: %v", userID, err)
			http.Error(w, "Failed to fetch user details", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("[AUTH] Successfully fetched account details for user: %s (ID: %d)", user.Email, user.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func generateJWT(userID int) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"nameid":  userID,
		"exp":     time.Now().Add(time.Duration(viper.GetInt("jwt.expiry_hours")) * time.Hour).Unix(),
	})

	return token.SignedString([]byte(viper.GetString("jwt.secret_key")))
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, viper.GetInt("argon2.salt_length"))
	if _, err := cryptorand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt,
		uint32(viper.GetInt("argon2.time")),
		uint32(viper.GetInt("argon2.memory")),
		uint8(viper.GetInt("argon2.threads")),
		uint32(viper.GetInt("argon2.key_length")))
	return fmt.Sprintf("%s$%s", base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(password, hashedPassword string) bool {
	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 2 {
		return false
	}

	salt, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}

	hash, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}

	computedHash := argon2.IDKey([]byte(password), salt,
		uint32(viper.GetInt("argon2.time")),
		uint32(viper.GetInt("argon2.memory")),
		uint8(viper.GetInt("argon2.threads")),
		uint32(viper.GetInt("argon2.key_length")))
	return string(hash) == string(computedHash)
}

func generateAccountID() string {
	const digits = "0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = digits[rand.Intn(len(digits))]
	}
	return string(b)
}

func generateOTP() string {
	b := make([]byte, 4)
	cryptorand.Read(b)
	return fmt.Sprintf("%08d", (int(b[0])<<24|int(b[1])<<16|int(b[2])<<8|int(b[3]))%100000000)
}
