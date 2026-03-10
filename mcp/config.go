package mcp

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"nofx/logger"
)

// Config client configuration (centralized management of all configurations)
type Config struct {
	// Provider configuration
	Provider string
	APIKey   string
	BaseURL  string
	Model    string

	// Behavior configuration
	MaxTokens        int
	Temperature      float64
	TopP             float64 // Nucleus sampling (0-1). Role-based: analyst 0.95, trader 0.85, compliance 0.1
	PresencePenalty  float64 // Presence penalty (-2 to 2). Role-based for diversity vs determinism
	FrequencyPenalty float64 // Frequency penalty (-2 to 2). Usually 0 to allow domain terms
	UseFullURL       bool

	// Retry configuration
	MaxRetries     int
	RetryWaitBase  time.Duration
	RetryableErrors []string

	// Timeout configuration
	Timeout time.Duration

	// Dependency injection
	Logger     Logger
	HTTPClient *http.Client
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		// Default values
		MaxTokens:      getEnvInt("AI_MAX_TOKENS", 2000),
		Temperature:    MCPClientTemperature,
		MaxRetries:     MaxRetryTimes,
		RetryWaitBase:  2 * time.Second,
		Timeout:        DefaultTimeout,
		RetryableErrors: retryableErrors,

		// Default dependencies (use global logger)
		Logger:     logger.NewMCPLogger(),
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// getEnvInt reads integer from environment variable, returns default value if failed
func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}

// getEnvString reads string from environment variable, returns default value if empty
func getEnvString(key string, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
