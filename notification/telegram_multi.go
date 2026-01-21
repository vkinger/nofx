package notification

import (
	"fmt"
	"nofx/config"
	"nofx/logger"
	"sync"
)

// MultiTelegramNotifier 多 Telegram Bot 通知服务
// 支持向多个 bot 同时发送消息
type MultiTelegramNotifier struct {
	notifiers []*TelegramNotifier
	mu        sync.RWMutex
}

// NewMultiTelegramNotifier 创建多 Telegram Bot 通知服务
func NewMultiTelegramNotifier() (*MultiTelegramNotifier, error) {
	botConfigs, err := config.GetTelegramBotConfigs()
	if err != nil {
		return nil, fmt.Errorf("failed to get telegram bot configs: %w", err)
	}

	mtn := &MultiTelegramNotifier{
		notifiers: make([]*TelegramNotifier, 0),
	}

	for i, botConfig := range botConfigs {
		if botConfig.Token == "" || botConfig.ChatID == 0 {
			logger.Warnf("⚠️ Telegram bot config #%d is incomplete (token or chat_id missing), skipping", i+1)
			continue
		}

		notifier, err := NewTelegramNotifier(botConfig.Token, botConfig.ChatID)
		if err != nil {
			logger.Warnf("⚠️ Failed to initialize Telegram bot #%d: %v", i+1, err)
			continue
		}

		if notifier != nil && notifier.enabled {
			mtn.notifiers = append(mtn.notifiers, notifier)
			logger.Infof("✓ Telegram bot #%d initialized (ChatID: %d)", i+1, botConfig.ChatID)
		}
	}

	if len(mtn.notifiers) == 0 {
		logger.Warnf("⚠️ No valid Telegram bots configured")
		return &MultiTelegramNotifier{notifiers: []*TelegramNotifier{}}, nil
	}

	logger.Infof("✓ MultiTelegramNotifier initialized with %d bot(s)", len(mtn.notifiers))
	return mtn, nil
}

// SendMessage 向所有配置的 bot 发送消息
func (mtn *MultiTelegramNotifier) SendMessage(text string) error {
	mtn.mu.RLock()
	defer mtn.mu.RUnlock()

	if len(mtn.notifiers) == 0 {
		return nil
	}

	var lastErr error
	for i, notifier := range mtn.notifiers {
		if err := notifier.SendMessage(text); err != nil {
			logger.Warnf("⚠️ Failed to send message to Telegram bot #%d: %v", i+1, err)
			lastErr = err
		}
	}

	return lastErr
}

// GetNotifierCount 获取配置的 bot 数量
func (mtn *MultiTelegramNotifier) GetNotifierCount() int {
	mtn.mu.RLock()
	defer mtn.mu.RUnlock()
	return len(mtn.notifiers)
}

// IsEnabled 检查是否有启用的 bot
func (mtn *MultiTelegramNotifier) IsEnabled() bool {
	mtn.mu.RLock()
	defer mtn.mu.RUnlock()
	return len(mtn.notifiers) > 0
}

// NewMultiTelegramNotifierFromSingle 从单个 TelegramNotifier 创建 MultiTelegramNotifier（向后兼容）
func NewMultiTelegramNotifierFromSingle(notifier *TelegramNotifier) *MultiTelegramNotifier {
	if notifier == nil {
		return &MultiTelegramNotifier{notifiers: []*TelegramNotifier{}}
	}
	return &MultiTelegramNotifier{notifiers: []*TelegramNotifier{notifier}}
}
