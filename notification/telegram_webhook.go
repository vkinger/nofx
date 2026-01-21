package notification

import (
	"fmt"
	"net/url"
	"nofx/logger"
	"regexp"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramWebhook Telegram Webhook 服务
type TelegramWebhook struct {
	bot             *tgbotapi.BotAPI
	chatID          int64
	enabled         bool
	commandChan     chan *tgbotapi.Update
	stopChan        chan struct{}
	commandHandlers map[string]CommandHandler
	mu              sync.RWMutex
}

// CommandHandler 指令处理函数类型
type CommandHandler func(update *tgbotapi.Update) string

// NewTelegramWebhook 创建 Telegram Webhook 服务
func NewTelegramWebhook(token string, chatID int64) (*TelegramWebhook, error) {
	if token == "" || chatID == 0 {
		return &TelegramWebhook{enabled: false}, nil
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	logger.Infof("✓ Telegram webhook bot initialized: @%s", bot.Self.UserName)

	return &TelegramWebhook{
		bot:             bot,
		chatID:          chatID,
		enabled:         true,
		commandChan:     make(chan *tgbotapi.Update, 100),
		stopChan:        make(chan struct{}),
		commandHandlers: make(map[string]CommandHandler),
	}, nil
}

// StartWebhook 启动 Webhook 监听
func (tw *TelegramWebhook) StartWebhook(webhookURL string) error {
	if !tw.enabled {
		return nil
	}

	// 验证 Webhook URL
	if err := validateWebhookURL(webhookURL); err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	// 设置 Webhook
	wh, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return fmt.Errorf("failed to create webhook: %w", err)
	}

	_, err = tw.bot.Request(wh)
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	// 启动命令处理协程
	go tw.processCommands()

	logger.Infof("✓ Telegram webhook started: %s", webhookURL)
	return nil
}

// validateWebhookURL 验证 Webhook URL 是否符合 Telegram 要求
func validateWebhookURL(webhookURL string) error {
	parsedURL, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// 1. 必须使用 HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("webhook URL must use HTTPS (got: %s). Telegram requires HTTPS for security", parsedURL.Scheme)
	}

	// 2. 检查端口（Telegram 只支持 443, 80, 88, 8443）
	port := parsedURL.Port()
	if port != "" {
		allowedPorts := map[string]bool{
			"443":  true,
			"80":   true,
			"88":   true,
			"8443": true,
		}
		if !allowedPorts[port] {
			return fmt.Errorf("port %s is not allowed. Telegram only supports ports: 443, 80, 88, 8443", port)
		}
	} else {
		// 默认端口 443 (HTTPS)
		logger.Infof("No port specified, using default HTTPS port 443")
	}

	// 3. 验证主机名（可以是域名或 IP 地址）
	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("hostname is required")
	}

	// 检查是否为 localhost（生产环境不允许）
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("localhost is not accessible from Telegram servers. Use a public IP address or domain name")
	}

	// 验证 IP 地址格式（如果使用 IP）
	ipRegex := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	if ipRegex.MatchString(host) {
		logger.Warnf("⚠️ Using IP address for webhook: %s. Make sure your SSL certificate's CN matches this IP address", host)
		logger.Warnf("⚠️ For IP addresses, you need a self-signed certificate with CN set to the IP address")
	} else {
		// 验证域名格式（基本检查）
		domainRegex := regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
		if !domainRegex.MatchString(host) {
			return fmt.Errorf("invalid hostname format: %s", host)
		}
	}

	// 4. 路径不能为空
	if parsedURL.Path == "" {
		return fmt.Errorf("webhook URL path is required (e.g., /api/telegram/webhook)")
	}

	return nil
}

// StopWebhook 停止 Webhook
func (tw *TelegramWebhook) StopWebhook() {
	if !tw.enabled {
		return
	}

	close(tw.stopChan)

	// 删除 Webhook
	_, _ = tw.bot.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: true})
	logger.Info("✓ Telegram webhook stopped")
}

// HandleUpdate 处理 Webhook 更新
func (tw *TelegramWebhook) HandleUpdate(update *tgbotapi.Update) {
	if !tw.enabled {
		logger.Warnf("Telegram webhook is not enabled, ignoring update")
		return
	}

	// 只处理消息类型的更新
	if update.Message == nil {
		logger.Debugf("Telegram webhook update has no message (UpdateID: %d), ignoring", update.UpdateID)
		return
	}

	// 只处理来自配置的 chatID 的消息
	// 注意：频道消息的 ChatID 是负数，个人聊天的 ChatID 是正数
	if update.Message.Chat.ID != tw.chatID {
		logger.Warnf("Telegram webhook message from unauthorized chatID: %d (expected: %d), ignoring. Chat type: %s", 
			update.Message.Chat.ID, tw.chatID, update.Message.Chat.Type)
		logger.Infof("Please check your TELEGRAM_CHAT_ID configuration. Current message chatID: %d, configured chatID: %d", 
			update.Message.Chat.ID, tw.chatID)
		return
	}

	logger.Infof("Telegram webhook message accepted, sending to command channel - ChatID: %d, Text: %s", 
		update.Message.Chat.ID, update.Message.Text)

	// 发送到命令处理通道
	select {
	case tw.commandChan <- update:
		logger.Infof("Telegram webhook update sent to command channel successfully")
	default:
		logger.Warnf("Command channel full, dropping update")
	}
}

