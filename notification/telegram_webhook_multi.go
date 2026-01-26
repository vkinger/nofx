package notification

import (
	"fmt"
	"nofx/config"
	"nofx/logger"
	"strings"
	"sync"
)

// MultiTelegramWebhook 多 Telegram Bot Webhook 管理器
// 管理多个 bot 的 webhook，每个 bot 对应一个 chatId
type MultiTelegramWebhook struct {
	webhooks map[string]*TelegramWebhook // key: bot token (用于识别不同的 bot)
	mu       sync.RWMutex
}

// NewMultiTelegramWebhook 创建多 Telegram Bot Webhook 管理器
func NewMultiTelegramWebhook() (*MultiTelegramWebhook, error) {
	botConfigs, err := config.GetTelegramBotConfigs()
	if err != nil {
		return nil, fmt.Errorf("failed to get telegram bot configs: %w", err)
	}

	mtw := &MultiTelegramWebhook{
		webhooks: make(map[string]*TelegramWebhook),
	}

	for i, botConfig := range botConfigs {
		if botConfig.Token == "" || botConfig.ChatID == 0 {
			logger.Warnf("⚠️ Telegram bot config #%d is incomplete (token or chat_id missing), skipping", i+1)
			continue
		}

		// 初始化所有 bot（webhook URL 将在 StartAllWebhooks 时统一设置）
		webhook, err := NewTelegramWebhook(botConfig.Token, botConfig.ChatID)
		if err != nil {
			logger.Warnf("⚠️ Failed to create Telegram webhook #%d: %v", i+1, err)
			continue
		}

		if webhook != nil && webhook.enabled {
			mtw.webhooks[botConfig.Token] = webhook
			logger.Infof("✓ Telegram webhook #%d initialized (ChatID: %d, Token: %s...)",
				i+1, botConfig.ChatID, botConfig.Token[:min(10, len(botConfig.Token))])
		}
	}

	if len(mtw.webhooks) == 0 {
		logger.Warnf("⚠️ No valid Telegram webhooks configured")
		return &MultiTelegramWebhook{webhooks: make(map[string]*TelegramWebhook)}, nil
	}

	logger.Infof("✓ MultiTelegramWebhook initialized with %d webhook(s)", len(mtw.webhooks))
	return mtw, nil
}

// StartAllWebhooks 启动所有 webhook
// webhookURL: 基础 webhook URL，每个 bot 会在此基础上添加 token 前缀
// 例如: https://domain.com/api/telegram/webhook -> https://domain.com/api/telegram/webhook/8333151099
func (mtw *MultiTelegramWebhook) StartAllWebhooks(webhookURL string) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook URL is required")
	}

	// 移除末尾的斜杠（如果有）
	webhookURL = strings.TrimSuffix(webhookURL, "/")

	mtw.mu.Lock()
	defer mtw.mu.Unlock()

	var lastErr error
	successCount := 0
	for token, webhook := range mtw.webhooks {
		// 为每个 bot 生成独立的 webhook URL，使用 token 前 10 位作为标识
		tokenPrefix := token[:min(10, len(token))]
		botWebhookURL := fmt.Sprintf("%s/%s", webhookURL, tokenPrefix)

		if err := webhook.StartWebhook(botWebhookURL); err != nil {
			logger.Warnf("⚠️ Failed to start webhook for bot %s... (ChatID: %d): %v",
				tokenPrefix, webhook.GetChatID(), err)
			lastErr = err
		} else {
			successCount++
			logger.Infof("✓ Webhook started for bot %s... (ChatID: %d) with URL: %s",
				tokenPrefix, webhook.GetChatID(), botWebhookURL)
		}
	}

	if successCount > 0 {
		logger.Infof("✓ Total %d/%d webhooks started successfully (each bot has unique URL based on: %s)",
			successCount, len(mtw.webhooks), webhookURL)
	}

	return lastErr
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StopAllWebhooks 停止所有 webhook
func (mtw *MultiTelegramWebhook) StopAllWebhooks() {
	mtw.mu.Lock()
	defer mtw.mu.Unlock()

	for token, webhook := range mtw.webhooks {
		webhook.StopWebhook()
		logger.Infof("✓ Webhook stopped for bot %s...", token[:min(10, len(token))])
	}
}

// GetWebhookByToken 根据 token 获取 webhook
func (mtw *MultiTelegramWebhook) GetWebhookByToken(token string) *TelegramWebhook {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()
	return mtw.webhooks[token]
}

// GetWebhookByTokenPrefix 根据 token 前缀获取 webhook
// tokenPrefix 是 bot token 的前 N 位（通常是 10 位）
func (mtw *MultiTelegramWebhook) GetWebhookByTokenPrefix(tokenPrefix string) *TelegramWebhook {
	if tokenPrefix == "" {
		return nil
	}

	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	for token, webhook := range mtw.webhooks {
		if strings.HasPrefix(token, tokenPrefix) {
			return webhook
		}
	}
	return nil
}

// GetWebhookCount 获取配置的 webhook 数量
func (mtw *MultiTelegramWebhook) GetWebhookCount() int {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()
	return len(mtw.webhooks)
}

// RegisterCommandForAll 为所有 webhook 注册命令处理器
func (mtw *MultiTelegramWebhook) RegisterCommandForAll(command string, handler CommandHandler) {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	for token, webhook := range mtw.webhooks {
		webhook.RegisterCommand(command, handler)
		logger.Debugf("Command '%s' registered for bot %s...", command, token[:min(10, len(token))])
	}
}

// SendWelcomeMessageToAll 向所有 webhook 发送欢迎消息
func (mtw *MultiTelegramWebhook) SendWelcomeMessageToAll() {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	for token, webhook := range mtw.webhooks {
		if err := webhook.SendWelcomeMessage(); err != nil {
			logger.Warnf("⚠️ Failed to send welcome message to bot %s...: %v",
				token[:min(10, len(token))], err)
		} else {
			logger.Debugf("✓ Welcome message sent to bot %s...", token[:min(10, len(token))])
		}
	}
}

// GetAllWebhooks 获取所有 webhook（用于 API server）
func (mtw *MultiTelegramWebhook) GetAllWebhooks() map[string]*TelegramWebhook {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	result := make(map[string]*TelegramWebhook)
	for token, webhook := range mtw.webhooks {
		result[token] = webhook
	}
	return result
}

// GetWebhookByChatID 根据 ChatID 获取对应的 webhook
func (mtw *MultiTelegramWebhook) GetWebhookByChatID(chatID int64) *TelegramWebhook {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	for _, webhook := range mtw.webhooks {
		if webhook != nil && webhook.GetChatID() == chatID {
			return webhook
		}
	}
	return nil
}

// GetFirstWebhook 获取第一个可用的 webhook（用于处理未匹配的 /login 或 /start 命令）
func (mtw *MultiTelegramWebhook) GetFirstWebhook() *TelegramWebhook {
	mtw.mu.RLock()
	defer mtw.mu.RUnlock()

	for _, webhook := range mtw.webhooks {
		if webhook != nil {
			return webhook
		}
	}
	return nil
}
