package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/ruralpay/backend/docs"
	"github.com/ruralpay/backend/internal/database"
	"github.com/ruralpay/backend/internal/handlers"
	"github.com/ruralpay/backend/internal/hsm"
	mW "github.com/ruralpay/backend/internal/middleware"
	"github.com/ruralpay/backend/internal/services"
	"github.com/spf13/viper"
	httpSwagger "github.com/swaggo/http-swagger"
)

// @title NFC Payments Backend API
// @version 1.0
// @description API for NFC-based payment processing system
// @host localhost:8080
// @BasePath /api/v1
// @schemes http https

// Global HSM instance
var hsmInstance hsm.HSMInterface

func main() {
	// Initialize config
	viper.SetConfigFile(".env") // explicitly point to .env file
	viper.AutomaticEnv()        // allow environment variables to override .env
	viper.ReadInConfig()        // read .env file

	// Set environment variable prefix
	viper.SetEnvPrefix("")

	viper.BindEnv("database.host", "DATABASE_HOST")
	viper.BindEnv("database.port", "DATABASE_PORT")
	viper.BindEnv("database.user", "DATABASE_USER")
	viper.BindEnv("database.password", "DATABASE_PASSWORD")
	viper.BindEnv("database.name", "DATABASE_NAME")
	viper.BindEnv("database.ssl_mode", "DATABASE_SSL_MODE")

	viper.BindEnv("redis.host", "REDIS_HOST")
	viper.BindEnv("redis.port", "REDIS_PORT")
	viper.BindEnv("redis.password", "REDIS_PASSWORD")
	viper.BindEnv("redis.db", "REDIS_DB")

	viper.BindEnv("hsm.master_key", "HSM_MASTER_KEY")
	viper.BindEnv("hsm.salt", "HSM_SALT")
	viper.BindEnv("hsm.key_store_path", "HSM_KEY_STORE_PATH")
	viper.BindEnv("jwt.secret_key", "JWT_SECRET_KEY")
	viper.BindEnv("jwt.expiry_hours", "JWT_EXPIRY_HOURS")
	viper.BindEnv("argon2.time", "ARGON2_TIME")
	viper.BindEnv("argon2.memory", "ARGON2_MEMORY")
	viper.BindEnv("argon2.threads", "ARGON2_THREADS")
	viper.BindEnv("argon2.key_length", "ARGON2_KEY_LENGTH")
	viper.BindEnv("argon2.salt_length", "ARGON2_SALT_LENGTH")

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Config file not found, using defaults: %v", err)
	}

	// Initialize Swagger docs
	docs.SwaggerInfo.Title = "NFC Payments Backend API"
	docs.SwaggerInfo.Description = "API for NFC-based payment processing system"
	docs.SwaggerInfo.Version = "1.0"
	docs.SwaggerInfo.Host = "localhost:8080"
	docs.SwaggerInfo.BasePath = "/api/v1"
	docs.SwaggerInfo.Schemes = []string{"http", "https"}

	// Initialize services
	db := database.InitDatabase()
	defer db.Close()

	redisClient := database.InitRedis()
	if redisClient != nil {
		defer redisClient.Close()
	}

	hsm, err := hsm.InitHSM(hsm.Config{
		MasterKey:       viper.GetString("hsm.master_key"),
		KeyStorePath:    viper.GetString("hsm.key_store_path"),
		KeyRotationDays: viper.GetInt("hsm.key_rotation_days"),
		Salt:            []byte(viper.GetString("hsm.salt")),
	})
	if err != nil {
		log.Fatalf("Failed to initialize HSM: %v", err)
	}

	// Sync HSM keys to database
	hsmKeyService := services.NewHSMKeyService(db, hsm)
	if err := hsmKeyService.SyncKeysToDatabase(); err != nil {
		log.Printf("Warning: Failed to sync HSM keys to database: %v", err)
	} else {
		log.Println("HSM keys synced to database successfully")
	}
	defer func() {
		if logger, ok := hsmInstance.(interface{ Close() error }); ok {
			if err := logger.Close(); err != nil {
				log.Printf("Failed to close HSM logger: %v", err)
			}
		}
	}()

	transactionService := services.NewTransactionService(db, redisClient, hsm)
	provisioningService := services.NewCardProvisioningService(db, hsm)
	iso20022Service := services.NewISO20022Service()
	authService := services.NewAuthService(db, redisClient)
	ussdService := services.NewUSSDService(db, redisClient)
	ussdHandler := handlers.NewUSSDHandler(ussdService)
	qrService := services.NewQRService(db, redisClient)
	qrHandler := handlers.NewQRHandler(qrService)
	bankService := services.NewBankService()
	voiceService := services.NewVoiceBankingService()
	defer voiceService.Close()

	// Initialize auth middleware with Redis
	mW.InitAuthMiddleware(redisClient)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(mW.SecurityHeaders)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Access-Control-Allow-Origin"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	})

	// Swagger documentation
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("http://localhost:8080/swagger/doc.json"),
	))

	// Serve OpenAPI spec
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./api/openapi.yaml")
	})

	// Static file server for bank logos
	r.Handle("/static/bank-logos/*", http.StripPrefix("/static/bank-logos/",
		mW.StaticFileServer("./static/bank-logos")))

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public endpoints (no auth required)
		r.Post("/auth/register", authService.Register)
		r.Post("/auth/login", authService.Login)
		r.Post("/auth/logout", authService.Logout)
		r.Get("/banks", bankService.GetAllBanks)
		r.Post("/accounts/validate-bvn", authService.ValidateBVN)
		r.Post("/accounts/verify-otp", authService.VerifyOTP)

		// Protected endpoints (auth required)
		r.Group(func(r chi.Router) {
			r.Use(mW.AuthMiddleware)

			r.Get("/auth/account", authService.GetUserAccount)

			r.Get("/transactions", transactionService.ListTransactions)
			r.Get("/transactions/{txId}", transactionService.GetTransaction)
			r.Post("/transactions", transactionService.CreateTransaction)
			r.Post("/transactions/batch", transactionService.BatchTransactions)
			r.Post("/transactions/external", transactionService.ExternalBankTransfer)
			r.Get("/transactions/recent", transactionService.GetRecentTransactions)

			// User account endpoint

			// Account enquiry endpoints (supports accountId query parameter)
			r.Get("/accounts/name-enquiry", transactionService.AccountNameEnquiry)
			r.Get("/accounts/balance-enquiry", transactionService.AccountBalanceEnquiry)

			// Card provisioning endpoints
			r.Post("/cards/provision", provisioningService.ProvisionCard)
			r.Post("/cards/activate", provisioningService.ActivateCard)
			r.Get("/cards/{cardId}", provisioningService.GetCard)
			r.Put("/cards/{cardId}/suspend", provisioningService.SuspendCard)
			r.Put("/cards/{cardId}/reinstate", provisioningService.ReinstateCard)

			// ISO 20022 endpoints
			r.Post("/iso20022/convert", iso20022Service.ConvertToISO20022)
			r.Post("/iso20022/settlement", iso20022Service.ProcessSettlement)

			// USSD endpoints
			r.Post("/ussd/generate", ussdHandler.GenerateCode)
			r.Post("/ussd/validate", ussdHandler.ValidateCode)
			r.Get("/ussd/codes", ussdHandler.GetUserCodes)

			// QR endpoints
			r.Post("/qr/generate", qrHandler.GenerateQR)
			r.Post("/qr/process", qrHandler.ProcessQR)

			// Voice banking endpoints
			r.Post("/transactions/voice-transcribe", voiceService.TranscribeAudio)
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Start server
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		log.Println("Server starting on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Server shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server stopped")
}
