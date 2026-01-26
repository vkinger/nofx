package trader

import (
	"encoding/json"
	"fmt"
	"math"
	globalconfig "nofx/config"
	"nofx/experience"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/notification"
	"nofx/store"
	"strings"
	"sync"
	"time"
)

// AutoTraderConfig auto trading configuration (simplified version - AI makes all decisions)
type AutoTraderConfig struct {
	// Trader identification
	ID      string // Trader unique identifier (for log directory, etc.)
	Name    string // Trader display name
	AIModel string // AI model: "qwen" or "deepseek"

	// Trading platform selection
	Exchange   string // Exchange type: "binance", "bybit", "okx", "bitget", "hyperliquid", "aster" or "lighter"
	ExchangeID string // Exchange account UUID (for multi-account support)

	// Binance API configuration
	BinanceAPIKey    string
	BinanceSecretKey string

	// Bybit API configuration
	BybitAPIKey    string
	BybitSecretKey string

	// OKX API configuration
	OKXAPIKey     string
	OKXSecretKey  string
	OKXPassphrase string

	// Bitget API configuration
	BitgetAPIKey     string
	BitgetSecretKey  string
	BitgetPassphrase string

	// Hyperliquid configuration
	HyperliquidPrivateKey string
	HyperliquidWalletAddr string
	HyperliquidTestnet    bool

	// Aster configuration
	AsterUser       string // Aster main wallet address
	AsterSigner     string // Aster API wallet address
	AsterPrivateKey string // Aster API wallet private key

	// LIGHTER configuration
	LighterWalletAddr       string // LIGHTER wallet address (L1 wallet)
	LighterPrivateKey       string // LIGHTER L1 private key (for account identification)
	LighterAPIKeyPrivateKey string // LIGHTER API Key private key (40 bytes, for transaction signing)
	LighterAPIKeyIndex      int    // LIGHTER API Key index (0-255)
	LighterTestnet          bool   // Whether to use testnet

	// AI configuration
	UseQwen     bool
	DeepSeekKey string
	QwenKey     string

	// Custom AI API configuration
	CustomAPIURL    string
	CustomAPIKey    string
	CustomModelName string

	// Scan configuration
	ScanInterval time.Duration // Scan interval (recommended 3 minutes)

	// Account configuration
	InitialBalance float64 // Initial balance (for P&L calculation, must be set manually)

	// Risk control (only as hints, AI can make autonomous decisions)
	MaxDailyLoss    float64       // Maximum daily loss percentage (hint)
	MaxDrawdown     float64       // Maximum drawdown percentage (hint)
	StopTradingTime time.Duration // Pause duration after risk control triggers

	// Position mode
	IsCrossMargin bool // true=cross margin mode, false=isolated margin mode

	// Competition visibility
	ShowInCompetition bool // Whether to show in competition page

	// Strategy configuration (use complete strategy config)
	StrategyConfig *store.StrategyConfig // Strategy configuration (includes coin sources, indicators, risk control, prompts, etc.)
}

// AutoTrader automatic trader
type AutoTrader struct {
	id                    string // Trader unique identifier
	name                  string // Trader display name
	aiModel               string // AI model name
	exchange              string // Trading platform type (binance/bybit/etc)
	exchangeID            string // Exchange account UUID
	showInCompetition     bool   // Whether to show in competition page
	config                AutoTraderConfig
	trader                Trader // Use Trader interface (supports multiple platforms)
	mcpClient             mcp.AIClient
	store                 *store.Store           // Data storage (decision records, etc.)
	strategyEngine        *kernel.StrategyEngine // Strategy engine (uses strategy configuration)
	cycleNumber           int                    // Current cycle number
	initialBalance        float64
	dailyPnL              float64
	customPrompt          string // Custom trading strategy prompt
	overrideBasePrompt    bool   // Whether to override base prompt
	lastResetTime         time.Time
	stopUntil             time.Time
	isRunning             bool
	isRunningMutex        sync.RWMutex                        // Mutex to protect isRunning flag
	startTime             time.Time                           // System start time
	callCount             int                                 // AI call count
	positionFirstSeenTime map[string]int64                    // Position first seen time (symbol_side -> timestamp in milliseconds)
	stopMonitorCh         chan struct{}                       // Used to stop monitoring goroutine
	monitorWg             sync.WaitGroup                      // Used to wait for monitoring goroutine to finish
	peakPnLCache          map[string]float64                  // Peak profit cache (symbol -> peak P&L percentage)
	peakPnLCacheMutex     sync.RWMutex                        // Cache read-write lock
	trailingStopPrice     map[string]float64                  // Trailing stop price cache (symbol_side -> trailing stop price)
	trailingStopPriceMutex sync.RWMutex                        // Trailing stop price cache mutex
	closingPositions      map[string]bool                     // Positions currently being closed (symbol_side -> true) to prevent duplicate closes
	closingPositionsMutex sync.Mutex                           // Mutex for closing positions map
	lastBalanceSyncTime   time.Time                           // Last balance sync time
	userID                string                              // User ID
	gridState             *GridState                          // Grid trading state (only used when StrategyType == "grid_trading")
	telegramNotifier      *notification.MultiTelegramNotifier // Telegram通知服务（支持多bot）
}

// NewAutoTrader creates an automatic trader
// st parameter is used to store decision records to database
func NewAutoTrader(config AutoTraderConfig, st *store.Store, userID string) (*AutoTrader, error) {
	// Set default values
	if config.ID == "" {
		config.ID = "default_trader"
	}
	if config.Name == "" {
		config.Name = "Default Trader"
	}
	if config.AIModel == "" {
		if config.UseQwen {
			config.AIModel = "qwen"
		} else {
			config.AIModel = "deepseek"
		}
	}

	// Initialize AI client based on provider
	var mcpClient mcp.AIClient
	aiModel := config.AIModel
	if config.UseQwen && aiModel == "" {
		aiModel = "qwen"
	}

	switch aiModel {
	case "claude":
		mcpClient = mcp.NewClaudeClient()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using Claude AI", config.Name)

	case "kimi":
		mcpClient = mcp.NewKimiClient()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using Kimi (Moonshot) AI", config.Name)

	case "gemini":
		mcpClient = mcp.NewGeminiClient()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using Google Gemini AI", config.Name)

	case "grok":
		mcpClient = mcp.NewGrokClient()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using xAI Grok AI", config.Name)

	case "openai":
		mcpClient = mcp.NewOpenAIClient()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using OpenAI", config.Name)

	case "qwen":
		mcpClient = mcp.NewQwenClient()
		apiKey := config.QwenKey
		if apiKey == "" {
			apiKey = config.CustomAPIKey
		}
		mcpClient.SetAPIKey(apiKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using Alibaba Cloud Qwen AI", config.Name)

	case "custom":
		mcpClient = mcp.New()
		mcpClient.SetAPIKey(config.CustomAPIKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using custom AI API: %s (model: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)

	default: // deepseek or empty
		mcpClient = mcp.NewDeepSeekClient()
		apiKey := config.DeepSeekKey
		if apiKey == "" {
			apiKey = config.CustomAPIKey
		}
		mcpClient.SetAPIKey(apiKey, config.CustomAPIURL, config.CustomModelName)
		logger.Infof("🤖 [%s] Using DeepSeek AI", config.Name)
	}

	if config.CustomAPIURL != "" || config.CustomModelName != "" {
		logger.Infof("🔧 [%s] Custom config - URL: %s, Model: %s", config.Name, config.CustomAPIURL, config.CustomModelName)
	}

	// Set default trading platform
	if config.Exchange == "" {
		config.Exchange = "binance"
	}

	// Create corresponding trader based on configuration
	var trader Trader
	var err error

	// Record position mode (general)
	marginModeStr := "Cross Margin"
	if !config.IsCrossMargin {
		marginModeStr = "Isolated Margin"
	}
	logger.Infof("📊 [%s] Position mode: %s", config.Name, marginModeStr)

	switch config.Exchange {
	case "binance":
		logger.Infof("🏦 [%s] Using Binance Futures trading", config.Name)
		trader = NewFuturesTrader(config.BinanceAPIKey, config.BinanceSecretKey, userID)
	case "bybit":
		logger.Infof("🏦 [%s] Using Bybit Futures trading", config.Name)
		trader = NewBybitTrader(config.BybitAPIKey, config.BybitSecretKey)
	case "okx":
		logger.Infof("🏦 [%s] Using OKX Futures trading", config.Name)
		trader = NewOKXTrader(config.OKXAPIKey, config.OKXSecretKey, config.OKXPassphrase)
	case "bitget":
		logger.Infof("🏦 [%s] Using Bitget Futures trading", config.Name)
		trader = NewBitgetTrader(config.BitgetAPIKey, config.BitgetSecretKey, config.BitgetPassphrase)
	case "hyperliquid":
		logger.Infof("🏦 [%s] Using Hyperliquid trading", config.Name)
		trader, err = NewHyperliquidTrader(config.HyperliquidPrivateKey, config.HyperliquidWalletAddr, config.HyperliquidTestnet)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Hyperliquid trader: %w", err)
		}
	case "aster":
		logger.Infof("🏦 [%s] Using Aster trading", config.Name)
		trader, err = NewAsterTrader(config.AsterUser, config.AsterSigner, config.AsterPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Aster trader: %w", err)
		}
	case "lighter":
		logger.Infof("🏦 [%s] Using LIGHTER trading", config.Name)

		if config.LighterWalletAddr == "" || config.LighterAPIKeyPrivateKey == "" {
			return nil, fmt.Errorf("Lighter requires wallet address and API Key private key")
		}

		// Lighter only supports mainnet (testnet disabled)
		trader, err = NewLighterTraderV2(
			config.LighterWalletAddr,
			config.LighterAPIKeyPrivateKey,
			config.LighterAPIKeyIndex,
			false, // Always use mainnet for Lighter
		)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize LIGHTER trader: %w", err)
		}
		logger.Infof("✓ LIGHTER trader initialized successfully")
	default:
		return nil, fmt.Errorf("unsupported trading platform: %s", config.Exchange)
	}

	// Validate initial balance configuration, auto-fetch from exchange if 0
	if config.InitialBalance <= 0 {
		logger.Infof("📊 [%s] Initial balance not set, attempting to fetch current balance from exchange...", config.Name)
		account, err := trader.GetBalance()
		if err != nil {
			return nil, fmt.Errorf("initial balance not set and unable to fetch balance from exchange: %w", err)
		}
		// Try multiple balance field names (different exchanges return different formats)
		balanceKeys := []string{"total_equity", "totalWalletBalance", "wallet_balance", "totalEq", "balance"}
		var foundBalance float64
		for _, key := range balanceKeys {
			if balance, ok := account[key].(float64); ok && balance > 0 {
				foundBalance = balance
				break
			}
		}
		if foundBalance > 0 {
			config.InitialBalance = foundBalance
			logger.Infof("✓ [%s] Auto-fetched initial balance: %.2f USDT", config.Name, foundBalance)
			// Save to database so it persists across restarts
			if st != nil {
				if err := st.Trader().UpdateInitialBalance(userID, config.ID, foundBalance); err != nil {
					logger.Infof("⚠️  [%s] Failed to save initial balance to database: %v", config.Name, err)
				} else {
					logger.Infof("✓ [%s] Initial balance saved to database", config.Name)
				}
			}
		} else {
			return nil, fmt.Errorf("initial balance must be greater than 0, please set InitialBalance in config or ensure exchange account has balance")
		}
	}

	// Get last cycle number (for recovery)
	var cycleNumber int
	if st != nil {
		cycleNumber, _ = st.Decision().GetLastCycleNumber(config.ID)
		logger.Infof("📊 [%s] Decision records will be stored to database", config.Name)
	}

	// Create strategy engine (must have strategy config)
	if config.StrategyConfig == nil {
		return nil, fmt.Errorf("[%s] strategy not configured", config.Name)
	}
	strategyEngine := kernel.NewStrategyEngine(config.StrategyConfig)
	logger.Infof("✓ [%s] Using strategy engine (strategy configuration loaded)", config.Name)

	// Initialize Telegram notifier (支持多 bot)
	var telegramNotifier *notification.MultiTelegramNotifier
	globalCfg := globalconfig.Get()

	// Priority: 1. Multi bot config (from TELEGRAM_BOTS), 2. Single bot config (backward compatibility), 3. Strategy config (deprecated)
	botConfigs, err := globalconfig.GetTelegramBotConfigs()
	if err == nil && len(botConfigs) > 0 {
		// 使用多 bot 配置
		telegramNotifier, err = notification.NewMultiTelegramNotifier()
		if err != nil {
			logger.Warnf("⚠️ [%s] Failed to initialize MultiTelegramNotifier: %v", config.Name, err)
		} else if telegramNotifier != nil && telegramNotifier.IsEnabled() {
			logger.Infof("✓ [%s] MultiTelegramNotifier enabled with %d bot(s)", config.Name, telegramNotifier.GetNotifierCount())
		}
	} else if globalCfg.TelegramEnabled && globalCfg.TelegramToken != "" && globalCfg.TelegramChatID != 0 {
		// 向后兼容：使用单 bot 配置
		telegramNotifier, err = notification.NewMultiTelegramNotifier()
		if err != nil {
			logger.Warnf("⚠️ [%s] Failed to initialize Telegram notifier: %v", config.Name, err)
		} else if telegramNotifier != nil && telegramNotifier.IsEnabled() {
			logger.Infof("✓ [%s] Telegram notifications enabled (single bot mode)", config.Name)
		}
	} else if config.StrategyConfig != nil && config.StrategyConfig.Telegram.IsValid() {
		// 向后兼容：使用策略配置（已废弃）
		// 创建单 bot notifier 并包装为 MultiTelegramNotifier
		singleNotifier, err := notification.NewTelegramNotifier(
			config.StrategyConfig.Telegram.Token,
			config.StrategyConfig.Telegram.ChatID,
		)
		if err == nil && singleNotifier != nil {
			telegramNotifier = notification.NewMultiTelegramNotifierFromSingle(singleNotifier)
			logger.Infof("📱 [%s] Using Telegram config from strategy (deprecated, use TELEGRAM_BOTS instead)", config.Name)
		}
	}

	return &AutoTrader{
		id:                    config.ID,
		name:                  config.Name,
		aiModel:               config.AIModel,
		exchange:              config.Exchange,
		exchangeID:            config.ExchangeID,
		showInCompetition:     config.ShowInCompetition,
		config:                config,
		trader:                trader,
		mcpClient:             mcpClient,
		store:                 st,
		strategyEngine:        strategyEngine,
		cycleNumber:           cycleNumber,
		initialBalance:        config.InitialBalance,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		stopMonitorCh:         make(chan struct{}),
		monitorWg:             sync.WaitGroup{},
		peakPnLCache:          make(map[string]float64),
		peakPnLCacheMutex:     sync.RWMutex{},
		trailingStopPrice:     make(map[string]float64),
		trailingStopPriceMutex: sync.RWMutex{},
		closingPositions:      make(map[string]bool),
		closingPositionsMutex: sync.Mutex{},
		lastBalanceSyncTime:   time.Now(),
		userID:                userID,
		telegramNotifier:      telegramNotifier,
	}, nil
}

