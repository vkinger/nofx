package main

import (
	"fmt"
	"nofx/api"
	"nofx/backtest"
	"nofx/config"
	"nofx/crypto"
	"nofx/experience"
	"nofx/kernel"
	"nofx/logger"
	"nofx/manager"
	"nofx/mcp"
	"nofx/notification"
	"nofx/store"
	"nofx/trader"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// userStoreAdapterImpl 用户存储适配器实现（在 main.go 中定义以避免循环导入）
type userStoreAdapterImpl struct {
	store *store.UserStore
}

// GetByID 根据用户ID获取用户
func (usa *userStoreAdapterImpl) GetByID(userID string) (notification.UserInterface, error) {
	user, err := usa.store.GetByID(userID)
	if err != nil {
		return nil, err
	}
	return &userAdapterImpl{user: user}, nil
}

// GetByEmail 根据邮箱获取用户
func (usa *userStoreAdapterImpl) GetByEmail(email string) (notification.UserInterface, error) {
	user, err := usa.store.GetByEmail(email)
	if err != nil {
		return nil, err
	}
	return &userAdapterImpl{user: user}, nil
}

// userAdapterImpl 用户适配器实现
type userAdapterImpl struct {
	user *store.User
}

// GetID 获取用户ID
func (ua *userAdapterImpl) GetID() string {
	return ua.user.ID
}

// GetOTPSecret 获取 OTP Secret
func (ua *userAdapterImpl) GetOTPSecret() string {
	return ua.user.OTPSecret
}

// IsOTPVerified 检查 OTP 是否已验证
func (ua *userAdapterImpl) IsOTPVerified() bool {
	return ua.user.OTPVerified
}

// traderManagerAdapterImpl 交易员管理器适配器实现（在 main.go 中定义以避免循环导入）
type traderManagerAdapterImpl struct {
	traderManager *manager.TraderManager
}

// GetAllTraders 获取所有交易员
func (tma *traderManagerAdapterImpl) GetAllTraders() map[string]notification.TraderInterface {
	traders := tma.traderManager.GetAllTraders()
	result := make(map[string]notification.TraderInterface)
	for id, trader := range traders {
		result[id] = &traderAdapterImpl{trader: trader}
	}
	return result
}

// GetTrader 获取指定交易员
func (tma *traderManagerAdapterImpl) GetTrader(traderID string) (notification.TraderInterface, error) {
	trader, err := tma.traderManager.GetTrader(traderID)
	if err != nil {
		return nil, err
	}
	return &traderAdapterImpl{trader: trader}, nil
}

// LoadUserTradersFromStore 从存储加载用户交易员
func (tma *traderManagerAdapterImpl) LoadUserTradersFromStore(store interface{}, userID string) error {
	// 类型断言获取 store.Store
	if storeAdapter, ok := store.(*storeAdapterImpl); ok {
		return tma.traderManager.LoadUserTradersFromStore(storeAdapter.store, userID)
	}
	return fmt.Errorf("invalid store type")
}

// traderAdapterImpl 交易员适配器实现（在 main.go 中定义以避免循环导入）
type traderAdapterImpl struct {
	trader *trader.AutoTrader
}

// GetID 获取交易员ID
func (ta *traderAdapterImpl) GetID() string {
	return ta.trader.GetID()
}

// GetName 获取交易员名称
func (ta *traderAdapterImpl) GetName() string {
	return ta.trader.GetName()
}

// GetAccountInfo 获取账户信息
func (ta *traderAdapterImpl) GetAccountInfo() (map[string]interface{}, error) {
	return ta.trader.GetAccountInfo()
}

// GetPositions 获取持仓信息
func (ta *traderAdapterImpl) GetPositions() ([]map[string]interface{}, error) {
	return ta.trader.GetPositions()
}

// ExecuteDecision 执行交易决策
func (ta *traderAdapterImpl) ExecuteDecision(decision *kernel.Decision) error {
	return ta.trader.ExecuteDecision(decision)
}

// GetStatus 获取交易员状态
func (ta *traderAdapterImpl) GetStatus() map[string]interface{} {
	return ta.trader.GetStatus()
}

// GetStore 获取存储接口
func (ta *traderAdapterImpl) GetStore() interface {
	Position() notification.PositionStoreInterface
} {
	return &positionStoreAdapterImpl{store: ta.trader.GetStore()}
}

// Run 启动交易员
func (ta *traderAdapterImpl) Run() error {
	return ta.trader.Run()
}

// Stop 停止交易员
func (ta *traderAdapterImpl) Stop() {
	ta.trader.Stop()
}

// GetTrader 获取底层交易员实例
func (ta *traderAdapterImpl) GetTrader() interface {
	SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error
	SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error
} {
	return ta.trader.GetTrader()
}

// positionStoreAdapterImpl 持仓存储适配器实现
type positionStoreAdapterImpl struct {
	store *store.Store
}

// Position 获取持仓存储
func (psa *positionStoreAdapterImpl) Position() notification.PositionStoreInterface {
	return &positionStoreImpl{positionStore: psa.store.Position()}
}

// positionStoreImpl 持仓存储实现
type positionStoreImpl struct {
	positionStore *store.PositionStore
}

// GetRecentTrades 获取最近交易
func (psi *positionStoreImpl) GetRecentTrades(traderID string, limit int) ([]map[string]interface{}, error) {
	trades, err := psi.positionStore.GetRecentTrades(traderID, limit)
	if err != nil {
		return nil, err
	}
	
	// 转换为 map 格式
	result := make([]map[string]interface{}, len(trades))
	for i, trade := range trades {
		result[i] = map[string]interface{}{
			"symbol":        trade.Symbol,
			"side":          trade.Side,
			"entry_price":   trade.EntryPrice,
			"exit_price":    trade.ExitPrice,
			"realized_pnl":  trade.RealizedPnL,
			"pnl_pct":       trade.PnLPct,
			"entry_time":    trade.EntryTime,
			"exit_time":     trade.ExitTime,
			"hold_duration": trade.HoldDuration,
		}
	}
	return result, nil
}

// storeAdapterImpl 存储适配器实现
type storeAdapterImpl struct {
	store *store.Store
}

// Trader 获取交易员存储
func (sa *storeAdapterImpl) Trader() notification.TraderStoreInterface {
	return &traderStoreImpl{traderStore: sa.store.Trader()}
}

// traderStoreImpl 交易员存储实现
type traderStoreImpl struct {
	traderStore *store.TraderStore
}

// List 获取交易员列表
func (tsi *traderStoreImpl) List(userID string) ([]notification.TraderInfo, error) {
	traders, err := tsi.traderStore.List(userID)
	if err != nil {
		return nil, err
	}
	
	result := make([]notification.TraderInfo, len(traders))
	for i, trader := range traders {
		result[i] = &traderInfoImpl{trader: trader}
	}
	return result, nil
}

// UpdateStatus 更新交易员状态
func (tsi *traderStoreImpl) UpdateStatus(userID, traderID string, isRunning bool) error {
	return tsi.traderStore.UpdateStatus(userID, traderID, isRunning)
}

// traderInfoImpl 交易员信息实现
type traderInfoImpl struct {
	trader *store.Trader
}

// GetID 获取交易员ID
func (ti *traderInfoImpl) GetID() string {
	return ti.trader.ID
}

// GetName 获取交易员名称
func (ti *traderInfoImpl) GetName() string {
	return ti.trader.Name
}

// IsRunning 获取运行状态
func (ti *traderInfoImpl) IsRunning() bool {
	return ti.trader.IsRunning
}

func main() {
	// Load .env environment variables
	_ = godotenv.Load()

	// Initialize logger
	logger.Init(nil)

	logger.Info("╔════════════════════════════════════════════════════════════╗")
	logger.Info("║           🚀 NOFX - AI-Powered Trading System              ║")
	logger.Info("╚════════════════════════════════════════════════════════════╝")

	// Initialize global configuration (loaded from .env)
	config.Init()
	cfg := config.Get()
	logger.Info("✅ Configuration loaded")

	// Initialize encryption service BEFORE database (so EncryptedString can decrypt on read)
	logger.Info("🔐 Initializing encryption service...")
	cryptoService, err := crypto.NewCryptoService()
	if err != nil {
		logger.Fatalf("❌ Failed to initialize encryption service: %v", err)
	}
	crypto.SetGlobalCryptoService(cryptoService)
	logger.Info("✅ Encryption service initialized successfully")

	// Initialize database from configuration
	// For backward compatibility: command line arg overrides config (SQLite only)
	if len(os.Args) > 1 {
		cfg.DBPath = os.Args[1]
	}
	// Ensure data directory exists (for SQLite)
	if cfg.DBType == "sqlite" {
		dataDir := filepath.Dir(cfg.DBPath)
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			logger.Fatalf("❌ Failed to create data directory: %v", err)
		}
	}

	logger.Info("💾 Initializing database...")
	dbConfig := store.DBConfig{
		Type:     store.DBType(cfg.DBType),
		Path:     cfg.DBPath,
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
	}
	st, err := store.NewWithConfig(dbConfig)
	if err != nil {
		logger.Fatalf("❌ Failed to initialize database: %v", err)
	}
	logger.Info("✅ Database initialized successfully")

	// Initialize backtest manager
	logger.Info("🧪 Initializing backtest manager...")
	backtestManager := backtest.NewManager(newSharedMCPClient())
	logger.Info("✅ Backtest manager initialized successfully")

	// Initialize trader manager
	logger.Info("🤖 Initializing trader manager...")
	traderManager := manager.NewTraderManager()

	// Load all traders from database to memory (may auto-start traders with IsRunning=true)
	if err := traderManager.LoadTradersFromStore(st); err != nil {
		logger.Fatalf("❌ Failed to load traders: %v", err)
	}

	// Display loaded trader information
	traders, err := st.Trader().List("default")
	if err != nil {
		logger.Fatalf("❌ Failed to get trader list: %v", err)
	}

	logger.Info("🤖 AI Trader Configurations in Database:")
	if len(traders) == 0 {
		logger.Info("  (No trader configurations, please create via Web interface)")
	} else {
		for _, t := range traders {
			status := "❌ Stopped"
			if t.IsRunning {
				status = "✅ Running"
			}
			logger.Infof("  • %s [%s] %s - AI Model: %s, Exchange: %s",
				t.Name, t.ID[:8], status, t.AIModelID, t.ExchangeID)
		}
	}

	// Start API server
	server := api.NewServer(traderManager, st, cryptoService, backtestManager, cfg.APIServerPort)
	go func() {
		if err := server.Start(); err != nil {
			logger.Fatalf("❌ Failed to start API server: %v", err)
		}
	}()

	// Initialize Telegram Webhook (支持多 bot)
	// Wait a bit for server to start
	time.Sleep(2 * time.Second)

	// 尝试使用多 webhook 配置
	multiWebhook, err := notification.NewMultiTelegramWebhook()
	if err == nil && multiWebhook != nil && multiWebhook.GetWebhookCount() > 0 {
		// 获取统一的 webhook URL（优先使用配置中的第一个，或从环境变量获取）
		webhookURL := cfg.TelegramWebhookURL
		botConfigs, err := config.GetTelegramBotConfigs()
		if err == nil && len(botConfigs) > 0 && botConfigs[0].WebhookURL != "" {
			// 使用第一个 bot 的 webhook URL 作为统一 URL
			webhookURL = botConfigs[0].WebhookURL
		}

		if webhookURL == "" {
			logger.Warnf("⚠️ TELEGRAM_WEBHOOK_URL is not set. Telegram Webhook will not be initialized.")
			logger.Warnf("⚠️ To enable Telegram Webhook, please set TELEGRAM_WEBHOOK_URL in your .env file:")
			logger.Warnf("⚠️   - For domain: https://your-domain.com/api/telegram/webhook")
			logger.Warnf("⚠️   - For IP: https://123.45.67.89:443/api/telegram/webhook (requires self-signed cert with IP as CN)")
		} else {
			// 注册指令处理器
			// 创建用户存储适配器（在 main.go 中创建以避免循环导入）
			userStoreAdapter := &userStoreAdapterImpl{store: st.User()}
			// 创建交易员管理器适配器（在 main.go 中创建以避免循环导入）
			traderManagerAdapter := &traderManagerAdapterImpl{traderManager: traderManager}
			// 创建存储适配器
			storeAdapter := &storeAdapterImpl{store: st}
			commandCtx := &notification.CommandContext{
				TraderManager: traderManagerAdapter,
				UserStore:     userStoreAdapter,
				Store:         storeAdapter,
			}
			handlers := notification.CreateCommandHandlers(commandCtx)

			// 为所有 webhook 注册命令处理器
			for cmd, handler := range handlers {
				multiWebhook.RegisterCommandForAll(cmd, handler)
			}

			// 启动所有 Webhook（使用统一的 webhook URL）
			logger.Infof("📱 Starting %d Telegram webhook(s) with unified URL: %s",
				multiWebhook.GetWebhookCount(), webhookURL)
			if err := multiWebhook.StartAllWebhooks(webhookURL); err != nil {
				logger.Warnf("⚠️ Some Telegram webhooks failed to start: %v", err)
			} else {
				server.SetMultiTelegramWebhook(multiWebhook)
				// 发送欢迎消息到所有 bot
				time.Sleep(1 * time.Second)
				multiWebhook.SendWelcomeMessageToAll()
				logger.Infof("✓ MultiTelegramWebhook initialized with %d webhook(s), all using unified URL: %s",
					multiWebhook.GetWebhookCount(), webhookURL)
			}
		}
	} else {
		// 向后兼容：尝试使用单 bot 配置
		if cfg.TelegramEnabled && cfg.TelegramToken != "" && cfg.TelegramChatID != 0 {
			webhookURL := cfg.TelegramWebhookURL
			if webhookURL == "" {
				logger.Warnf("⚠️ TELEGRAM_WEBHOOK_URL is not set. Telegram Webhook will not be initialized.")
				logger.Warnf("⚠️ To enable Telegram Webhook, please set TELEGRAM_WEBHOOK_URL in your .env file:")
				logger.Warnf("⚠️   - For domain: https://your-domain.com/api/telegram/webhook")
				logger.Warnf("⚠️   - For IP: https://123.45.67.89:443/api/telegram/webhook (requires self-signed cert with IP as CN)")
				logger.Warnf("⚠️ Requirements:")
				logger.Warnf("⚠️   - Must use HTTPS (not HTTP)")
				logger.Warnf("⚠️   - Port must be one of: 443, 80, 88, 8443")
				logger.Warnf("⚠️   - Must be publicly accessible (not localhost)")
				logger.Warnf("⚠️   - For IP addresses, SSL certificate CN must match the IP")
			} else {
				logger.Infof("📱 Initializing Telegram Webhook (single bot mode) - ChatID: %d", cfg.TelegramChatID)
				telegramWebhook, err := notification.NewTelegramWebhook(
					cfg.TelegramToken,
					cfg.TelegramChatID,
				)
				if err == nil && telegramWebhook != nil {
					// 注册指令处理器
					userStoreAdapter := &userStoreAdapterImpl{store: st.User()}
					traderManagerAdapter := &traderManagerAdapterImpl{traderManager: traderManager}
					// 创建存储适配器
					storeAdapter := &storeAdapterImpl{store: st}
					commandCtx := &notification.CommandContext{
						TraderManager: traderManagerAdapter,
						UserStore:     userStoreAdapter,
						Store:         storeAdapter,
					}
					handlers := notification.CreateCommandHandlers(commandCtx)
					for cmd, handler := range handlers {
						telegramWebhook.RegisterCommand(cmd, handler)
					}

					// 启动 Webhook
					if err := telegramWebhook.StartWebhook(webhookURL); err == nil {
						server.SetTelegramWebhook(telegramWebhook)
						// 发送欢迎消息
						time.Sleep(1 * time.Second)
						telegramWebhook.SendWelcomeMessage()
						logger.Info("✓ Telegram Webhook initialized and welcome message sent")
					} else {
						logger.Warnf("⚠️ Failed to start Telegram webhook: %v", err)
						logger.Warnf("⚠️ Please check your TELEGRAM_WEBHOOK_URL configuration")
					}
				} else if err != nil {
					logger.Warnf("⚠️ Failed to create Telegram webhook: %v", err)
				}
			}
		}
	}

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	logger.Info("✅ System started successfully, waiting for trading commands...")
	logger.Info("📌 Tip: Use Ctrl+C to stop the system")

	<-quit
	logger.Info("📴 Shutdown signal received, closing system...")

	// Stop all traders
	traderManager.StopAll()
	logger.Info("✅ System shut down safely")
}

// newSharedMCPClient creates a shared MCP AI client (for backtesting)
func newSharedMCPClient() mcp.AIClient {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		logger.Warn("⚠️ DEEPSEEK_API_KEY not set, AI features will be unavailable")
		return nil
	}
	return mcp.NewDeepSeekClient()
}

// initInstallationID initializes the anonymous installation ID for experience improvement
// This ID is persisted in database and used for anonymous usage statistics
func initInstallationID(st *store.Store) {
	const key = "installation_id"

	// Try to load from database
	installationID, err := st.GetSystemConfig(key)
	if err != nil {
		logger.Warnf("⚠️ Failed to load installation ID: %v", err)
	}

	// Generate new ID if not exists
	if installationID == "" {
		installationID = uuid.New().String()
		if err := st.SetSystemConfig(key, installationID); err != nil {
			logger.Warnf("⚠️ Failed to save installation ID: %v", err)
		}
		logger.Infof("📊 Generated new installation ID: %s", installationID[:8]+"...")
	}

	// Set installation ID in experience module
	experience.SetInstallationID(installationID)
}
