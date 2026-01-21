package config

import (
	"encoding/json"
	"nofx/experience"
	"nofx/mcp"
	"os"
	"strconv"
	"strings"
)

// Global configuration instance
var global *Config

// TelegramBotConfig 单个 Telegram Bot 配置
type TelegramBotConfig struct {
	Token      string `json:"token"`       // Bot Token
	ChatID     int64  `json:"chat_id"`     // Chat ID
	WebhookURL string `json:"webhook_url"` // Webhook URL (可选)
}

// Config is the global configuration (loaded from .env)
// Only contains truly global config, trading related config is at trader/strategy level
type Config struct {
	// Service configuration
	APIServerPort       int
	JWTSecret           string
	RegistrationEnabled bool
	MaxUsers            int // Maximum number of users allowed (0 = unlimited, default = 10)

	// Database configuration
	DBType     string // sqlite or postgres
	DBPath     string // SQLite database file path
	DBHost     string // PostgreSQL host
	DBPort     int    // PostgreSQL port
	DBUser     string // PostgreSQL user
	DBPassword string // PostgreSQL password
	DBName     string // PostgreSQL database name
	DBSSLMode  string // PostgreSQL SSL mode

	// Security configuration
	// TransportEncryption enables browser-side encryption for API keys
	// Requires HTTPS or localhost. Set to false for HTTP access via IP.
	TransportEncryption bool

	// Experience improvement (anonymous usage statistics)
	// Helps us understand product usage and improve the experience
	// Set EXPERIENCE_IMPROVEMENT=false to disable
	ExperienceImprovement bool

	// Market data provider API keys
	AlpacaAPIKey    string // Alpaca API key for US stocks
	AlpacaSecretKey string // Alpaca secret key
	TwelveDataKey   string // TwelveData API key for forex & metals

	// Telegram notification configuration
	TelegramEnabled  bool   // Whether Telegram notifications are enabled
	TelegramToken    string // Telegram Bot Token (deprecated, use TelegramBots instead)
	TelegramChatID   int64  // Telegram Chat ID (deprecated, use TelegramBots instead)
	TelegramWebhookURL string // Telegram Webhook URL (deprecated, use TelegramBots instead)
	
	// TelegramBots 多个 Telegram Bot 配置（JSON 格式）
	// 格式: [{"token":"xxx","chat_id":123,"webhook_url":"https://..."}]
	TelegramBots string // JSON array of bot configs
}

// Init initializes global configuration (from .env)
func Init() {
	cfg := &Config{
		APIServerPort:         8080,
		RegistrationEnabled:   true,
		MaxUsers:              10,   // Default: 10 users allowed
		ExperienceImprovement: true, // Default: enabled to help improve the product
		// Database defaults
		DBType:    "sqlite",
		DBPath:    "data/data.db",
		DBHost:    "localhost",
		DBPort:    5432,
		DBUser:    "postgres",
		DBName:    "nofx",
		DBSSLMode: "disable",
	}

	// Load from environment variables
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = strings.TrimSpace(v)
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "default-jwt-secret-change-in-production"
	}

	if v := os.Getenv("REGISTRATION_ENABLED"); v != "" {
		cfg.RegistrationEnabled = strings.ToLower(v) == "true"
	}

	if v := os.Getenv("MAX_USERS"); v != "" {
		if maxUsers, err := strconv.Atoi(v); err == nil && maxUsers >= 0 {
			cfg.MaxUsers = maxUsers
		}
	}

	if v := os.Getenv("API_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.APIServerPort = port
		}
	}

	// Transport encryption: default false for easier deployment
	// Set TRANSPORT_ENCRYPTION=true to enable (requires HTTPS or localhost)
	if v := os.Getenv("TRANSPORT_ENCRYPTION"); v != "" {
		cfg.TransportEncryption = strings.ToLower(v) == "true"
	}

	// Experience improvement: anonymous usage statistics
	// Default enabled, set EXPERIENCE_IMPROVEMENT=false to disable
	if v := os.Getenv("EXPERIENCE_IMPROVEMENT"); v != "" {
		cfg.ExperienceImprovement = strings.ToLower(v) != "false"
	}

	// Market data provider API keys
	cfg.AlpacaAPIKey = os.Getenv("ALPACA_API_KEY")
	cfg.AlpacaSecretKey = os.Getenv("ALPACA_SECRET_KEY")
	cfg.TwelveDataKey = os.Getenv("TWELVEDATA_API_KEY")

	// Database configuration
	if v := os.Getenv("DB_TYPE"); v != "" {
		cfg.DBType = strings.ToLower(v)
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.DBHost = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.DBPort = port
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.DBUser = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.DBPassword = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DBName = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		cfg.DBSSLMode = v
	}

	// Telegram notification configuration
	if v := os.Getenv("TELEGRAM_ENABLED"); v != "" {
		cfg.TelegramEnabled = strings.ToLower(v) == "true"
	}
	cfg.TelegramToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		if chatID, err := strconv.ParseInt(v, 10, 64); err == nil && chatID != 0 {
			cfg.TelegramChatID = chatID
		}
	}
	cfg.TelegramWebhookURL = os.Getenv("TELEGRAM_WEBHOOK_URL")
	
	// 解析多个 Telegram Bot 配置
	cfg.TelegramBots = os.Getenv("TELEGRAM_BOTS")

	global = cfg

	// Initialize experience improvement (installation ID will be set after database init)
	experience.Init(cfg.ExperienceImprovement, "")

	// Set up AI token usage tracking callback
	mcp.TokenUsageCallback = func(usage mcp.TokenUsage) {
		experience.TrackAIUsage(experience.AIUsageEvent{
			ModelProvider: usage.Provider,
			ModelName:     usage.Model,
			InputTokens:   usage.PromptTokens,
			OutputTokens:  usage.CompletionTokens,
		})
	}
}

// Get returns the global configuration
func Get() *Config {
	if global == nil {
		Init()
	}
	return global
}

// GetTelegramBotConfigs 解析并返回所有 Telegram Bot 配置
func GetTelegramBotConfigs() ([]TelegramBotConfig, error) {
	cfg := Get()
	
	// 如果配置了新的多 bot 格式，优先使用
	if cfg.TelegramBots != "" {
		var bots []TelegramBotConfig
		if err := json.Unmarshal([]byte(cfg.TelegramBots), &bots); err != nil {
			return nil, err
		}
		return bots, nil
	}
	
	// 向后兼容：如果配置了旧的单 bot 格式，转换为新格式
	if cfg.TelegramEnabled && cfg.TelegramToken != "" && cfg.TelegramChatID != 0 {
		return []TelegramBotConfig{
			{
				Token:      cfg.TelegramToken,
				ChatID:     cfg.TelegramChatID,
				WebhookURL: cfg.TelegramWebhookURL,
			},
		}, nil
	}
	
	return []TelegramBotConfig{}, nil
}
