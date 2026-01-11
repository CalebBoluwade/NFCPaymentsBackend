package config

import (
	"os"
	"strconv"
	"time"
)

type USSDConfig struct {
	CodeLength           int
	CodeTimeout          time.Duration
	MaxGenerationPerUser int
	RateLimitWindow      time.Duration
	PushCodePrefix       string
	PullCodePrefix       string
	HashIterations       int
	DialPrefix           string
	DialSuffix           string
}

func LoadUSSDConfig() *USSDConfig {
	return &USSDConfig{
		CodeLength:           getEnvAsInt("USSD_CODE_LENGTH", 8),
		CodeTimeout:          getEnvAsDuration("USSD_CODE_TIMEOUT", 5*time.Minute),
		MaxGenerationPerUser: getEnvAsInt("USSD_MAX_GEN_PER_USER", 5),
		RateLimitWindow:      getEnvAsDuration("USSD_RATE_LIMIT_WINDOW", 1*time.Hour),
		PushCodePrefix:       getEnv("USSD_PUSH_PREFIX", "PUSH"),
		PullCodePrefix:       getEnv("USSD_PULL_PREFIX", "PULL"),
		HashIterations:       getEnvAsInt("USSD_HASH_ITERATIONS", 10000),
		DialPrefix:           getEnv("USSD_DIAL_PREFIX", "*565*1*"),
		DialSuffix:           getEnv("USSD_DIAL_SUFFIX", "#"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvAsDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			return duration
		}
	}
	return defaultVal
}