// Run runs the automatic trading main loop
func (at *AutoTrader) Run() error {
	at.isRunningMutex.Lock()
	at.isRunning = true
	at.isRunningMutex.Unlock()

	at.stopMonitorCh = make(chan struct{})
	at.startTime = time.Now()

	logger.Info("🚀 AI-driven automatic trading system started")
	logger.Infof("💰 Initial balance: %.2f USDT", at.initialBalance)
	logger.Infof("⚙️  Scan interval: %v", at.config.ScanInterval)
	logger.Info("🤖 AI will make full decisions on leverage, position size, stop loss/take profit, etc.")
	at.monitorWg.Add(1)
	defer at.monitorWg.Done()

	// Start drawdown monitoring
	at.startDrawdownMonitor()

	// Start automatic stop-loss monitoring (real-time protection)
	at.startStopLossMonitor()

	// Start Lighter order sync if using Lighter exchange
	if at.exchange == "lighter" {
		if lighterTrader, ok := at.trader.(*LighterTraderV2); ok && at.store != nil {
			lighterTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Lighter order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start Hyperliquid order sync if using Hyperliquid exchange
	if at.exchange == "hyperliquid" {
		if hyperliquidTrader, ok := at.trader.(*HyperliquidTrader); ok && at.store != nil {
			hyperliquidTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Hyperliquid order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start Bybit order sync if using Bybit exchange
	if at.exchange == "bybit" {
		if bybitTrader, ok := at.trader.(*BybitTrader); ok && at.store != nil {
			bybitTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Bybit order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start OKX order sync if using OKX exchange
	if at.exchange == "okx" {
		if okxTrader, ok := at.trader.(*OKXTrader); ok && at.store != nil {
			okxTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] OKX order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start Bitget order sync if using Bitget exchange
	if at.exchange == "bitget" {
		if bitgetTrader, ok := at.trader.(*BitgetTrader); ok && at.store != nil {
			bitgetTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Bitget order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start Aster order sync if using Aster exchange
	if at.exchange == "aster" {
		if asterTrader, ok := at.trader.(*AsterTrader); ok && at.store != nil {
			asterTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Aster order+position sync enabled (every 30s)", at.name)
		}
	}

	// Start Binance order sync if using Binance exchange
	if at.exchange == "binance" {
		if binanceTrader, ok := at.trader.(*FuturesTrader); ok && at.store != nil {
			binanceTrader.StartOrderSync(at.id, at.exchangeID, at.exchange, at.store, 30*time.Second)
			logger.Infof("🔄 [%s] Binance order+position sync enabled (every 30s)", at.name)
		}
	}

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// Check if this is a grid trading strategy
	isGridStrategy := at.IsGridStrategy()
	if isGridStrategy {
		logger.Infof("🔲 [%s] Grid trading strategy detected, initializing grid...", at.name)
		if err := at.InitializeGrid(); err != nil {
			logger.Errorf("❌ [%s] Failed to initialize grid: %v", at.name, err)
			return fmt.Errorf("grid initialization failed: %w", err)
		}
	}

	// Execute immediately on first run
	if isGridStrategy {
		if err := at.RunGridCycle(); err != nil {
			logger.Infof("❌ Grid execution failed: %v", err)
		}
	} else {
		if err := at.runCycle(); err != nil {
			logger.Infof("❌ Execution failed: %v", err)
		}
	}

	for {
		at.isRunningMutex.RLock()
		running := at.isRunning
		at.isRunningMutex.RUnlock()

		if !running {
			break
		}

		select {
		case <-ticker.C:
			if isGridStrategy {
				if err := at.RunGridCycle(); err != nil {
					logger.Infof("❌ Grid execution failed: %v", err)
				}
			} else {
				if err := at.runCycle(); err != nil {
					logger.Infof("❌ Execution failed: %v", err)
				}
			}
		case <-at.stopMonitorCh:
			logger.Infof("[%s] ⏹ Stop signal received, exiting automatic trading main loop", at.name)
			return nil
		}
	}

	return nil
}

// Stop stops the automatic trading
func (at *AutoTrader) Stop() {
	at.isRunningMutex.Lock()
	if !at.isRunning {
		at.isRunningMutex.Unlock()
		return
	}
	at.isRunning = false
	at.isRunningMutex.Unlock()

	close(at.stopMonitorCh) // Notify monitoring goroutine to stop
	at.monitorWg.Wait()     // Wait for monitoring goroutine to finish
	logger.Info("⏹ Automatic trading system stopped")
}

// runCycle runs one trading cycle (using AI full decision-making)
func (at *AutoTrader) runCycle() error {
	at.callCount++

	logger.Info("\n" + strings.Repeat("=", 70) + "\n")
	logger.Infof("⏰ %s - AI decision cycle #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
	logger.Info(strings.Repeat("=", 70))

	// 0. Check if trader is stopped (early exit to prevent trades after Stop() is called)
	at.isRunningMutex.RLock()
	running := at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		logger.Infof("⏹ Trader is stopped, aborting cycle #%d", at.callCount)
		return nil
	}

	// Create decision record
	record := &store.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
	}

	// 1. Check if trading needs to be stopped
	if time.Now().Before(at.stopUntil) {
		remaining := at.stopUntil.Sub(time.Now())
		logger.Infof("⏸ Risk control: Trading paused, remaining %.0f minutes", remaining.Minutes())
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Risk control paused, remaining %.0f minutes", remaining.Minutes())
		at.saveDecision(record)
		return nil
	}

	// 2. Reset daily P&L (reset every day)
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		logger.Info("📅 Daily P&L reset")
	}

	// 4. Collect trading context
	ctx, err := at.buildTradingContext()
	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to build trading context: %v", err)
		at.saveDecision(record)
		return fmt.Errorf("failed to build trading context: %w", err)
	}

	// 如果没有候选币种，友好提示并跳过本周期
	if len(ctx.CandidateCoins) == 0 {
		logger.Infof("ℹ️  No candidate coins available, skipping this cycle")
		return nil
	}

	// Save equity snapshot independently (decoupled from AI decision, used for drawing profit curve)
	at.saveEquitySnapshot(ctx)

	logger.Info(strings.Repeat("=", 70))
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	logger.Infof("📊 Account equity: %.2f USDT | Available: %.2f USDT | Positions: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// 5. Use strategy engine to call AI for decision
	logger.Infof("🤖 Requesting AI analysis and decision... [Strategy Engine]")
	aiDecision, err := kernel.GetFullDecisionWithStrategy(ctx, at.mcpClient, at.strategyEngine, "balanced")

	if aiDecision != nil && aiDecision.AIRequestDurationMs > 0 {
		record.AIRequestDurationMs = aiDecision.AIRequestDurationMs
		logger.Infof("⏱️ AI call duration: %.2f seconds", float64(record.AIRequestDurationMs)/1000)
		record.ExecutionLog = append(record.ExecutionLog,
			fmt.Sprintf("AI call duration: %d ms", record.AIRequestDurationMs))
	}

	// Save chain of thought, decisions, and input prompt even if there's an error (for debugging)
	if aiDecision != nil {
		record.SystemPrompt = aiDecision.SystemPrompt // Save system prompt
		record.InputPrompt = aiDecision.UserPrompt
		record.CoTTrace = aiDecision.CoTTrace
		record.RawResponse = aiDecision.RawResponse // Save raw AI response for debugging
		if len(aiDecision.Decisions) > 0 {
			decisionJSON, _ := json.MarshalIndent(aiDecision.Decisions, "", "  ")
			record.DecisionJSON = string(decisionJSON)
		}
	}

	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("Failed to get AI decision: %v", err)

		// Print system prompt and AI chain of thought (output even with errors for debugging)
		if aiDecision != nil {
			logger.Info("\n" + strings.Repeat("=", 70) + "\n")
			logger.Infof("📋 System prompt (error case)")
			logger.Info(strings.Repeat("=", 70))
			logger.Info(aiDecision.SystemPrompt)
			logger.Info(strings.Repeat("=", 70))

			if aiDecision.CoTTrace != "" {
				logger.Info("\n" + strings.Repeat("-", 70) + "\n")
				logger.Info("💭 AI chain of thought analysis (error case):")
				logger.Info(strings.Repeat("-", 70))
				logger.Info(aiDecision.CoTTrace)
				logger.Info(strings.Repeat("-", 70))
			}
		}

		at.saveDecision(record)
		return fmt.Errorf("failed to get AI decision: %w", err)
	}

	// // 5. Print system prompt
	// logger.Infof("\n" + strings.Repeat("=", 70))
	// logger.Infof("📋 System prompt [template: %s]", at.systemPromptTemplate)
	// logger.Info(strings.Repeat("=", 70))
	// logger.Info(decision.SystemPrompt)
	// logger.Infof(strings.Repeat("=", 70) + "\n")

	// 6. Print AI chain of thought
	// logger.Infof("\n" + strings.Repeat("-", 70))
	// logger.Info("💭 AI chain of thought analysis:")
	// logger.Info(strings.Repeat("-", 70))
	// logger.Info(decision.CoTTrace)
	// logger.Infof(strings.Repeat("-", 70) + "\n")

	// 7. Print AI decisions
	// logger.Infof("📋 AI decision list (%d items):\n", len(kernel.Decisions))
	// for i, d := range kernel.Decisions {
	//     logger.Infof("  [%d] %s: %s - %s", i+1, d.Symbol, d.Action, d.Reasoning)
	//     if d.Action == "open_long" || d.Action == "open_short" {
	//        logger.Infof("      Leverage: %dx | Position: %.2f USDT | Stop loss: %.4f | Take profit: %.4f",
	//           d.Leverage, d.PositionSizeUSD, d.StopLoss, d.TakeProfit)
	//     }
	// }
	logger.Info()
	logger.Info(strings.Repeat("-", 70))
	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	logger.Info(strings.Repeat("-", 70))

	// 8. Sort decisions: ensure close positions first, then open positions (prevent position stacking overflow)
	sortedDecisions := sortDecisionsByPriority(aiDecision.Decisions)

	logger.Info("🔄 Execution order (optimized): Close positions first → Open positions later")
	for i, d := range sortedDecisions {
		logger.Infof("  [%d] %s %s", i+1, d.Symbol, d.Action)
	}
	logger.Info()

	// Check if trader is stopped before executing any decisions (prevent trades after Stop())
	at.isRunningMutex.RLock()
	running = at.isRunning
	at.isRunningMutex.RUnlock()
	if !running {
		logger.Infof("⏹ Trader stopped before decision execution, aborting cycle #%d", at.callCount)
		return nil
	}

	// Execute decisions and record results
	hasSuccessfulDecision := false
	for _, d := range sortedDecisions {
		// Check if trader is stopped before each decision (allow immediate stop during execution)
		at.isRunningMutex.RLock()
		running = at.isRunning
		at.isRunningMutex.RUnlock()
		if !running {
			logger.Infof("⏹ Trader stopped during decision execution, aborting remaining decisions")
			break
		}

		actionRecord := store.DecisionAction{
			Action:     d.Action,
			Symbol:     d.Symbol,
			Quantity:   0,
			Leverage:   d.Leverage,
			Price:      0,
			StopLoss:   d.StopLoss,
			TakeProfit: d.TakeProfit,
			Confidence: d.Confidence,
			Reasoning:  d.Reasoning,
			Timestamp:  time.Now().UTC(),
			Success:    false,
		}

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			logger.Infof("❌ Failed to execute decision (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("❌ %s %s failed: %v", d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			hasSuccessfulDecision = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("✓ %s %s succeeded", d.Symbol, d.Action))
			// Brief delay after successful execution
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 9. Save decision record
	if err := at.saveDecision(record); err != nil {
		logger.Infof("⚠ Failed to save decision record: %v", err)
	}

	// 10. Send account and position summary via Telegram (only if there were successful decisions)
	// 延迟发送，确保所有决策通知都已发送完成
	if hasSuccessfulDecision {
		// 等待一小段时间，确保所有决策通知都已发送
		time.Sleep(500 * time.Millisecond)
		at.sendAccountSummary()
	}

	return nil
}

// buildTradingContext builds trading context
func (at *AutoTrader) buildTradingContext() (*kernel.Context, error) {
	// 1. Get account information
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get account balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// 2. Get position information
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var positionInfos []kernel.PositionInfo
	totalMarginUsed := 0.0

	// Current position key set (for cleaning up closed position records)
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Skip closed positions (quantity = 0), prevent "ghost positions" from being passed to AI
		if quantity == 0 {
			continue
		}

		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		// Calculate margin used (estimated)
		leverage := 10 // Default value, should actually be fetched from position info
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// Calculate P&L percentage (based on margin, considering leverage)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		// Get position open time from exchange (preferred) or fallback to local tracking
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true

		var updateTime int64
		// Priority 1: Get from database (trader_positions table) - most accurate
		if at.store != nil {
			if dbPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, side); err == nil && dbPos != nil {
				if dbPos.EntryTime > 0 {
					updateTime = dbPos.EntryTime
				}
			}
		}
		// Priority 2: Get from exchange API (Bybit: createdTime, OKX: createdTime)
		if updateTime == 0 {
			if createdTime, ok := pos["createdTime"].(int64); ok && createdTime > 0 {
				updateTime = createdTime
			}
		}
		// Priority 3: Fallback to local tracking
		if updateTime == 0 {
			if _, exists := at.positionFirstSeenTime[posKey]; !exists {
				at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
			}
			updateTime = at.positionFirstSeenTime[posKey]
		}

		// Get peak profit rate for this position
		at.peakPnLCacheMutex.RLock()
		peakPnlPct := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		positionInfos = append(positionInfos, kernel.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnl,
			UnrealizedPnLPct: pnlPct,
			PeakPnLPct:       peakPnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
		})
	}

	// Clean up closed position records
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

	// 3. Use strategy engine to get candidate coins (must have strategy engine)
	if at.strategyEngine == nil {
		return nil, fmt.Errorf("trader has no strategy engine configured")
	}
	candidateCoins, err := at.strategyEngine.GetCandidateCoins()
	if err != nil {
		return nil, fmt.Errorf("failed to get candidate coins: %w", err)
	}
	logger.Infof("📋 [%s] Strategy engine fetched candidate coins: %d", at.name, len(candidateCoins))

	// 4. Calculate total P&L
	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	// 5. Get leverage from strategy config
	strategyConfig := at.strategyEngine.GetConfig()
	btcEthLeverage := strategyConfig.RiskControl.BTCETHMaxLeverage
	altcoinLeverage := strategyConfig.RiskControl.AltcoinMaxLeverage
	logger.Infof("📋 [%s] Strategy leverage config: BTC/ETH=%dx, Altcoin=%dx", at.name, btcEthLeverage, altcoinLeverage)

	// 6. Build context
	ctx := &kernel.Context{
		CurrentTime:     time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		BTCETHLeverage:  btcEthLeverage,
		AltcoinLeverage: altcoinLeverage,
		Account: kernel.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			UnrealizedPnL:    totalUnrealizedProfit,
			TotalPnL:         totalPnL,
			TotalPnLPct:      totalPnLPct,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:      positionInfos,
		CandidateCoins: candidateCoins,
	}

	// 7. Add recent closed trades (if store is available)
	if at.store != nil {
		// Get recent 10 closed trades for AI context
		recentTrades, err := at.store.Position().GetRecentTrades(at.id, 10)
		if err != nil {
			logger.Infof("⚠️ [%s] Failed to get recent trades: %v", at.name, err)
		} else {
			logger.Infof("📊 [%s] Found %d recent closed trades for AI context", at.name, len(recentTrades))
			for _, trade := range recentTrades {
				// Convert Unix timestamps to formatted strings for AI readability
				entryTimeStr := ""
				if trade.EntryTime > 0 {
					entryTimeStr = time.Unix(trade.EntryTime, 0).UTC().Format("01-02 15:04 UTC")
				}
				exitTimeStr := ""
				if trade.ExitTime > 0 {
					exitTimeStr = time.Unix(trade.ExitTime, 0).UTC().Format("01-02 15:04 UTC")
				}

				ctx.RecentOrders = append(ctx.RecentOrders, kernel.RecentOrder{
					Symbol:       trade.Symbol,
					Side:         trade.Side,
					EntryPrice:   trade.EntryPrice,
					ExitPrice:    trade.ExitPrice,
					RealizedPnL:  trade.RealizedPnL,
					PnLPct:       trade.PnLPct,
					EntryTime:    entryTimeStr,
					ExitTime:     exitTimeStr,
					HoldDuration: trade.HoldDuration,
				})
			}
		}
		// Get trading statistics for AI context
		stats, err := at.store.Position().GetFullStats(at.id)
		if err != nil {
			logger.Infof("⚠️ [%s] Failed to get trading stats: %v", at.name, err)
		} else if stats == nil {
			logger.Infof("⚠️ [%s] GetFullStats returned nil", at.name)
		} else if stats.TotalTrades == 0 {
			logger.Infof("⚠️ [%s] GetFullStats returned 0 trades (traderID=%s)", at.name, at.id)
		} else {
			ctx.TradingStats = &kernel.TradingStats{
				TotalTrades:    stats.TotalTrades,
				WinRate:        stats.WinRate,
				ProfitFactor:   stats.ProfitFactor,
				SharpeRatio:    stats.SharpeRatio,
				TotalPnL:       stats.TotalPnL,
				AvgWin:         stats.AvgWin,
				AvgLoss:        stats.AvgLoss,
				MaxDrawdownPct: stats.MaxDrawdownPct,
			}
			logger.Infof("📈 [%s] Trading stats: %d trades, %.1f%% win rate, PF=%.2f, Sharpe=%.2f, DD=%.1f%%",
				at.name, stats.TotalTrades, stats.WinRate, stats.ProfitFactor, stats.SharpeRatio, stats.MaxDrawdownPct)
		}
	} else {
		logger.Infof("⚠️ [%s] Store is nil, cannot get recent trades", at.name)
	}

	// 8. Get quantitative data (if enabled in strategy config)
	if strategyConfig.Indicators.EnableQuantData {
		// Collect symbols to query (candidate coins + position coins)
		symbolsToQuery := make(map[string]bool)
		for _, coin := range candidateCoins {
			symbolsToQuery[coin.Symbol] = true
		}
		for _, pos := range positionInfos {
			symbolsToQuery[pos.Symbol] = true
		}

		symbols := make([]string, 0, len(symbolsToQuery))
		for sym := range symbolsToQuery {
			symbols = append(symbols, sym)
		}

		logger.Infof("📊 [%s] Fetching quantitative data for %d symbols...", at.name, len(symbols))
		ctx.QuantDataMap = at.strategyEngine.FetchQuantDataBatch(symbols)
		logger.Infof("📊 [%s] Successfully fetched quantitative data for %d symbols", at.name, len(ctx.QuantDataMap))
	}

	// 9. Get OI ranking data (market-wide position changes)
	if strategyConfig.Indicators.EnableOIRanking {
		logger.Infof("📊 [%s] Fetching OI ranking data...", at.name)
		ctx.OIRankingData = at.strategyEngine.FetchOIRankingData()
		if ctx.OIRankingData != nil {
			logger.Infof("📊 [%s] OI ranking data ready: %d top, %d low positions",
				at.name, len(ctx.OIRankingData.TopPositions), len(ctx.OIRankingData.LowPositions))
		}
	}

	// 10. Get NetFlow ranking data (market-wide fund flow)
	if strategyConfig.Indicators.EnableNetFlowRanking {
		logger.Infof("💰 [%s] Fetching NetFlow ranking data...", at.name)
		ctx.NetFlowRankingData = at.strategyEngine.FetchNetFlowRankingData()
		if ctx.NetFlowRankingData != nil {
			logger.Infof("💰 [%s] NetFlow ranking data ready: inst_in=%d, inst_out=%d",
				at.name, len(ctx.NetFlowRankingData.InstitutionFutureTop), len(ctx.NetFlowRankingData.InstitutionFutureLow))
		}
	}

	// 11. Get Price ranking data (market-wide gainers/losers)
	if strategyConfig.Indicators.EnablePriceRanking {
		logger.Infof("📈 [%s] Fetching Price ranking data...", at.name)
		ctx.PriceRankingData = at.strategyEngine.FetchPriceRankingData()
		if ctx.PriceRankingData != nil {
			logger.Infof("📈 [%s] Price ranking data ready for %d durations",
				at.name, len(ctx.PriceRankingData.Durations))
		}
	}

	// 12. Set exchange credentials for accurate trading fee fetching
	ctx.ExchangeCredentials = at.getExchangeCredentials()

	return ctx, nil
}

// getExchangeCredentials returns the exchange credentials based on current exchange type
func (at *AutoTrader) getExchangeCredentials() *market.ExchangeCredentials {
	switch at.exchange {
	case "binance":
		if at.config.BinanceAPIKey != "" && at.config.BinanceSecretKey != "" {
			return &market.ExchangeCredentials{
				ExchangeType: "binance",
				APIKey:       at.config.BinanceAPIKey,
				SecretKey:    at.config.BinanceSecretKey,
			}
		}
	case "bybit":
		if at.config.BybitAPIKey != "" && at.config.BybitSecretKey != "" {
			return &market.ExchangeCredentials{
				ExchangeType: "bybit",
				APIKey:       at.config.BybitAPIKey,
				SecretKey:    at.config.BybitSecretKey,
			}
		}
	case "okx":
		if at.config.OKXAPIKey != "" && at.config.OKXSecretKey != "" {
			return &market.ExchangeCredentials{
				ExchangeType: "okx",
				APIKey:       at.config.OKXAPIKey,
				SecretKey:    at.config.OKXSecretKey,
			}
		}
	case "bitget":
		if at.config.BitgetAPIKey != "" && at.config.BitgetSecretKey != "" {
			return &market.ExchangeCredentials{
				ExchangeType: "bitget",
				APIKey:       at.config.BitgetAPIKey,
				SecretKey:    at.config.BitgetSecretKey,
			}
		}
		// DEX exchanges don't have traditional API key/secret for fee queries
		// case "hyperliquid", "aster", "lighter":
		//     These use wallet-based auth, trading fees are typically fixed/known
	}
	return nil
}

// executeDecisionWithRecord executes AI decision and records detailed information
func (at *AutoTrader) executeDecisionWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	var err error

	switch decision.Action {
	case "open_long":
		err = at.executeOpenLongWithRecord(decision, actionRecord)
		at.sendDecisionNotification(decision, actionRecord, err)
	case "open_short":
		err = at.executeOpenShortWithRecord(decision, actionRecord)
		at.sendDecisionNotification(decision, actionRecord, err)
	case "close_long":
		err = at.executeCloseLongWithRecord(decision, actionRecord)
		at.sendDecisionNotification(decision, actionRecord, err)
	case "close_short":
		err = at.executeCloseShortWithRecord(decision, actionRecord)
		at.sendDecisionNotification(decision, actionRecord, err)
	case "hold", "wait":
		// No execution needed, just record
		return nil
	default:
		return fmt.Errorf("unknown action: %s", decision.Action)
	}

	return err
}

// ExecuteDecision executes a trading decision from external sources (e.g., debate consensus)
// This is a public method that can be called by other modules
func (at *AutoTrader) ExecuteDecision(d *kernel.Decision) error {
	logger.Infof("[%s] Executing external decision: %s %s", at.name, d.Action, d.Symbol)

	// Create a minimal action record for tracking
	actionRecord := &store.DecisionAction{
		Symbol:     d.Symbol,
		Action:     d.Action,
		Leverage:   d.Leverage,
		StopLoss:   d.StopLoss,
		TakeProfit: d.TakeProfit,
		Confidence: d.Confidence,
		Reasoning:  d.Reasoning,
	}

	// Execute the decision
	err := at.executeDecisionWithRecord(d, actionRecord)
	if err != nil {
		logger.Errorf("[%s] External decision execution failed: %v", at.name, err)
		return err
	}

	logger.Infof("[%s] External decision executed successfully: %s %s", at.name, d.Action, d.Symbol)
	return nil
}

// executeOpenLongWithRecord executes open long position and records detailed information
func (at *AutoTrader) executeOpenLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📈 Open long: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
			return fmt.Errorf("❌ %s already has long position, close it first", decision.Symbol)
		}
	}

	// Get current price
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_long", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_long"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Set stop loss and take profit
	if err := at.trader.SetStopLoss(decision.Symbol, "LONG", quantity, decision.StopLoss); err != nil {
		logger.Infof("  ⚠ Failed to set stop loss: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "LONG", quantity, decision.TakeProfit); err != nil {
		logger.Infof("  ⚠ Failed to set take profit: %v", err)
	}

	return nil
}

// executeOpenShortWithRecord executes open short position and records detailed information
func (at *AutoTrader) executeOpenShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  📉 Open short: %s", decision.Symbol)

	// ⚠️ Get current positions for multiple checks
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}

	// [CODE ENFORCED] Check max positions limit
	if err := at.enforceMaxPositions(len(positions)); err != nil {
		return err
	}

	// Check if there's already a position in the same symbol and direction
	for _, pos := range positions {
		if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
			return fmt.Errorf("❌ %s already has short position, close it first", decision.Symbol)
		}
	}

	// Get current price
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}

	// Get balance (needed for multiple checks)
	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to get account balance: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Get equity for position value ratio check
	equity := 0.0
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		equity = eq
	} else if eq, ok := balance["totalWalletBalance"].(float64); ok && eq > 0 {
		equity = eq
	} else {
		equity = availableBalance // Fallback to available balance
	}

	// [CODE ENFORCED] Position Value Ratio Check: position_value <= equity × ratio
	adjustedPositionSize, wasCapped := at.enforcePositionValueRatio(decision.PositionSizeUSD, equity, decision.Symbol)
	if wasCapped {
		decision.PositionSizeUSD = adjustedPositionSize
	}

	// ⚠️ Auto-adjust position size if insufficient margin
	// Formula: totalRequired = positionSize/leverage + positionSize*0.001 + positionSize/leverage*0.01
	//        = positionSize * (1.01/leverage + 0.001)
	marginFactor := 1.01/float64(decision.Leverage) + 0.001
	maxAffordablePositionSize := availableBalance / marginFactor

	actualPositionSize := decision.PositionSizeUSD
	if actualPositionSize > maxAffordablePositionSize {
		// Use 98% of max to leave buffer for price fluctuation
		adjustedSize := maxAffordablePositionSize * 0.98
		logger.Infof("  ⚠️ Position size %.2f exceeds max affordable %.2f, auto-reducing to %.2f",
			actualPositionSize, maxAffordablePositionSize, adjustedSize)
		actualPositionSize = adjustedSize
		decision.PositionSizeUSD = actualPositionSize
	}

	// [CODE ENFORCED] Minimum position size check
	if err := at.enforceMinPositionSize(decision.PositionSizeUSD); err != nil {
		return err
	}

	// Calculate quantity with adjusted position size
	quantity := actualPositionSize / marketData.CurrentPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = marketData.CurrentPrice

	// Set margin mode
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		logger.Infof("  ⚠️ Failed to set margin mode: %v", err)
		// Continue execution, doesn't affect trading
	}

	// Open position
	order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
	if err != nil {
		return err
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	logger.Infof("  ✓ Position opened successfully, order ID: %v, quantity: %.4f", order["orderId"], quantity)

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "open_short", quantity, marketData.CurrentPrice, decision.Leverage, 0)

	// Record position opening time
	posKey := decision.Symbol + "_short"
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// Set stop loss and take profit
	if err := at.trader.SetStopLoss(decision.Symbol, "SHORT", quantity, decision.StopLoss); err != nil {
		logger.Infof("  ⚠ Failed to set stop loss: %v", err)
	}
	if err := at.trader.SetTakeProfit(decision.Symbol, "SHORT", quantity, decision.TakeProfit); err != nil {
		logger.Infof("  ⚠ Failed to set take profit: %v", err)
	}

	return nil
}

