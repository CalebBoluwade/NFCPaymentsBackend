package models

import "time"

type User struct {
	ID                  int    `json:"id" example:"1"`                       // User ID
	Email               string `json:"email" example:"user@example.com"`     // User email
	FirstName           string `json:"FirstName" example:"John"`             // User first name
	LastName            string `json:"LastName" example:"Doe"`               // User last name
	AccountId           string `json:"AccountId" example:"1234567890"`       // User account ID`
	PhoneNumber         string `json:"PhoneNumber" example:"+2348012345678"` // User phone number
	BVN                 string `json:"BVN" example:"12345678901"`            // User BVN
	DeviceID            string `json:"device_id"`
	Role                string `gorm:"default:'user'"`
	BiometricEnabled    bool   `gorm:"default:false"`
	VoiceBankingEnabled bool   `gorm:"default:false"`
	FailedLoginAttempts int    `gorm:"default:0"`
	LockedUntil         *time.Time
	LastLogin           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
