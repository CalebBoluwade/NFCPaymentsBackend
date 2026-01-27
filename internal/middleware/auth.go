package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
)

var redisClient *redis.Client

func InitAuthMiddleware(redis *redis.Client) {
	redisClient = redis
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get token from Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		// Extract token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := parts[1]

		// Check if token is blacklisted
		if redisClient != nil {
			ctx := context.Background()
			key := fmt.Sprintf("blacklist:%s", token)
			if exists, _ := redisClient.Exists(ctx, key).Result(); exists > 0 {
				http.Error(w, "Token has been revoked", http.StatusUnauthorized)
				return
			}
		}

		// Validate token (implement your JWT validation here)
		userID, err := validateToken(token)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Add user ID to context
		ctx := context.WithValue(r.Context(), "userID", userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(viper.GetString("jwt.secret_key")), nil
	})

	if err != nil || !token.Valid {
		return "", err
	}

	userID := claims["user_id"]
	return fmt.Sprintf("%v", userID), nil
}