// executeCloseLongWithRecord executes close long position and records detailed information
func (at *AutoTrader) executeCloseLongWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close long: %s", decision.Symbol)

	// Get current price
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "LONG"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok && amt > 0 {
						quantity = amt
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Record quantity for notification pnl calculation
	actionRecord.Quantity = quantity

	// Check if position is already being closed by stop-loss/drawdown system (conflict detection)
	posKey := decision.Symbol + "_long"
	at.closingPositionsMutex.Lock()
	if at.closingPositions[posKey] {
		at.closingPositionsMutex.Unlock()
		logger.Infof("⚠️  Position %s long is being closed by stop-loss/drawdown system, AI close decision skipped", decision.Symbol)
		return fmt.Errorf("position %s long is being closed by stop-loss/drawdown system", decision.Symbol)
	}
	// Mark as closing
	at.closingPositions[posKey] = true
	at.closingPositionsMutex.Unlock()

	// Ensure we clear the flag when done
	defer func() {
		at.closingPositionsMutex.Lock()
		delete(at.closingPositions, posKey)
		at.closingPositionsMutex.Unlock()
	}()

	// Close position
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = close all
	if err != nil {
		// Check if error is due to position not existing (already closed by stop-loss/drawdown)
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "no position") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "already closed") {
			logger.Infof("ℹ️  Position %s long already closed (may have been closed by stop-loss/drawdown), AI close decision skipped", decision.Symbol)
			return nil // Not an error, position was already closed
		}
		return err
	}
	// Check if order result indicates no position
	if status, ok := order["status"].(string); ok && status == "NO_POSITION" {
		logger.Infof("ℹ️  Position %s long already closed (NO_POSITION status), AI close decision skipped", decision.Symbol)
		return nil
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_long", quantity, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// executeCloseShortWithRecord executes close short position and records detailed information
func (at *AutoTrader) executeCloseShortWithRecord(decision *kernel.Decision, actionRecord *store.DecisionAction) error {
	logger.Infof("  🔄 Close short: %s", decision.Symbol)

	// Get current price
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// Normalize symbol for database lookup
	normalizedSymbol := market.Normalize(decision.Symbol)

	// Get entry price and quantity - prioritize local database for accurate quantity
	var entryPrice float64
	var quantity float64

	// First try to get from local database (more accurate for quantity)
	if at.store != nil {
		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, "SHORT"); err == nil && openPos != nil {
			quantity = openPos.Quantity
			entryPrice = openPos.EntryPrice
			logger.Infof("  📊 Using local position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
		}
	}

	// Fallback to exchange API if local data not found
	if quantity == 0 {
		positions, err := at.trader.GetPositions()
		if err == nil {
			for _, pos := range positions {
				if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
					if ep, ok := pos["entryPrice"].(float64); ok {
						entryPrice = ep
					}
					if amt, ok := pos["positionAmt"].(float64); ok {
						quantity = -amt // positionAmt is negative for short
					}
					break
				}
			}
		}
		logger.Infof("  📊 Using exchange position data: qty=%.8f, entry=%.2f", quantity, entryPrice)
	}

	// Record quantity for notification pnl calculation
	actionRecord.Quantity = quantity

	// Check if position is already being closed by stop-loss/drawdown system (conflict detection)
	posKey := decision.Symbol + "_short"
	at.closingPositionsMutex.Lock()
	if at.closingPositions[posKey] {
		at.closingPositionsMutex.Unlock()
		logger.Infof("⚠️  Position %s short is being closed by stop-loss/drawdown system, AI close decision skipped", decision.Symbol)
		return fmt.Errorf("position %s short is being closed by stop-loss/drawdown system", decision.Symbol)
	}
	// Mark as closing
	at.closingPositions[posKey] = true
	at.closingPositionsMutex.Unlock()

	// Ensure we clear the flag when done
	defer func() {
		at.closingPositionsMutex.Lock()
		delete(at.closingPositions, posKey)
		at.closingPositionsMutex.Unlock()
	}()

	// Close position
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = close all
	if err != nil {
		// Check if error is due to position not existing (already closed by stop-loss/drawdown)
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "no position") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "already closed") {
			logger.Infof("ℹ️  Position %s short already closed (may have been closed by stop-loss/drawdown), AI close decision skipped", decision.Symbol)
			return nil // Not an error, position was already closed
		}
		return err
	}
	// Check if order result indicates no position
	if status, ok := order["status"].(string); ok && status == "NO_POSITION" {
		logger.Infof("ℹ️  Position %s short already closed (NO_POSITION status), AI close decision skipped", decision.Symbol)
		return nil
	}

	// Record order ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	// Record order to database and poll for confirmation
	at.recordAndConfirmOrder(order, decision.Symbol, "close_short", quantity, marketData.CurrentPrice, 0, entryPrice)

	logger.Infof("  ✓ Position closed successfully")
	return nil
}