// RegisterCommand 注册指令处理器
func (tw *TelegramWebhook) RegisterCommand(command string, handler CommandHandler) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.commandHandlers[command] = handler
}

// SendMessage 发送消息
func (tw *TelegramWebhook) SendMessage(text string) error {
	if !tw.enabled {
		return nil
	}

	msg := tgbotapi.NewMessage(tw.chatID, text)
	msg.ParseMode = "HTML"

	_, err := tw.bot.Send(msg)
	if err != nil {
		logger.Errorf("Failed to send telegram message: %v", err)
		return err
	}
	return nil
}

// SendWelcomeMessage 发送欢迎消息和使用说明
func (tw *TelegramWebhook) SendWelcomeMessage() error {
	helpText := `🤖 <b>NOFX 交易机器人已启动</b>

📋 <b>可用指令：</b>

/price [币种] - 查看币种当前价格（无需验证）
  示例: /price BTCUSDT

<b>需要邮箱和 2FA 验证码的操作：</b>
/account [邮箱] [OTP码] - 查看账户及持仓信息
  示例: /account user@example.com 123456

/sl [邮箱] [币种] [止损价] [OTP码] - 设置止损
  示例: /sl user@example.com BTCUSDT 42000 123456

/tp [邮箱] [币种] [止盈价] [OTP码] - 设置止盈
  示例: /tp user@example.com BTCUSDT 45000 123456

/close [邮箱] [币种] [方向] [OTP码] - 平仓
  示例: /close user@example.com BTCUSDT long 123456

/help - 显示帮助信息（无需验证）

📢 <b>自动推送功能：</b>
系统会自动推送交易决策、账户摘要和持仓详情到 Telegram，无需手动查询。

💡 <b>提示：</b>
- 只有 /price 和 /help 指令无需验证码
- 其他所有指令都需要提供邮箱和 Google Authenticator 验证码
- 邮箱应该是注册时使用的邮箱地址
- OTP 码来自你的 Google Authenticator 等 2FA 应用
- 发送 /help 查看完整帮助信息
- 币种格式: BTCUSDT, ETHUSDT 等
- 方向: long (做多) 或 short (做空)
- 价格请使用数字，无需单位`

	return tw.SendMessage(helpText)
}

// processCommands 处理命令
func (tw *TelegramWebhook) processCommands() {
	for {
		select {
		case <-tw.stopChan:
			return
		case update := <-tw.commandChan:
			logger.Infof("Processing command from channel - UpdateID: %d", update.UpdateID)
			if update.Message == nil {
				logger.Warnf("Update has no message, skipping")
				continue
			}

			text := update.Message.Text
			if text == "" {
				logger.Warnf("Message text is empty, skipping")
				continue
			}

			logger.Infof("Processing command text: %s", text)

			// 解析命令
			parts := strings.Fields(text)
			if len(parts) == 0 {
				logger.Warnf("Command has no parts, skipping")
				continue
			}

			command := strings.ToLower(parts[0])
			args := parts[1:]
			logger.Infof("Parsed command: %s, args: %v", command, args)

			tw.mu.RLock()
			handler, ok := tw.commandHandlers[command]
			tw.mu.RUnlock()

			var response string
			if ok {
				logger.Infof("Found handler for command: %s", command)
				// 创建带参数的更新对象
				updateWithArgs := *update
				updateWithArgs.Message.Text = strings.Join(args, " ")
				response = handler(&updateWithArgs)
				logger.Infof("Handler returned response (length: %d)", len(response))
			} else {
				logger.Warnf("No handler found for command: %s", command)
				response = "❌ 未知指令。发送 /help 查看帮助。"
			}

			// 发送响应
			if response != "" {
				logger.Infof("Sending response message (length: %d)", len(response))
				if err := tw.SendMessage(response); err != nil {
					logger.Errorf("Failed to send command response: %v", err)
				} else {
					logger.Infof("Response message sent successfully")
				}
			} else {
				logger.Warnf("Response is empty, not sending message")
			}
		}
	}
}

// GetBot 获取 Bot 实例（用于发送消息）
func (tw *TelegramWebhook) GetBot() *tgbotapi.BotAPI {
	return tw.bot
}

