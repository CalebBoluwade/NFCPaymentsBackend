package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/skip2/go-qrcode"
)

type QRService struct {
	db    *sql.DB
	redis *redis.Client
}

func NewQRService(db *sql.DB, redis *redis.Client) *QRService {
	return &QRService{
		db:    db,
		redis: redis,
	}
}

func (s *QRService) GenerateQRCode(ctx context.Context, userID string, amount int64) (string, string, error) {
	qrData := map[string]any{
		"userId":    userID,
		"amount":    amount,
		"timestamp": time.Now().Unix(),
		"nonce":     s.generateNonce(),
	}

	jsonData, err := json.Marshal(qrData)
	if err != nil {
		return "", "", err
	}

	qrCode := base64.URLEncoding.EncodeToString(jsonData)

	key := fmt.Sprintf("qr:%s", qrCode)
	if err := s.redis.Set(ctx, key, jsonData, 5*time.Minute).Err(); err != nil {
		return "", "", err
	}

	qr, err := qrcode.New(qrCode, qrcode.Medium)
	if err != nil {
		return "", "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, qr.Image(256)); err != nil {
		return "", "", err
	}

	qrImage := base64.StdEncoding.EncodeToString(buf.Bytes())

	return qrCode, qrImage, nil
}

func (s *QRService) ProcessQRCode(ctx context.Context, qrData string) (map[string]any, error) {
	key := fmt.Sprintf("qr:%s", qrData)

	data, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("invalid or expired QR code")
	}
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	s.redis.Del(ctx, key)

	return result, nil
}

func (s *QRService) generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}