// GetID gets trader ID
func (at *AutoTrader) GetID() string {
	return at.id
}

// GetUnderlyingTrader returns the underlying Trader interface implementation
// This is used by grid trading and other components that need direct exchange access
func (at *AutoTrader) GetUnderlyingTrader() Trader {
	return at.trader
}

// GetName gets trader name
func (at *AutoTrader) GetName() string {
	return at.name
}

// GetAIModel gets AI model
func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

// GetExchange gets exchange
func (at *AutoTrader) GetExchange() string {
	return at.exchange
}

// GetShowInCompetition returns whether trader should be shown in competition
func (at *AutoTrader) GetShowInCompetition() bool {
	return at.showInCompetition
}

// SetShowInCompetition sets whether trader should be shown in competition
func (at *AutoTrader) SetShowInCompetition(show bool) {
	at.showInCompetition = show
}

// SetCustomPrompt sets custom trading strategy prompt
func (at *AutoTrader) SetCustomPrompt(prompt string) {
	at.customPrompt = prompt
}

// SetOverrideBasePrompt sets whether to override base prompt
func (at *AutoTrader) SetOverrideBasePrompt(override bool) {
	at.overrideBasePrompt = override
}

// GetSystemPromptTemplate gets current system prompt template name (from strategy config)
func (at *AutoTrader) GetSystemPromptTemplate() string {
	if at.strategyEngine != nil {
		config := at.strategyEngine.GetConfig()
		if config.CustomPrompt != "" {
			return "custom"
		}
	}
	return "strategy"
}

// saveEquitySnapshot saves equity snapshot independently (for drawing profit curve, decoupled from AI decision)
func (at *AutoTrader) saveEquitySnapshot(ctx *kernel.Context) {
	if at.store == nil || ctx == nil {
		return
	}

	snapshot := &store.EquitySnapshot{
		TraderID:      at.id,
		Timestamp:     time.Now().UTC(),
		TotalEquity:   ctx.Account.TotalEquity,
		Balance:       ctx.Account.TotalEquity - ctx.Account.UnrealizedPnL,
		UnrealizedPnL: ctx.Account.UnrealizedPnL,
		PositionCount: ctx.Account.PositionCount,
		MarginUsedPct: ctx.Account.MarginUsedPct,
	}

	if err := at.store.Equity().Save(snapshot); err != nil {
		logger.Infof("⚠️ Failed to save equity snapshot: %v", err)
	}
}

// saveDecision saves AI decision log to database (only records AI input/output, for debugging)
func (at *AutoTrader) saveDecision(record *store.DecisionRecord) error {
	if at.store == nil {
		return nil
	}

	at.cycleNumber++
	record.CycleNumber = at.cycleNumber
	record.TraderID = at.id

	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	if err := at.store.Decision().LogDecision(record); err != nil {
		logger.Infof("⚠️ Failed to save decision record: %v", err)
		return err
	}

	logger.Infof("📝 Decision record saved: trader=%s, cycle=%d", at.id, at.cycleNumber)
	return nil
}

// GetStore gets data store (for external access to decision records, etc.)
func (at *AutoTrader) GetStore() *store.Store {
	return at.store
}

// GetStatus gets system status (for API)
func (at *AutoTrader) GetStatus() map[string]interface{} {
	aiProvider := "DeepSeek"
	if at.config.UseQwen {
		aiProvider = "Qwen"
	}

	at.isRunningMutex.RLock()
	isRunning := at.isRunning
	at.isRunningMutex.RUnlock()

	result := map[string]interface{}{
		"trader_id":       at.id,
		"trader_name":     at.name,
		"ai_model":        at.aiModel,
		"exchange":        at.exchange,
		"is_running":      isRunning,
		"start_time":      at.startTime.Format(time.RFC3339),
		"runtime_minutes": int(time.Since(at.startTime).Minutes()),
		"call_count":      at.callCount,
		"initial_balance": at.initialBalance,
		"scan_interval":   at.config.ScanInterval.String(),
		"stop_until":      at.stopUntil.Format(time.RFC3339),
		"last_reset_time": at.lastResetTime.Format(time.RFC3339),
		"ai_provider":     aiProvider,
	}

	// Add strategy info
	if at.config.StrategyConfig != nil {
		result["strategy_type"] = at.config.StrategyConfig.StrategyType
		if at.config.StrategyConfig.GridConfig != nil {
			result["grid_symbol"] = at.config.StrategyConfig.GridConfig.Symbol
		}
	}

	return result
}

// GetAccountInfo gets account information (for API)
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	// Get account fields
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0
	totalEquity := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Use totalEquity directly if provided by trader (more accurate)
	if eq, ok := balance["totalEquity"].(float64); ok && eq > 0 {
		totalEquity = eq
	} else {
		// Fallback: Total Equity = Wallet balance + Unrealized profit
		totalEquity = totalWalletBalance + totalUnrealizedProfit
	}

	// Get positions to calculate total margin
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	totalMarginUsed := 0.0
	totalUnrealizedPnLCalculated := 0.0
	for _, pos := range positions {
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnLCalculated += unrealizedPnl

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	// Verify unrealized P&L consistency (API value vs calculated from positions)
	// Note: Lighter API may return 0 for unrealized PnL, this is a known limitation
	diff := math.Abs(totalUnrealizedProfit - totalUnrealizedPnLCalculated)
	if diff > 5.0 { // Only warn if difference is significant (> 5 USDT)
		logger.Infof("⚠️ Unrealized P&L inconsistency (Lighter API limitation): API=%.4f, Calculated=%.4f, Diff=%.4f",
			totalUnrealizedProfit, totalUnrealizedPnLCalculated, diff)
	}

	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	} else {
		logger.Infof("⚠️ Initial Balance abnormal: %.2f, cannot calculate P&L percentage", at.initialBalance)
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	return map[string]interface{}{
		// Core fields
		"total_equity":      totalEquity,           // Account equity = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // Wallet balance (excluding unrealized P&L)
		"unrealized_profit": totalUnrealizedProfit, // Unrealized P&L (official value from exchange API)
		"available_balance": availableBalance,      // Available balance

		// P&L statistics
		"total_pnl":       totalPnL,          // Total P&L = equity - initial
		"total_pnl_pct":   totalPnLPct,       // Total P&L percentage
		"initial_balance": at.initialBalance, // Initial balance
		"daily_pnl":       at.dailyPnL,       // Daily P&L

		// Position information
		"position_count":  len(positions),  // Position count
		"margin_used":     totalMarginUsed, // Margin used
		"margin_used_pct": marginUsedPct,   // Margin usage rate
	}, nil
}

// GetPositions gets position list (for API)
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		// Calculate margin used
		marginUsed := (quantity * markPrice) / float64(leverage)

		// Calculate P&L percentage (based on margin)
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
		})
	}

	return result, nil
}

// calculatePnLPercentage calculates P&L percentage (based on margin, automatically considers leverage)
// Return rate = Unrealized P&L / Margin × 100%
func calculatePnLPercentage(unrealizedPnl, marginUsed float64) float64 {
	if marginUsed > 0 {
		return (unrealizedPnl / marginUsed) * 100
	}
	return 0.0
}

// sortDecisionsByPriority sorts decisions: close positions first, then open positions, finally hold/wait
// This avoids position stacking overflow when changing positions
func sortDecisionsByPriority(decisions []kernel.Decision) []kernel.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// Define priority
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short":
			return 1 // Highest priority: close positions first
		case "open_long", "open_short":
			return 2 // Second priority: open positions later
		case "hold", "wait":
			return 3 // Lowest priority: wait
		default:
			return 999 // Unknown actions at the end
		}
	}

	// Copy decision list
	sorted := make([]kernel.Decision, len(decisions))
	copy(sorted, decisions)

	// Sort by priority
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// startDrawdownMonitor starts drawdown monitoring with dynamic frequency
// 动态监控频率：根据持仓最大利润水平调整检查间隔
func (at *AutoTrader) startDrawdownMonitor() {
	at.monitorWg.Add(1)
	go func() {
		defer at.monitorWg.Done()

		// Initial check interval (will be adjusted dynamically)
		checkInterval := 1 * time.Minute
		timer := time.NewTimer(checkInterval)
		defer timer.Stop()

		logger.Info("📊 Started position drawdown monitoring (dynamic frequency: 30s/45s/60s based on profit level)")

		for {
			select {
			case <-timer.C:
				// Perform drawdown check and get maximum profit level
				maxProfitPct := at.checkPositionDrawdown()

				// Calculate next check interval based on maximum profit
				nextInterval := getDrawdownCheckInterval(maxProfitPct)

				// Update timer if interval changed
				if nextInterval != checkInterval {
					checkInterval = nextInterval
					logger.Infof("🔄 Drawdown monitoring frequency adjusted: %.0fs (max profit: %.2f%%)",
						checkInterval.Seconds(), maxProfitPct)
				}

				// Reset timer for next check
				timer.Reset(checkInterval)

			case <-at.stopMonitorCh:
				logger.Info("⏹ Stopped position drawdown monitoring")
				return
			}
		}
	}()
}

// getTieredDrawdownThreshold returns drawdown threshold based on profit level (tiered drawdown strategy)
// 分级回撤策略（修正逻辑）：利润越高，回调风险越高，应该用更严格的阈值
// 重要：回撤是从峰值计算的，所以必须基于峰值确定阈值
// 但峰值越高，阈值应该更严格（百分比更小），以保护高利润
func getTieredDrawdownThreshold(profitPct float64) float64 {
	if profitPct >= 25.0 {
		return 30.0 // 利润>=25%，回撤30%（严格：峰值越高，阈值越严格，保护高利润）
	} else if profitPct >= 12.0 {
		return 30.0 // 利润12-25%，回撤30%（标准保护）
	} else if profitPct >= 6.0 {
		return 25.0 // 利润6-12%，回撤25%（保守保护）
	}
	return 0.0 // 利润<6%，不触发回撤保护
}

// getDrawdownCheckInterval returns dynamic check interval based on maximum profit level
// 动态监控频率：利润越高，检查越频繁（降低回撤风险）
func getDrawdownCheckInterval(maxProfitPct float64) time.Duration {
	if maxProfitPct >= 20.0 {
		return 30 * time.Second // 高利润（≥20%）：30秒检查（更频繁，及时保护）
	} else if maxProfitPct >= 10.0 {
		return 45 * time.Second // 中等利润（10-20%）：45秒检查（平衡）
	} else {
		return 1 * time.Minute // 低利润（<10%）：1分钟检查（标准频率）
	}
}

// checkPositionDrawdown checks position drawdown situation
// 优化：使用分级回撤策略 + 混合利润计算（价格变化实时监控 + 实际盈亏最终验证）
// 返回：最大利润水平（用于动态调整检查频率）
func (at *AutoTrader) checkPositionDrawdown() float64 {
	// Get current positions (with cache for efficiency)
	positions, err := at.trader.GetPositions()
	if err != nil {
		logger.Infof("❌ Drawdown monitoring: failed to get positions: %v", err)
		return 0.0
	}

	maxProfitPct := 0.0 // Track maximum profit across all positions

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // Short position quantity is negative, convert to positive
		}

		// Calculate current P&L percentage using price change (real-time, no cache delay)
		leverage := 10 // Default value
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		var currentPnLPct float64
		if side == "long" {
			currentPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			currentPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// Construct unique position identifier (distinguish long/short)
		posKey := symbol + "_" + side

		// Get historical peak profit for this position
		at.peakPnLCacheMutex.RLock()
		peakPnLPct, exists := at.peakPnLCache[posKey]
		at.peakPnLCacheMutex.RUnlock()

		if !exists {
			// If no historical peak record, use current P&L as initial value
			peakPnLPct = currentPnLPct
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		} else {
			// Update peak cache
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		}

		// Calculate drawdown (magnitude of decline from peak)
		var drawdownPct float64
		if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
			drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
		}

		// Get tiered drawdown threshold based on peak profit level (not current profit)
		// 重要：必须基于峰值利润确定阈值，因为回撤是从峰值计算的：drawdown = (peak - current) / peak
		// 逻辑：峰值越高，回调风险越高，阈值应该更严格（百分比更小），以保护高利润
		drawdownThreshold := getTieredDrawdownThreshold(peakPnLPct)

		// Check if close to trigger threshold (using price-based calculation for real-time monitoring)
		// 使用价格变化进行实时监控，如果接近触发阈值，再用实际盈亏验证
		// 注意：触发门槛从4.5%提高到5.5%，与getTieredDrawdownThreshold的6%门槛保持一致
		needsVerification := currentPnLPct >= 5.5 && drawdownPct >= (drawdownThreshold-2.0) && drawdownThreshold > 0

		if needsVerification {
			// 接近触发阈值，强制刷新缓存获取最新实际盈亏进行验证
			// 注意：由于trader接口没有暴露清除缓存的方法，这里通过重新获取来刷新
			// 如果缓存未过期，可能需要等待；但通常回撤监控每分钟检查一次，缓存应该已更新
			refreshPositions, err := at.trader.GetPositions()
			if err == nil {
				// 找到对应的持仓
				for _, refreshPos := range refreshPositions {
					if refreshPos["symbol"].(string) == symbol && refreshPos["side"].(string) == side {
						// 使用实际盈亏进行最终验证（更准确，考虑手续费、资金费率等）
						unrealizedPnl := refreshPos["unRealizedProfit"].(float64)
						refreshQuantity := refreshPos["positionAmt"].(float64)
						if refreshQuantity < 0 {
							refreshQuantity = -refreshQuantity
						}
						refreshMarkPrice := refreshPos["markPrice"].(float64)
						refreshLeverage := 10
						if lev, ok := refreshPos["leverage"].(float64); ok {
							refreshLeverage = int(lev)
						}
						marginUsed := (refreshQuantity * refreshMarkPrice) / float64(refreshLeverage)
						realPnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

						// 使用实际盈亏重新计算回撤
						at.peakPnLCacheMutex.RLock()
						refreshPeakPnlPct := at.peakPnLCache[posKey]
						at.peakPnLCacheMutex.RUnlock()

						var realDrawdownPct float64
						if refreshPeakPnlPct > 0 && realPnlPct < refreshPeakPnlPct {
							realDrawdownPct = ((refreshPeakPnlPct - realPnlPct) / refreshPeakPnlPct) * 100
						}

						// 使用实际盈亏和分级回撤阈值进行最终判断
						// 重要：必须基于峰值利润确定阈值，因为回撤是从峰值计算的
						// 逻辑：峰值越高，回调风险越高，阈值应该更严格（百分比更小）
						// 例如：峰值16.42%应使用12-25%区间的阈值（30%），而不是当前利润6.78%对应的阈值
						realDrawdownThreshold := getTieredDrawdownThreshold(refreshPeakPnlPct)
						if realPnlPct >= 6.0 && realDrawdownPct >= realDrawdownThreshold && realDrawdownThreshold > 0 {
							// 记录平仓前的持仓信息（用于打印盈亏）
							entryPrice := refreshPos["entryPrice"].(float64)
							closeQuantity := refreshQuantity
							
							logger.Infof("🚨 Drawdown close position condition triggered (tiered): %s %s | Real profit: %.2f%% | Peak profit: %.2f%% | Drawdown: %.2f%% (threshold: %.2f%%)",
								symbol, side, realPnlPct, refreshPeakPnlPct, realDrawdownPct, realDrawdownThreshold)

							// Execute close position (risk control has absolute priority)
							if err := at.emergencyClosePosition(symbol, side, true); err != nil {
								logger.Infof("❌ Drawdown close position failed (%s %s): %v", symbol, side, err)
								// Send Telegram notification for close failure
								failureDetails := map[string]interface{}{
									"warning_type": "平仓失败",
									"current_profit": realPnlPct,
									"peak_profit": refreshPeakPnlPct,
									"drawdown": realDrawdownPct,
									"threshold": realDrawdownThreshold,
									"error": err.Error(),
								}
								at.sendDrawdownWarningNotification(symbol, side, failureDetails)
							} else {
								// 计算并打印平仓盈亏信息
								// 注意：unrealizedPnl 是平仓前的未实现盈亏，平仓后这个值会变成已实现盈亏
								// 但由于平仓可能涉及滑点和手续费，实际盈亏可能略有差异
								realizedPnl := unrealizedPnl
								realizedPnlPct := realPnlPct
								
								logger.Infof("✅ Drawdown close position succeeded: %s %s | Realized PnL: %.2f USDT (%.2f%%) | Entry: %.6f | Exit: %.6f | Qty: %.6f | Margin: %.2f USDT",
									symbol, side, realizedPnl, realizedPnlPct, entryPrice, refreshMarkPrice, closeQuantity, marginUsed)
								
								// Send Telegram notification
								notificationDetails := map[string]interface{}{
									"entry_price": entryPrice,
									"exit_price":  refreshMarkPrice,
									"quantity":    closeQuantity,
									"margin":      marginUsed,
									"pnl":         realizedPnl,
									"pnl_pct":     realizedPnlPct,
									"peak_profit": refreshPeakPnlPct,
									"drawdown":    realDrawdownPct,
								}
								at.sendRiskControlCloseNotification(symbol, side, "回撤止损 (Drawdown Stop-Loss)", notificationDetails)
								
								// Clear cache for this position after closing
								at.ClearPeakPnLCache(symbol, side)
							}
						} else {
							// 实际盈亏验证未触发，记录调试信息
							logger.Infof("📊 Drawdown monitoring (verified): %s %s | Price-based: %.2f%% (drawdown: %.2f%%) | Real PnL: %.2f%% (drawdown: %.2f%%, threshold: %.2f%%)",
								symbol, side, currentPnLPct, drawdownPct, realPnlPct, realDrawdownPct, realDrawdownThreshold)
							// Send Telegram notification for drawdown warning (close to threshold)
							warningDetails := map[string]interface{}{
								"warning_type": "接近回撤阈值",
								"current_profit": currentPnLPct,
								"peak_profit": refreshPeakPnlPct,
								"drawdown": drawdownPct,
								"threshold": realDrawdownThreshold,
								"real_pnl": realPnlPct,
								"real_drawdown": realDrawdownPct,
							}
							at.sendDrawdownWarningNotification(symbol, side, warningDetails)
						}
						break
					}
				}
			}
		} else if currentPnLPct >= 6.0 {
			// Record situations close to close position condition (for debugging)
			// 注意：记录门槛从5%提高到6%，与触发门槛保持一致
			logger.Infof("📊 Drawdown monitoring: %s %s | Profit: %.2f%% | Peak: %.2f%% | Drawdown: %.2f%% (threshold: %.2f%%)",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct, drawdownThreshold)
			// Send Telegram notification for high profit monitoring (only if drawdown is significant)
			// 只在回撤较大时发送通知，避免通知过于频繁
			if drawdownPct > 0 && drawdownPct >= (drawdownThreshold*0.5) && drawdownThreshold > 0 {
				monitoringDetails := map[string]interface{}{
					"warning_type": "回撤监控",
					"current_profit": currentPnLPct,
					"peak_profit": peakPnLPct,
					"drawdown": drawdownPct,
					"threshold": drawdownThreshold,
				}
				at.sendDrawdownWarningNotification(symbol, side, monitoringDetails)
			}
		}

		// Track maximum profit for dynamic frequency adjustment
		if currentPnLPct > maxProfitPct {
			maxProfitPct = currentPnLPct
		}
	}

	return maxProfitPct
}

// emergencyClosePosition emergency close position function with conflict detection
// 紧急平仓函数，带冲突检测：防止与AI决策、自动回撤、自动止损重复平仓
// isRiskControl: true表示风控系统调用（监控系统），绝对优先，可以打断AI决策；false表示AI决策调用
func (at *AutoTrader) emergencyClosePosition(symbol, side string, isRiskControl bool) error {
	posKey := symbol + "_" + side

	// Risk control system has absolute priority: can interrupt AI decisions
	// 风控系统绝对优先：可以打断AI决策的平仓
	at.closingPositionsMutex.Lock()
	if at.closingPositions[posKey] {
		if isRiskControl {
			// Risk control system: force interrupt AI decision and execute close
			// 风控系统：强制打断AI决策并执行平仓
			logger.Infof("🛡️  Risk control system interrupting AI close decision for %s %s (risk control has absolute priority)", symbol, side)
			// Clear the flag and mark as closing by risk control
			delete(at.closingPositions, posKey)
			at.closingPositions[posKey] = true
			at.closingPositionsMutex.Unlock()
		} else {
			// AI decision: skip if risk control is closing
			// AI决策：如果风控系统正在平仓，则跳过
			at.closingPositionsMutex.Unlock()
			logger.Infof("⚠️  Position %s %s is being closed by risk control system (stop-loss/drawdown), AI close decision skipped", symbol, side)
			return fmt.Errorf("position %s %s is being closed by risk control system", symbol, side)
		}
	} else {
		// Mark as closing
		at.closingPositions[posKey] = true
		at.closingPositionsMutex.Unlock()
	}

	// Ensure we clear the flag when done (even if error occurs)
	defer func() {
		at.closingPositionsMutex.Lock()
		delete(at.closingPositions, posKey)
		at.closingPositionsMutex.Unlock()
	}()

	// Verify position still exists before closing (may have been closed by AI decision or other system)
	positions, err := at.trader.GetPositions()
	if err == nil {
		positionExists := false
		for _, pos := range positions {
			if pos["symbol"].(string) == symbol && pos["side"].(string) == side {
				if amt, ok := pos["positionAmt"].(float64); ok {
					if side == "long" && amt > 0 {
						positionExists = true
					} else if side == "short" && amt < 0 {
						positionExists = true
					}
				}
				break
			}
		}
		if !positionExists {
			logger.Infof("ℹ️  Position %s %s no longer exists (may have been closed by AI decision/stop-loss/drawdown), skipping emergency close", symbol, side)
			return nil // Not an error, position was already closed
		}
	}

	// Execute close
	switch side {
	case "long":
		order, err := at.trader.CloseLong(symbol, 0) // 0 = close all
		if err != nil {
			// Check if error is due to position not existing (already closed)
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "no position") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "already closed") {
				logger.Infof("ℹ️  Position %s %s already closed (no position error), emergency close skipped", symbol, side)
				return nil // Not an error, position was already closed
			}
			return err
		}
		// Check if order result indicates no position
		if status, ok := order["status"].(string); ok && status == "NO_POSITION" {
			logger.Infof("ℹ️  Position %s %s already closed (NO_POSITION status), emergency close skipped", symbol, side)
			return nil
		}
		logger.Infof("✅ Emergency close long position succeeded, order ID: %v", order["orderId"])
	case "short":
		order, err := at.trader.CloseShort(symbol, 0) // 0 = close all
		if err != nil {
			// Check if error is due to position not existing (already closed)
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "no position") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "already closed") {
				logger.Infof("ℹ️  Position %s %s already closed (no position error), emergency close skipped", symbol, side)
				return nil // Not an error, position was already closed
			}
			return err
		}
		// Check if order result indicates no position
		if status, ok := order["status"].(string); ok && status == "NO_POSITION" {
			logger.Infof("ℹ️  Position %s %s already closed (NO_POSITION status), emergency close skipped", symbol, side)
			return nil
		}
		logger.Infof("✅ Emergency close short position succeeded, order ID: %v", order["orderId"])
	default:
		return fmt.Errorf("unknown position direction: %s", side)
	}

	return nil
}

// GetPeakPnLCache gets peak profit cache
func (at *AutoTrader) GetPeakPnLCache() map[string]float64 {
	at.peakPnLCacheMutex.RLock()
	defer at.peakPnLCacheMutex.RUnlock()

	// Return a copy of the cache
	cache := make(map[string]float64)
	for k, v := range at.peakPnLCache {
		cache[k] = v
	}
	return cache
}

// UpdatePeakPnL updates peak profit cache
func (at *AutoTrader) UpdatePeakPnL(symbol, side string, currentPnLPct float64) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	if peak, exists := at.peakPnLCache[posKey]; exists {
		// Update peak (if long, take larger value; if short, currentPnLPct is negative, also compare)
		if currentPnLPct > peak {
			at.peakPnLCache[posKey] = currentPnLPct
		}
	} else {
		// First time recording
		at.peakPnLCache[posKey] = currentPnLPct
	}
}

// ClearPeakPnLCache clears peak cache for specified position
func (at *AutoTrader) ClearPeakPnLCache(symbol, side string) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	posKey := symbol + "_" + side
	delete(at.peakPnLCache, posKey)
}

// recordAndConfirmOrder polls order status for actual fill data and records position
// action: open_long, open_short, close_long, close_short
// entryPrice: entry price when closing (0 when opening)
func (at *AutoTrader) recordAndConfirmOrder(orderResult map[string]interface{}, symbol, action string, quantity float64, price float64, leverage int, entryPrice float64) {
	if at.store == nil {
		return
	}

	// Get order ID (supports multiple types)
	var orderID string
	switch v := orderResult["orderId"].(type) {
	case int64:
		orderID = fmt.Sprintf("%d", v)
	case float64:
		orderID = fmt.Sprintf("%.0f", v)
	case string:
		orderID = v
	default:
		orderID = fmt.Sprintf("%v", v)
	}

	if orderID == "" || orderID == "0" {
		logger.Infof("  ⚠️ Order ID is empty, skipping record")
		return
	}

	// Determine positionSide
	var positionSide string
	switch action {
	case "open_long", "close_long":
		positionSide = "LONG"
	case "open_short", "close_short":
		positionSide = "SHORT"
	}

	var actualPrice = price
	var actualQty = quantity
	var fee float64

	// Exchanges with OrderSync: Skip immediate order recording, let OrderSync handle it
	// This ensures accurate data from GetTrades API and avoids duplicate records
	switch at.exchange {
	case "binance", "lighter", "hyperliquid", "bybit", "okx", "bitget", "aster":
		logger.Infof("  📝 Order submitted (id: %s), will be synced by OrderSync", orderID)
		return
	}

	// For exchanges without OrderSync (e.g., Binance): record immediately and poll for fill data
	orderRecord := at.createOrderRecord(orderID, symbol, action, positionSide, quantity, price, leverage)
	if err := at.store.Order().CreateOrder(orderRecord); err != nil {
		logger.Infof("  ⚠️ Failed to record order: %v", err)
	} else {
		logger.Infof("  📝 Order recorded: %s [%s] %s", orderID, action, symbol)
	}

	// Wait for order to be filled and get actual fill data
	time.Sleep(500 * time.Millisecond)
	for i := 0; i < 5; i++ {
		status, err := at.trader.GetOrderStatus(symbol, orderID)
		if err == nil {
			statusStr, _ := status["status"].(string)
			if statusStr == "FILLED" {
				// Get actual fill price
				if avgPrice, ok := status["avgPrice"].(float64); ok && avgPrice > 0 {
					actualPrice = avgPrice
				}
				// Get actual executed quantity
				if execQty, ok := status["executedQty"].(float64); ok && execQty > 0 {
					actualQty = execQty
				}
				// Get commission/fee
				if commission, ok := status["commission"].(float64); ok {
					fee = commission
				}
				logger.Infof("  ✅ Order filled: avgPrice=%.6f, qty=%.6f, fee=%.6f", actualPrice, actualQty, fee)

				// Update order status to FILLED
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, "FILLED", actualQty, actualPrice, fee); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}

				// Record fill details
				at.recordOrderFill(orderRecord.ID, orderID, symbol, action, actualPrice, actualQty, fee)
				break
			} else if statusStr == "CANCELED" || statusStr == "EXPIRED" || statusStr == "REJECTED" {
				logger.Infof("  ⚠️ Order %s, skipping position record", statusStr)

				// Update order status
				if err := at.store.Order().UpdateOrderStatus(orderRecord.ID, statusStr, 0, 0, 0); err != nil {
					logger.Infof("  ⚠️ Failed to update order status: %v", err)
				}
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Normalize symbol for position record consistency
	normalizedSymbolForPosition := market.Normalize(symbol)

	logger.Infof("  📝 Recording position (ID: %s, action: %s, price: %.6f, qty: %.6f, fee: %.4f)",
		orderID, action, actualPrice, actualQty, fee)

	// Record position change with actual fill data (use normalized symbol)
	at.recordPositionChange(orderID, normalizedSymbolForPosition, positionSide, action, actualQty, actualPrice, leverage, entryPrice, fee)

	// Send anonymous trade statistics for experience improvement (async, non-blocking)
	// This helps us understand overall product usage across all deployments
	experience.TrackTrade(experience.TradeEvent{
		Exchange:  at.exchange,
		TradeType: action,
		Symbol:    symbol,
		AmountUSD: actualPrice * actualQty,
		Leverage:  leverage,
		UserID:    at.userID,
		TraderID:  at.id,
	})
}

// recordPositionChange records position change (create record on open, update record on close)
func (at *AutoTrader) recordPositionChange(orderID, symbol, side, action string, quantity, price float64, leverage int, entryPrice float64, fee float64) {
	if at.store == nil {
		return
	}

	switch action {
	case "open_long", "open_short":
		// Open position: create new position record
		nowMs := time.Now().UTC().UnixMilli()
		pos := &store.TraderPosition{
			TraderID:     at.id,
			ExchangeID:   at.exchangeID, // Exchange account UUID
			ExchangeType: at.exchange,   // Exchange type: binance/bybit/okx/etc
			Symbol:       symbol,
			Side:         side, // LONG or SHORT
			Quantity:     quantity,
			EntryPrice:   price,
			EntryOrderID: orderID,
			EntryTime:    nowMs,
			Leverage:     leverage,
			Status:       "OPEN",
			CreatedAt:    nowMs,
			UpdatedAt:    nowMs,
		}
		if err := at.store.Position().Create(pos); err != nil {
			logger.Infof("  ⚠️ Failed to record position: %v", err)
		} else {
			logger.Infof("  📊 Position recorded [%s] %s %s @ %.4f", at.id[:8], symbol, side, price)
		}

	case "close_long", "close_short":
		// Close position using PositionBuilder for consistent handling
		// PositionBuilder will handle both cases:
		// 1. If open position exists: close it properly
		// 2. If no open position (e.g., table cleared): create a closed position record
		posBuilder := store.NewPositionBuilder(at.store.Position())
		if err := posBuilder.ProcessTrade(
			at.id, at.exchangeID, at.exchange,
			symbol, side, action,
			quantity, price, fee, 0, // realizedPnL will be calculated
			time.Now().UTC().UnixMilli(), orderID,
		); err != nil {
			logger.Infof("  ⚠️ Failed to process close position: %v", err)
		} else {
			logger.Infof("  ✅ Position closed [%s] %s %s @ %.4f", at.id[:8], symbol, side, price)
		}
	}
}

// createOrderRecord creates an order record struct from order details
func (at *AutoTrader) createOrderRecord(orderID, symbol, action, positionSide string, quantity, price float64, leverage int) *store.TraderOrder {
	// Determine order type (market for auto trader)
	orderType := "MARKET"

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Use action as orderAction directly (keep lowercase format)
	orderAction := action

	// Determine if it's a reduce only order
	reduceOnly := (action == "close_long" || action == "close_short")

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	return &store.TraderOrder{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		ExchangeOrderID: orderID,
		Symbol:          normalizedSymbol,
		Side:            side,
		PositionSide:    positionSide,
		Type:            orderType,
		TimeInForce:     "GTC",
		Quantity:        quantity,
		Price:           price,
		Status:          "NEW",
		FilledQuantity:  0,
		AvgFillPrice:    0,
		Commission:      0,
		CommissionAsset: "USDT",
		Leverage:        leverage,
		ReduceOnly:      reduceOnly,
		ClosePosition:   reduceOnly,
		OrderAction:     orderAction,
		CreatedAt:       time.Now().UTC().UnixMilli(),
		UpdatedAt:       time.Now().UTC().UnixMilli(),
	}
}

// recordOrderFill records order fill/trade details
func (at *AutoTrader) recordOrderFill(orderRecordID int64, exchangeOrderID, symbol, action string, price, quantity, fee float64) {
	if at.store == nil {
		return
	}

	// Determine side (BUY/SELL)
	var side string
	switch action {
	case "open_long", "close_short":
		side = "BUY"
	case "open_short", "close_long":
		side = "SELL"
	}

	// Generate a simple trade ID (exchange doesn't always provide one)
	tradeID := fmt.Sprintf("%s-%d", exchangeOrderID, time.Now().UnixNano())

	// Normalize symbol for consistency
	normalizedSymbol := market.Normalize(symbol)

	fill := &store.TraderFill{
		TraderID:        at.id,
		ExchangeID:      at.exchangeID,
		ExchangeType:    at.exchange,
		OrderID:         orderRecordID,
		ExchangeOrderID: exchangeOrderID,
		ExchangeTradeID: tradeID,
		Symbol:          normalizedSymbol,
		Side:            side,
		Price:           price,
		Quantity:        quantity,
		QuoteQuantity:   price * quantity,
		Commission:      fee,
		CommissionAsset: "USDT",
		RealizedPnL:     0,     // Will be calculated for close orders
		IsMaker:         false, // Market orders are usually taker
		CreatedAt:       time.Now().UTC().UnixMilli(),
	}

	// Calculate realized PnL for close orders
	if action == "close_long" || action == "close_short" {
		// Try to get the entry price from the open position
		var positionSide string
		if action == "close_long" {
			positionSide = "LONG"
		} else {
			positionSide = "SHORT"
		}

		if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, symbol, positionSide); err == nil && openPos != nil {
			if positionSide == "LONG" {
				fill.RealizedPnL = (price - openPos.EntryPrice) * quantity
			} else {
				fill.RealizedPnL = (openPos.EntryPrice - price) * quantity
			}
		}
	}

	if err := at.store.Order().CreateFill(fill); err != nil {
		logger.Infof("  ⚠️ Failed to record fill: %v", err)
	} else {
		logger.Infof("  📋 Fill recorded: %.4f @ %.6f, fee: %.4f", quantity, price, fee)
	}
}

// ============================================================================
// Risk Control Helpers
// ============================================================================

// isBTCETH checks if a symbol is BTC or ETH
func isBTCETH(symbol string) bool {
	symbol = strings.ToUpper(symbol)
	return strings.HasPrefix(symbol, "BTC") || strings.HasPrefix(symbol, "ETH")
}

// enforcePositionValueRatio checks and enforces position value ratio limits (CODE ENFORCED)
// Returns the adjusted position size (capped if necessary) and whether the position was capped
// positionSizeUSD: the original position size in USD
// equity: the account equity
// symbol: the trading symbol
func (at *AutoTrader) enforcePositionValueRatio(positionSizeUSD float64, equity float64, symbol string) (float64, bool) {
	if at.config.StrategyConfig == nil {
		return positionSizeUSD, false
	}

	riskControl := at.config.StrategyConfig.RiskControl

	// Get the appropriate position value ratio limit
	var maxPositionValueRatio float64
	if isBTCETH(symbol) {
		maxPositionValueRatio = riskControl.BTCETHMaxPositionValueRatio
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = 5.0 // Default: 5x for BTC/ETH
		}
	} else {
		maxPositionValueRatio = riskControl.AltcoinMaxPositionValueRatio
		if maxPositionValueRatio <= 0 {
			maxPositionValueRatio = 1.0 // Default: 1x for altcoins
		}
	}

	// Calculate max allowed position value = equity × ratio
	maxPositionValue := equity * maxPositionValueRatio

	// Check if position size exceeds limit
	if positionSizeUSD > maxPositionValue {
		logger.Infof("  ⚠️ [RISK CONTROL] Position %.2f USDT exceeds limit (equity %.2f × %.1fx = %.2f USDT max for %s), capping",
			positionSizeUSD, equity, maxPositionValueRatio, maxPositionValue, symbol)
		return maxPositionValue, true
	}

	return positionSizeUSD, false
}

// enforceMinPositionSize checks minimum position size (CODE ENFORCED)
func (at *AutoTrader) enforceMinPositionSize(positionSizeUSD float64) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	minSize := at.config.StrategyConfig.RiskControl.MinPositionSize
	if minSize <= 0 {
		minSize = 12 // Default: 12 USDT
	}

	if positionSizeUSD < minSize {
		return fmt.Errorf("❌ [RISK CONTROL] Position %.2f USDT below minimum (%.2f USDT)", positionSizeUSD, minSize)
	}
	return nil
}

// enforceMaxPositions checks maximum positions count (CODE ENFORCED)
func (at *AutoTrader) enforceMaxPositions(currentPositionCount int) error {
	if at.config.StrategyConfig == nil {
		return nil
	}

	maxPositions := at.config.StrategyConfig.RiskControl.MaxPositions
	if maxPositions <= 0 {
		maxPositions = 3 // Default: 3 positions
	}

	if currentPositionCount >= maxPositions {
		return fmt.Errorf("❌ [RISK CONTROL] Already at max positions (%d/%d)", currentPositionCount, maxPositions)
	}
	return nil
}

// getSideFromAction converts order action to side (BUY/SELL)
func getSideFromAction(action string) string {
	switch action {
	case "open_long", "close_short":
		return "BUY"
	case "open_short", "close_long":
		return "SELL"
	default:
		return "BUY"
	}
}

// GetOpenOrders returns open orders (pending SL/TP) from exchange
func (at *AutoTrader) GetOpenOrders(symbol string) ([]OpenOrder, error) {
	return at.trader.GetOpenOrders(symbol)
}

// GetTrader 获取底层 Trader 实例
func (at *AutoTrader) GetTrader() Trader {
	return at.trader
}

// sendDecisionNotification 发送决策通知（按用户推送）
func (at *AutoTrader) sendDecisionNotification(decision *kernel.Decision, actionRecord *store.DecisionAction, execErr error) {
	if at.telegramNotifier == nil {
		return
	}

	details := map[string]interface{}{
		"price":             actionRecord.Price,
		"quantity":          actionRecord.Quantity,
		"leverage":          actionRecord.Leverage,
		"position_size_usd": decision.PositionSizeUSD,
		"stop_loss":         actionRecord.StopLoss,
		"take_profit":       actionRecord.TakeProfit,
		"confidence":        actionRecord.Confidence,
	}

	// 对于平仓操作，添加开仓价和盈亏
	if decision.Action == "close_long" || decision.Action == "close_short" {
		if at.store != nil {
			normalizedSymbol := market.Normalize(decision.Symbol)
			side := "LONG"
			if decision.Action == "close_short" {
				side = "SHORT"
			}
			if openPos, err := at.store.Position().GetOpenPositionBySymbol(at.id, normalizedSymbol, side); err == nil && openPos != nil {
				details["entry_price"] = openPos.EntryPrice
				if actionRecord.Price > 0 {
					var pnl float64
					if decision.Action == "close_long" {
						pnl = (actionRecord.Price - openPos.EntryPrice) * actionRecord.Quantity
					} else {
						pnl = (openPos.EntryPrice - actionRecord.Price) * actionRecord.Quantity
					}
					details["pnl"] = pnl
				}
			}
		}
	}

	if execErr != nil {
		details["error"] = execErr.Error()
	}

	msg := notification.FormatDecisionMessage(at.name, decision.Symbol, decision.Action, details)
	
	// 按用户推送：根据交易员的 userID 查找用户的 TelegramChatID
	if at.userID != "" && at.store != nil {
		user, err := at.store.User().GetByID(at.userID)
		if err == nil && user != nil && user.TelegramChatID != 0 {
			// 用户配置了 Telegram Chat ID，只向该用户推送
			if err := at.telegramNotifier.SendMessageToUser(user.TelegramChatID, msg); err != nil {
				logger.Warnf("Failed to send telegram notification to user %s (ChatID: %d): %v", at.userID, user.TelegramChatID, err)
			}
			return
		}
	}
	
	// 回退：用户未配置 Telegram Chat ID，使用广播模式（向后兼容）
	if err := at.telegramNotifier.SendMessage(msg); err != nil {
		logger.Warnf("Failed to send telegram notification: %v", err)
	}
}

// sendRiskControlCloseNotification 发送风控系统平仓通知（按用户推送）
func (at *AutoTrader) sendRiskControlCloseNotification(symbol, side, strategy string, details map[string]interface{}) {
	if at.telegramNotifier == nil {
		return
	}

	msg := notification.FormatRiskControlCloseMessage(at.name, symbol, side, strategy, details)
	
	// 按用户推送：根据交易员的 userID 查找用户的 TelegramChatID
	if at.userID != "" && at.store != nil {
		user, err := at.store.User().GetByID(at.userID)
		if err == nil && user != nil && user.TelegramChatID != 0 {
			// 用户配置了 Telegram Chat ID，只向该用户推送
			if err := at.telegramNotifier.SendMessageToUser(user.TelegramChatID, msg); err != nil {
				logger.Warnf("Failed to send risk control close notification to user %s (ChatID: %d): %v", at.userID, user.TelegramChatID, err)
			}
			return
		}
	}
	
	// 回退：用户未配置 Telegram Chat ID，使用广播模式（向后兼容）
	if err := at.telegramNotifier.SendMessage(msg); err != nil {
		logger.Warnf("Failed to send risk control close notification: %v", err)
	}
}

// sendDrawdownWarningNotification 发送回撤监控警告通知（按用户推送）
func (at *AutoTrader) sendDrawdownWarningNotification(symbol, side string, details map[string]interface{}) {
	if at.telegramNotifier == nil {
		return
	}

	msg := notification.FormatDrawdownWarningMessage(at.name, symbol, side, details)
	
	// 按用户推送：根据交易员的 userID 查找用户的 TelegramChatID
	if at.userID != "" && at.store != nil {
		user, err := at.store.User().GetByID(at.userID)
		if err == nil && user != nil && user.TelegramChatID != 0 {
			// 用户配置了 Telegram Chat ID，只向该用户推送
			if err := at.telegramNotifier.SendMessageToUser(user.TelegramChatID, msg); err != nil {
				logger.Warnf("Failed to send drawdown warning notification to user %s (ChatID: %d): %v", at.userID, user.TelegramChatID, err)
			}
			return
		}
	}
	
	// 回退：用户未配置 Telegram Chat ID，使用广播模式（向后兼容）
	if err := at.telegramNotifier.SendMessage(msg); err != nil {
		logger.Warnf("Failed to send drawdown warning notification: %v", err)
	}
}

// sendAccountSummary 发送账户和持仓摘要（按用户推送）
func (at *AutoTrader) sendAccountSummary() {
	if at.telegramNotifier == nil {
		return
	}

	// 清除缓存，确保获取最新数据
	// 交易执行后，本地缓存可能还是旧数据
	at.trader.InvalidateCache()

	// 等待交易所端数据同步（交易所内部处理可能有延迟）
	time.Sleep(1 * time.Second)

	// 获取用户的 Telegram Chat ID（用于按用户推送）
	var userChatID int64
	if at.userID != "" && at.store != nil {
		user, err := at.store.User().GetByID(at.userID)
		if err == nil && user != nil {
			userChatID = user.TelegramChatID
		}
	}

	// 获取账户信息（现在会从API获取最新数据）
	accountInfo, err := at.GetAccountInfo()
	if err != nil {
		logger.Warnf("Failed to get account info for telegram: %v", err)
		return
	}

	// 获取持仓信息
	positions, err := at.GetPositions()
	if err != nil {
		logger.Warnf("Failed to get positions for telegram: %v", err)
		return
	}

	// 计算持仓数量
	positionCount := len(positions)
	accountInfo["position_count"] = positionCount

	// 发送账户信息
	accountMsg := notification.FormatAccountInfoMessage(at.name, accountInfo)
	if userChatID != 0 {
		// 按用户推送账户信息
		if err := at.telegramNotifier.SendMessageToUser(userChatID, accountMsg); err != nil {
			logger.Warnf("Failed to send account info to user %s (ChatID: %d): %v", at.userID, userChatID, err)
		}
	} else {
		// 回退：用户未配置 Telegram Chat ID，使用广播模式（向后兼容）
		if err := at.telegramNotifier.SendMessage(accountMsg); err != nil {
			logger.Warnf("Failed to send account info: %v", err)
		}
	}

	// 发送持仓信息
	if positionCount > 0 {
		positionsMsg := notification.FormatPositionsMessage(at.name, positions)
		if userChatID != 0 {
			// 按用户推送持仓信息
			if err := at.telegramNotifier.SendMessageToUser(userChatID, positionsMsg); err != nil {
				logger.Warnf("Failed to send positions info to user %s (ChatID: %d): %v", at.userID, userChatID, err)
			}
		} else {
			// 回退：用户未配置 Telegram Chat ID，使用广播模式（向后兼容）
			if err := at.telegramNotifier.SendMessage(positionsMsg); err != nil {
				logger.Warnf("Failed to send positions info: %v", err)
			}
		}
	}
}

// startStopLossMonitor starts automatic stop-loss monitoring with high-frequency polling (5-10 seconds)
// 自动止损监控：高频轮询（5-10秒），检查固定止损（-5%）和回撤止损
func (at *AutoTrader) startStopLossMonitor() {
	at.monitorWg.Add(1)
	go func() {
		defer at.monitorWg.Done()

		// High-frequency check interval (5-10 seconds for timely stop-loss)
		checkInterval := 8 * time.Second
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		logger.Info("🛡️ Started automatic stop-loss monitoring (high-frequency: 8s interval)")

		for {
			select {
			case <-ticker.C:
				at.checkStopLoss()
			case <-at.stopMonitorCh:
				logger.Info("🛡️ Stop-loss monitoring stopped")
				return
			}
		}
	}()
}

// checkStopLoss checks all positions for stop-loss conditions (multi-strategy coordination)
// 多策略协同止损：固定止损、回撤止损、移动止损、时间止损、波动止损
// 任一策略触发即止损，提供全方位保护
func (at *AutoTrader) checkStopLoss() {
	// Get current positions
	positions, err := at.trader.GetPositions()
	if err != nil {
		logger.Infof("❌ Stop-loss monitoring: failed to get positions: %v", err)
		return
	}

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}

		// Get leverage
		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		// Calculate current P&L percentage using price change (real-time)
		var currentPnLPct float64
		if side == "long" {
			currentPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			currentPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// Prepare common data for all strategies
		posKey := symbol + "_" + side
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		marginUsed := (quantity * markPrice) / float64(leverage)
		realPnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)
		
		// Track which strategy triggered (for logging)
		triggeredStrategy := ""
		shouldClose := false

		// 1. Check fixed stop-loss (-5%)
		// 固定止损：亏损达到-5%时立即止损
		if currentPnLPct <= -5.0 && realPnlPct <= -5.0 {
			triggeredStrategy = "Fixed Stop-Loss"
			shouldClose = true
			logger.Infof("🚨 [%s] Fixed stop-loss triggered: %s %s | Loss: %.2f%% (threshold: -5.00%%)",
				triggeredStrategy, symbol, side, realPnlPct)
		}

		// 2. Check trailing stop-loss (移动止损)
		// 移动止损：价格上升时止损价跟随上升，让利润奔跑
		if !shouldClose && currentPnLPct > 3.0 { // Only activate when profit > 3%
			at.trailingStopPriceMutex.Lock()
			trailingStop, exists := at.trailingStopPrice[posKey]
			
			// Calculate trailing stop distance (3% of current price for long, adjust for short)
			trailingDistance := markPrice * 0.03 // 3% trailing distance
			
			if side == "long" {
				// For long: trailing stop follows price upward
				newTrailingStop := markPrice - trailingDistance
				if !exists || newTrailingStop > trailingStop {
					// Update trailing stop only upward (never lower)
					at.trailingStopPrice[posKey] = newTrailingStop
					trailingStop = newTrailingStop
				}
				// Check if price dropped below trailing stop
				if markPrice <= trailingStop {
					triggeredStrategy = "Trailing Stop-Loss"
					shouldClose = true
					logger.Infof("🚨 [%s] Trailing stop-loss triggered: %s %s | Price: %.6f | Trailing Stop: %.6f | Profit: %.2f%%",
						triggeredStrategy, symbol, side, markPrice, trailingStop, currentPnLPct)
				}
			} else {
				// For short: trailing stop follows price downward
				newTrailingStop := markPrice + trailingDistance
				if !exists || newTrailingStop < trailingStop || trailingStop == 0 {
					// Update trailing stop only downward (never higher)
					at.trailingStopPrice[posKey] = newTrailingStop
					trailingStop = newTrailingStop
				}
				// Check if price rose above trailing stop
				if markPrice >= trailingStop {
					triggeredStrategy = "Trailing Stop-Loss"
					shouldClose = true
					logger.Infof("🚨 [%s] Trailing stop-loss triggered: %s %s | Price: %.6f | Trailing Stop: %.6f | Profit: %.2f%%",
						triggeredStrategy, symbol, side, markPrice, trailingStop, currentPnLPct)
				}
			}
			at.trailingStopPriceMutex.Unlock()
		}

		// 3. Check time-based stop-loss (时间止损)
		// 时间止损：持仓时间过长自动止损，避免长期持仓
		if !shouldClose {
			positionStartTime, exists := at.positionFirstSeenTime[posKey]
			if exists {
				positionDuration := time.Since(time.UnixMilli(positionStartTime))
				maxHoldTime := 24 * time.Hour // Maximum hold time: 24 hours
				
				if positionDuration >= maxHoldTime {
					triggeredStrategy = "Time-based Stop-Loss"
					shouldClose = true
					logger.Infof("🚨 [%s] Time-based stop-loss triggered: %s %s | Hold time: %.1fh (max: %.1fh) | Profit: %.2f%%",
						triggeredStrategy, symbol, side, positionDuration.Hours(), maxHoldTime.Hours(), currentPnLPct)
				}
			}
		}

		// 4. Check volatility stop-loss (波动止损)
		// 波动止损：基于ATR的动态止损，适应市场波动
		if !shouldClose && currentPnLPct > 0 {
			// Get market data for ATR calculation
			marketData, err := market.Get(symbol)
			if err == nil && marketData != nil {
				// Try to get ATR14 from different possible locations
				var atr14 float64
				if marketData.IntradaySeries != nil && marketData.IntradaySeries.ATR14 > 0 {
					atr14 = marketData.IntradaySeries.ATR14
				} else if marketData.LongerTermContext != nil && marketData.LongerTermContext.ATR14 > 0 {
					atr14 = marketData.LongerTermContext.ATR14
				}
				
				if atr14 > 0 {
					// Calculate ATR-based stop distance (2x ATR for volatility stop)
					atrStopDistance := atr14 * 2.0
					
					if side == "long" {
						// For long: stop if price drops below entry - 2x ATR
						volatilityStopPrice := entryPrice - atrStopDistance
						if markPrice <= volatilityStopPrice {
							triggeredStrategy = "Volatility Stop-Loss"
							shouldClose = true
							logger.Infof("🚨 [%s] Volatility stop-loss triggered: %s %s | Price: %.6f | ATR Stop: %.6f (ATR14: %.6f) | Profit: %.2f%%",
								triggeredStrategy, symbol, side, markPrice, volatilityStopPrice, atr14, currentPnLPct)
						}
					} else {
						// For short: stop if price rises above entry + 2x ATR
						volatilityStopPrice := entryPrice + atrStopDistance
						if markPrice >= volatilityStopPrice {
							triggeredStrategy = "Volatility Stop-Loss"
							shouldClose = true
							logger.Infof("🚨 [%s] Volatility stop-loss triggered: %s %s | Price: %.6f | ATR Stop: %.6f (ATR14: %.6f) | Profit: %.2f%%",
								triggeredStrategy, symbol, side, markPrice, volatilityStopPrice, atr14, currentPnLPct)
						}
					}
				}
			}
		}

		// 5. Check drawdown stop-loss (回撤止损)
		// 回撤止损：使用现有的分级回撤策略，但使用高频检查
		var refreshPeakPnlPct, realDrawdownPct float64
		if !shouldClose {
			// Get peak profit
			at.peakPnLCacheMutex.RLock()
			peakPnLPct, exists := at.peakPnLCache[posKey]
			at.peakPnLCacheMutex.RUnlock()

			if !exists {
				// Initialize peak cache
				peakPnLPct = currentPnLPct
				at.UpdatePeakPnL(symbol, side, currentPnLPct)
			} else {
				// Update peak cache
				at.UpdatePeakPnL(symbol, side, currentPnLPct)
			}

			// Calculate drawdown
			var drawdownPct float64
			if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
				drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
			}

			// Get tiered drawdown threshold
			drawdownThreshold := getTieredDrawdownThreshold(peakPnLPct)

			// Check if drawdown threshold is triggered (using price-based for quick check)
			// 使用价格变化进行快速检查，如果接近阈值再用实际盈亏验证
			if currentPnLPct >= 5.5 && drawdownPct >= (drawdownThreshold-2.0) && drawdownThreshold > 0 {
				// Recalculate drawdown with actual P&L
				at.peakPnLCacheMutex.RLock()
				refreshPeakPnlPct = at.peakPnLCache[posKey]
				at.peakPnLCacheMutex.RUnlock()

				if refreshPeakPnlPct > 0 && realPnlPct < refreshPeakPnlPct {
					realDrawdownPct = ((refreshPeakPnlPct - realPnlPct) / refreshPeakPnlPct) * 100
				}

				realDrawdownThreshold := getTieredDrawdownThreshold(refreshPeakPnlPct)
				if realPnlPct >= 6.0 && realDrawdownPct >= realDrawdownThreshold && realDrawdownThreshold > 0 {
					triggeredStrategy = "Drawdown Stop-Loss"
					shouldClose = true
					logger.Infof("🚨 [%s] Drawdown stop-loss triggered: %s %s | Real profit: %.2f%% | Peak profit: %.2f%% | Drawdown: %.2f%% (threshold: %.2f%%)",
						triggeredStrategy, symbol, side, realPnlPct, refreshPeakPnlPct, realDrawdownPct, realDrawdownThreshold)
				}
			}
		}

		// Execute stop-loss if any strategy triggered (multi-strategy coordination)
		// 多策略协同：任一策略触发即止损（风控系统绝对优先）
		if shouldClose {
			if err := at.emergencyClosePosition(symbol, side, true); err != nil {
				logger.Infof("❌ [%s] Stop-loss close position failed (%s %s): %v", triggeredStrategy, symbol, side, err)
			} else {
				logger.Infof("✅ [%s] Stop-loss close position succeeded: %s %s | Realized PnL: %.2f USDT (%.2f%%) | Entry: %.6f | Exit: %.6f | Qty: %.6f | Margin: %.2f USDT",
					triggeredStrategy, symbol, side, unrealizedPnl, realPnlPct, entryPrice, markPrice, quantity, marginUsed)
				
				// Send Telegram notification
				notificationDetails := map[string]interface{}{
					"entry_price": entryPrice,
					"exit_price":  markPrice,
					"quantity":    quantity,
					"margin":      marginUsed,
					"pnl":         unrealizedPnl,
					"pnl_pct":     realPnlPct,
				}
				// Add strategy-specific details
				if triggeredStrategy == "Drawdown Stop-Loss" {
					if refreshPeakPnlPct > 0 {
						notificationDetails["peak_profit"] = refreshPeakPnlPct
					}
					if realDrawdownPct > 0 {
						notificationDetails["drawdown"] = realDrawdownPct
					}
				}
				at.sendRiskControlCloseNotification(symbol, side, triggeredStrategy, notificationDetails)
				
				// Clear all caches after closing
				at.ClearPeakPnLCache(symbol, side)
				at.trailingStopPriceMutex.Lock()
				delete(at.trailingStopPrice, posKey)
				at.trailingStopPriceMutex.Unlock()
				delete(at.positionFirstSeenTime, posKey)
			}
		}
	}
}
