package notification

import (
	"fmt"
	"nofx/auth"
	"nofx/kernel"
	"nofx/logger"
	"nofx/market"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// UserInterface 用户接口（避免循环导入）
type UserInterface interface {
	GetID() string
	GetOTPSecret() string
	IsOTPVerified() bool
}

// UserStoreInterface 用户存储接口（避免循环导入）
type UserStoreInterface interface {
	GetByID(userID string) (UserInterface, error)
	GetByEmail(email string) (UserInterface, error)
	UpdateTelegramChatID(userID string, chatID int64) error
}

// TraderInterface 交易员接口（避免循环导入）
type TraderInterface interface {
	GetName() string
	GetID() string
	GetAccountInfo() (map[string]interface{}, error)
	GetPositions() ([]map[string]interface{}, error)
	GetStatus() map[string]interface{}
	ExecuteDecision(*kernel.Decision) error
	GetTrader() interface {
		SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error
		SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error
	}
	GetStore() interface {
		Position() PositionStoreInterface
	}
	Run() error
	Stop()
}

// PositionStoreInterface 持仓存储接口（避免循环导入）
type PositionStoreInterface interface {
	GetRecentTrades(traderID string, limit int) ([]map[string]interface{}, error)
}

// TraderManagerInterface 交易员管理器接口（避免循环导入）
type TraderManagerInterface interface {
	GetAllTraders() map[string]TraderInterface
	GetTrader(traderID string) (TraderInterface, error)
	LoadUserTradersFromStore(store interface{}, userID string) error
}

// StoreInterface 存储接口（避免循环导入）
type StoreInterface interface {
	Trader() TraderStoreInterface
}

// TraderStoreInterface 交易员存储接口（避免循环导入）
type TraderStoreInterface interface {
	List(userID string) ([]TraderInfo, error)
	UpdateStatus(userID, traderID string, isRunning bool) error
}

// TraderInfo 交易员信息（避免循环导入）
type TraderInfo interface {
	GetID() string
	GetName() string
	IsRunning() bool
}

// TelegramSession 会话信息（用于免验证）
type TelegramSession struct {
	UserID    string
	Email     string
	ExpiresAt time.Time
}

// CommandContext 指令处理上下文
type CommandContext struct {
	TraderManager TraderManagerInterface
	UserStore     UserStoreInterface
	Store         StoreInterface
	// 会话管理：key = chatID，value = 会话信息
	sessions      map[int64]*TelegramSession
	sessionsMutex sync.RWMutex
}

// initSessions 初始化会话管理（如果未初始化）
func (ctx *CommandContext) initSessions() {
	if ctx.sessions == nil {
		ctx.sessions = make(map[int64]*TelegramSession)
	}
}

// getSession 获取会话（如果有效）
func (ctx *CommandContext) getSession(chatID int64) (*TelegramSession, bool) {
	ctx.initSessions()
	ctx.sessionsMutex.RLock()
	defer ctx.sessionsMutex.RUnlock()
	
	session, exists := ctx.sessions[chatID]
	if !exists {
		return nil, false
	}
	
	// 检查会话是否过期
	if time.Now().After(session.ExpiresAt) {
		return nil, false
	}
	
	return session, true
}

// setSession 设置会话（有效期30秒，与OTP有效期一致）
func (ctx *CommandContext) setSession(chatID int64, userID, email string) {
	ctx.initSessions()
	ctx.sessionsMutex.Lock()
	defer ctx.sessionsMutex.Unlock()
	
	ctx.sessions[chatID] = &TelegramSession{
		UserID:    userID,
		Email:     email,
		ExpiresAt: time.Now().Add(30 * time.Second), // OTP有效期30秒
	}
}

// clearSession 清除会话
func (ctx *CommandContext) clearSession(chatID int64) {
	ctx.initSessions()
	ctx.sessionsMutex.Lock()
	defer ctx.sessionsMutex.Unlock()
	
	delete(ctx.sessions, chatID)
}

// CreateCommandHandlers 创建指令处理器
func CreateCommandHandlers(ctx *CommandContext) map[string]CommandHandler {
	handlers := make(map[string]CommandHandler)

	// /start - 发送欢迎消息（用户首次打开对话时）
	handlers["/start"] = func(update *tgbotapi.Update) string {
		return `🤖 <b>欢迎使用 NOFX 交易机器人</b>

📋 <b>快速开始：</b>

1️⃣ <b>登录</b>（首次使用需要）：
   /login [邮箱] [OTP码]
   示例: /login user@example.com 123456

2️⃣ <b>查看账户</b>：
   /account [邮箱] [OTP码]
   或登录后直接: /account

3️⃣ <b>查看价格</b>（无需登录）：
   /price BTCUSDT

📢 <b>自动推送功能：</b>
登录成功后，系统会自动推送以下信息：
- 📈 交易决策通知（开仓/平仓）
- 📊 账户摘要和持仓详情
- 🛡️ 风控系统通知（止损/回撤）

💡 <b>提示：</b>
- 首次使用请先执行 /login 命令进行登录
- 登录成功后，您的 Telegram ChatID 会自动配置
- 之后您将自动接收交易通知，无需手动查询
- 发送 /help 查看完整帮助信息`
	}

	// /login - 登录并保存session（邮箱和OTP）
	handlers["/login"] = func(update *tgbotapi.Update) string {
		return handleLoginCommand(ctx, update)
	}

	// /account - 查看账户及持仓（支持session或邮箱+OTP）
	handlers["/account"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleAccountCommandWithOTP)
	}

	// /price - 查看币种价格（无需 OTP）
	handlers["/price"] = func(update *tgbotapi.Update) string {
		args := strings.Fields(update.Message.Text)
		if len(args) < 1 {
			return "❌ 请指定币种\n示例: /price BTCUSDT"
		}
		return handlePriceCommand(ctx, args[0])
	}

	// /sl - 设置止损（支持session或邮箱+OTP）
	handlers["/sl"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleStopLossCommandWithOTP)
	}

	// /tp - 设置止盈（支持session或邮箱+OTP）
	handlers["/tp"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleTakeProfitCommandWithOTP)
	}

	// /close - 平仓（支持session或邮箱+OTP）
	handlers["/close"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleCloseCommandWithOTP)
	}

	// /trades - 查看最近交易（支持session或邮箱+OTP）
	handlers["/trades"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleTradesCommandWithOTP)
	}

	// /trader - 查看交易员状态（支持session或邮箱+OTP）
	handlers["/trader"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleTraderStatusCommandWithOTP)
	}

	// /start-trader - 启用交易员（支持session或邮箱+OTP）
	handlers["/start-trader"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleStartTraderCommandWithOTP)
	}

	// /stop-trader - 停用交易员（支持session或邮箱+OTP）
	handlers["/stop-trader"] = func(update *tgbotapi.Update) string {
		return handleCommandWithSessionOrOTP(ctx, update, handleStopTraderCommandWithOTP)
	}

	// /help - 帮助
	handlers["/help"] = func(update *tgbotapi.Update) string {
		return `📋 <b>可用指令：</b>

/price [币种] - 查看币种当前价格（无需验证）
  示例: /price BTCUSDT

/login [邮箱] [OTP码] - 登录并保存session（30秒免验证）
  示例: /login user@example.com 123456

<b>需要认证的操作（支持session或邮箱+OTP）：</b>
/account - 查看账户及持仓信息
  有session: /account
  无session: /account user@example.com 123456
  覆盖session: /account user@example.com 123456

/trades [币种] [数量] - 查看最近交易记录
  有session: /trades BTCUSDT 10
  无session: /trades BTCUSDT 10 user@example.com 123456

/trader [交易员ID] - 查看交易员状态
  有session: /trader trader_id_123
  无session: /trader trader_id_123 user@example.com 123456

/start-trader [交易员ID] - 启用交易员
  有session: /start-trader trader_id_123
  无session: /start-trader trader_id_123 user@example.com 123456

/stop-trader [交易员ID] - 停用交易员
  有session: /stop-trader trader_id_123
  无session: /stop-trader trader_id_123 user@example.com 123456

/sl [币种] [止损价] - 设置止损
  有session: /sl BTCUSDT 42000
  无session: /sl BTCUSDT 42000 user@example.com 123456

/tp [币种] [止盈价] - 设置止盈
  有session: /tp BTCUSDT 45000
  无session: /tp BTCUSDT 45000 user@example.com 123456

/close [币种] [方向] - 平仓
  有session: /close BTCUSDT long
  无session: /close BTCUSDT long user@example.com 123456

/help - 显示帮助信息（无需验证）

📢 <b>自动推送功能：</b>
系统会自动推送以下交易信息到 Telegram：

<b>交易决策通知：</b>
📈 开仓通知 - 包含价格、数量、杠杆、仓位大小、止损、止盈、信心度
📉 平仓通知 - 包含价格、数量、开仓价、盈亏
🔄 持仓调整 - 包含调整详情

<b>账户摘要：</b>
📊 总权益、可用余额、已用保证金
📈 总盈亏（含百分比）
📋 持仓数量

<b>持仓详情：</b>
📋 每个持仓的符号、方向、数量、杠杆
💰 开仓价、标记价、未实现盈亏

💡 <b>提示：</b>
- 只有 /price 和 /help 指令无需验证
- 使用 /login email OTP 登录后，30秒内其他指令无需输入邮箱和OTP
- 也可以在指令末尾提供邮箱和OTP来操作（覆盖当前session）
- 免验证时长与OTP有效期一致（30秒）
- 邮箱应该是注册时使用的邮箱地址
- OTP 码来自你的 Google Authenticator 等 2FA 应用
- 交易通知会在 AI 交易员执行交易时自动推送，无需手动查询`
	}

	return handlers
}

// getFirstTrader 获取第一个运行中的交易员
func getFirstTrader(ctx *CommandContext) (TraderInterface, error) {
	traders := ctx.TraderManager.GetAllTraders()
	if len(traders) == 0 {
		return nil, fmt.Errorf("没有找到运行中的交易员")
	}

	// 使用第一个交易员
	for _, t := range traders {
		return t, nil
	}

	return nil, fmt.Errorf("没有找到运行中的交易员")
}

// handleLoginCommand 处理登录指令，创建session
func handleLoginCommand(ctx *CommandContext, update *tgbotapi.Update) string {
	args := strings.Fields(update.Message.Text)
	chatID := update.Message.Chat.ID

	// 注意：telegram_webhook.go 在调用处理器前已经移除了命令部分，只保留了参数
	// 所以 args 只包含邮箱和 OTP，不包含 /login 命令
	if len(args) < 2 {
		return "❌ 参数不足。登录需要邮箱和 Google Authenticator 验证码。\n\n📋 <b>使用方法：</b>\n/login [邮箱] [OTP码]\n\n💡 <b>示例：</b>\n/login user@example.com 123456\n\n📝 <b>说明：</b>\n- 邮箱：注册时使用的邮箱地址\n- OTP码：Google Authenticator 等 2FA 应用中的6位验证码"
	}

	// 第一个参数是邮箱，第二个参数是 OTP
	email := args[0]
	otpCode := args[1]

	// 验证邮箱格式（简单检查）
	if !strings.Contains(email, "@") {
		return "❌ 邮箱格式不正确。\n\n请提供有效的邮箱地址，例如：user@example.com"
	}

	// 验证 OTP 格式（应该是6位数字）
	if len(otpCode) != 6 {
		return fmt.Sprintf("❌ OTP 验证码格式不正确。\n\nOTP 应该是6位数字，您提供的是 %d 位。\n请使用 Google Authenticator 应用中的当前验证码。", len(otpCode))
	}
	if _, err := strconv.Atoi(otpCode); err != nil {
		return "❌ OTP 验证码必须是数字。\n\n请使用 Google Authenticator 应用中的6位数字验证码。"
	}

	// 通过邮箱获取用户
	user, err := ctx.UserStore.GetByEmail(email)
	if err != nil {
		return fmt.Sprintf("❌ 用户不存在: %s\n\n请确认邮箱地址是否正确。邮箱应该是注册时使用的邮箱地址。", email)
	}

	// 检查用户是否已启用 OTP
	if !user.IsOTPVerified() {
		return "❌ 该账户尚未完成 2FA 设置。请先在 Web 界面完成 2FA 配置。"
	}

	// 验证 OTP
	if !auth.VerifyOTP(user.GetOTPSecret(), otpCode) {
		return "❌ OTP 验证码错误。请使用 Google Authenticator 应用中的当前验证码。"
	}

	// OTP验证成功，自动更新用户的 Telegram ChatID
	userID := user.GetID()
	if err := ctx.UserStore.UpdateTelegramChatID(userID, chatID); err != nil {
		logger.Warnf("Failed to update user Telegram ChatID: %v", err)
		// 即使更新失败，也继续创建会话，不阻止登录
	} else {
		logger.Infof("✓ Updated Telegram ChatID for user %s (email: %s) to %d", userID, email, chatID)
	}

	// 创建会话（有效期30秒，与OTP有效期一致）
	ctx.setSession(chatID, userID, email)

	return fmt.Sprintf("✅ 登录成功！\n\n账户: %s\nTelegram ChatID: %d\n免验证时长: 30秒\n\n💡 30秒内使用其他指令无需输入邮箱和OTP。\n💡 您的 Telegram ChatID 已自动配置，后续将只向您推送通知。", email, chatID)
}

// handleCommandWithSessionOrOTP 处理需要认证的指令（支持session或邮箱+OTP）
// 如果session有效，直接使用；如果提供了邮箱和OTP（在参数最后），则使用提供的账户
func handleCommandWithSessionOrOTP(ctx *CommandContext, update *tgbotapi.Update, handler func(*CommandContext, *tgbotapi.Update, UserInterface) string) string {
	chatID := update.Message.Chat.ID
	args := strings.Fields(update.Message.Text)

	var user UserInterface
	var err error
	var useProvidedAccount bool

	// 检查参数最后是否有邮箱和OTP（邮箱包含@，OTP是6位数字）
	// 格式: /account email OTP
	// 格式: /sl symbol price email OTP
	// 格式: /close symbol side email OTP
	if len(args) >= 2 {
		lastArg := args[len(args)-1]
		secondLastArg := args[len(args)-2]

		// 检查最后一个参数是否是OTP（6位数字）
		if len(lastArg) == 6 {
			if _, parseErr := strconv.Atoi(lastArg); parseErr == nil {
				// 检查倒数第二个参数是否是邮箱（包含@）
				if strings.Contains(secondLastArg, "@") {
					// 提供了邮箱和OTP，使用提供的账户
					email := secondLastArg
					otpCode := lastArg

					user, err = ctx.UserStore.GetByEmail(email)
					if err == nil {
						// 检查用户是否已启用 OTP
						if !user.IsOTPVerified() {
							return "❌ 该账户尚未完成 2FA 设置。请先在 Web 界面完成 2FA 配置。"
						}

						// 验证 OTP
						if !auth.VerifyOTP(user.GetOTPSecret(), otpCode) {
							return "❌ OTP 验证码错误。请使用 Google Authenticator 应用中的当前验证码。"
						}

						// OTP验证成功，更新会话
						ctx.setSession(chatID, user.GetID(), email)
						useProvidedAccount = true

						// 移除邮箱和OTP参数
						args = args[:len(args)-2]
					}
				}
			}
		}
	}

	// 如果没有提供邮箱和OTP，尝试使用session
	if !useProvidedAccount {
		if session, valid := ctx.getSession(chatID); valid {
			// 会话有效，使用session中的用户
			user, err = ctx.UserStore.GetByID(session.UserID)
			if err != nil {
				// 用户不存在，清除会话
				ctx.clearSession(chatID)
			}
		}
	}

	// 如果既没有提供账户，也没有有效session，返回错误
	if user == nil {
		return "❌ 需要登录或提供账户信息。\n\n方式1: 先使用 /login email OTP 登录\n方式2: 在指令末尾提供邮箱和OTP\n示例: /account user@example.com 123456\n示例: /sl BTCUSDT 42000 user@example.com 123456"
	}

	// 注意：telegram_webhook.go 在调用处理器前已经移除了命令部分，只保留了参数
	// 如果使用了提供的账户，邮箱和OTP已经在前面移除了
	// 所以这里直接使用所有剩余的参数（不需要再移除第一个参数）
	if len(args) > 0 {
		update.Message.Text = strings.Join(args, " ")
	} else {
		update.Message.Text = ""
	}

	return handler(ctx, update, user)
}

// handleAccountCommandWithOTP 处理账户查询指令（带用户 OTP 验证）
func handleAccountCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	return handleAccountCommand(ctx)
}

// handleAccountCommand 处理账户查询指令（内部函数）
func handleAccountCommand(ctx *CommandContext) string {
	firstTrader, err := getFirstTrader(ctx)
	if err != nil {
		return fmt.Sprintf("❌ %s", err.Error())
	}

	// 获取账户信息
	accountInfo, err := firstTrader.GetAccountInfo()
	if err != nil {
		return fmt.Sprintf("❌ 获取账户信息失败: %v", err)
	}

	// 获取持仓信息
	positions, err := firstTrader.GetPositions()
	if err != nil {
		return fmt.Sprintf("❌ 获取持仓信息失败: %v", err)
	}

	// 格式化消息
	msg := FormatAccountInfoMessage(firstTrader.GetName(), accountInfo)
	if len(positions) > 0 {
		msg += "\n\n" + FormatPositionsMessage(firstTrader.GetName(), positions)
	}

	return msg
}

// handlePriceCommand 处理价格查询指令
func handlePriceCommand(ctx *CommandContext, symbol string) string {
	symbol = market.Normalize(symbol)

	// 获取市场价格
	marketData, err := market.Get(symbol)
	if err != nil {
		return fmt.Sprintf("❌ 获取 %s 价格失败: %v", symbol, err)
	}

	msg := fmt.Sprintf("💰 <b>%s 当前价格</b>\n\n", symbol)
	// 使用动态精度格式化价格（支持低价meme币到高价BTC）
	msg += fmt.Sprintf("📊 价格: $%s\n", market.FormatPriceWithDynamicPrecision(marketData.CurrentPrice))

	// 计算24h价格变化（使用4h数据作为近似）
	if marketData.PriceChange4h != 0 {
		msg += fmt.Sprintf("📈 4h 涨跌: %.2f%%\n", marketData.PriceChange4h)
	}
	if marketData.PriceChange1h != 0 {
		msg += fmt.Sprintf("📈 1h 涨跌: %.2f%%\n", marketData.PriceChange1h)
	}

	// 从K线数据获取最高最低价
	if marketData.TimeframeData != nil {
		if tfData, ok := marketData.TimeframeData["5m"]; ok && len(tfData.Klines) > 0 {
			klines := tfData.Klines
			high := klines[0].High
			low := klines[0].Low
			for _, k := range klines {
				if k.High > high {
					high = k.High
				}
				if k.Low < low {
					low = k.Low
				}
			}
			// 使用动态精度格式化高低价
			msg += fmt.Sprintf("📊 区间最高: $%s\n", market.FormatPriceWithDynamicPrecision(high))
			msg += fmt.Sprintf("📊 区间最低: $%s", market.FormatPriceWithDynamicPrecision(low))
		}
	}

	return msg
}

// handleStopLossCommandWithOTP 处理止损指令（带用户 OTP 验证）
func handleStopLossCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	if len(args) < 2 {
		return "❌ 请指定币种和止损价\n示例: /sl user_abc123 BTCUSDT 42000 123456"
	}

	price, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return fmt.Sprintf("❌ 无效的价格: %s", args[1])
	}

	return handleStopLossCommand(ctx, args[0], price)
}

// handleStopLossCommand 处理止损设置指令（内部函数）
func handleStopLossCommand(ctx *CommandContext, symbol string, stopPrice float64) string {
	symbol = market.Normalize(symbol)

	firstTrader, err := getFirstTrader(ctx)
	if err != nil {
		return fmt.Sprintf("❌ %s", err.Error())
	}

	// 获取持仓信息，找到该币种的持仓
	positions, err := firstTrader.GetPositions()
	if err != nil {
		return fmt.Sprintf("❌ 获取持仓信息失败: %v", err)
	}

	var targetPosition map[string]interface{}
	for _, pos := range positions {
		if pos["symbol"] == symbol {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Sprintf("❌ 未找到 %s 的持仓", symbol)
	}

	// 获取持仓方向和数量
	side, _ := targetPosition["side"].(string)
	quantity, _ := targetPosition["positionAmt"].(float64)
	if quantity < 0 {
		quantity = -quantity
	}

	positionSide := "LONG"
	if side == "short" {
		positionSide = "SHORT"
	}

	// 设置止损
	traderInstance := firstTrader.GetTrader()
	if err := traderInstance.SetStopLoss(symbol, positionSide, quantity, stopPrice); err != nil {
		return fmt.Sprintf("❌ 设置止损失败: %v", err)
	}

	return fmt.Sprintf("✅ 已设置 %s %s 止损: $%.2f", symbol, side, stopPrice)
}

// handleTakeProfitCommandWithOTP 处理止盈指令（带用户 OTP 验证）
func handleTakeProfitCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	if len(args) < 2 {
		return "❌ 请指定币种和止盈价\n示例: /tp user@example.com BTCUSDT 45000 123456"
	}

	price, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return fmt.Sprintf("❌ 无效的价格: %s", args[1])
	}

	return handleTakeProfitCommand(ctx, args[0], price)
}

// handleTakeProfitCommand 处理止盈设置指令（内部函数）
func handleTakeProfitCommand(ctx *CommandContext, symbol string, takeProfitPrice float64) string {
	symbol = market.Normalize(symbol)

	firstTrader, err := getFirstTrader(ctx)
	if err != nil {
		return fmt.Sprintf("❌ %s", err.Error())
	}

	// 获取持仓信息，找到该币种的持仓
	positions, err := firstTrader.GetPositions()
	if err != nil {
		return fmt.Sprintf("❌ 获取持仓信息失败: %v", err)
	}

	var targetPosition map[string]interface{}
	for _, pos := range positions {
		if pos["symbol"] == symbol {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Sprintf("❌ 未找到 %s 的持仓", symbol)
	}

	// 获取持仓方向和数量
	side, _ := targetPosition["side"].(string)
	quantity, _ := targetPosition["positionAmt"].(float64)
	if quantity < 0 {
		quantity = -quantity
	}

	positionSide := "LONG"
	if side == "short" {
		positionSide = "SHORT"
	}

	// 设置止盈
	traderInstance := firstTrader.GetTrader()
	if err := traderInstance.SetTakeProfit(symbol, positionSide, quantity, takeProfitPrice); err != nil {
		return fmt.Sprintf("❌ 设置止盈失败: %v", err)
	}

	return fmt.Sprintf("✅ 已设置 %s %s 止盈: $%.2f", symbol, side, takeProfitPrice)
}

// handleCloseCommandWithOTP 处理平仓指令（带用户 OTP 验证）
func handleCloseCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	if len(args) < 2 {
		return "❌ 请指定币种和方向\n示例: /close user@example.com BTCUSDT long 123456"
	}

	return handleCloseCommand(ctx, args[0], args[1])
}

// handleCloseCommand 处理平仓指令（内部函数）
func handleCloseCommand(ctx *CommandContext, symbol string, side string) string {
	symbol = market.Normalize(symbol)
	side = strings.ToLower(side)

	if side != "long" && side != "short" {
		return "❌ 方向必须是 long 或 short"
	}

	firstTrader, err := getFirstTrader(ctx)
	if err != nil {
		return fmt.Sprintf("❌ %s", err.Error())
	}

	// 执行平仓
	decision := &kernel.Decision{
		Symbol: symbol,
		Action: fmt.Sprintf("close_%s", side),
	}

	if err := firstTrader.ExecuteDecision(decision); err != nil {
		return fmt.Sprintf("❌ 平仓失败: %v", err)
	}

	return fmt.Sprintf("✅ 已平仓 %s %s", symbol, side)
}

// handleTradesCommandWithOTP 处理最近交易查询指令（带用户 OTP 验证）
func handleTradesCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	
	// 解析参数：symbol limit（可选）
	symbol := ""
	limit := 10
	
	if len(args) > 0 {
		// 第一个参数可能是币种或数量
		if limitVal, err := strconv.Atoi(args[0]); err == nil {
			limit = limitVal
		} else {
			symbol = market.Normalize(args[0])
		}
	}
	if len(args) > 1 {
		// 如果有第二个参数，且第一个是币种，则第二个是数量
		if symbol != "" {
			if limitVal, err := strconv.Atoi(args[1]); err == nil {
				limit = limitVal
			}
		}
	}
	
	// 限制数量范围
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}
	
	return handleTradesCommand(ctx, symbol, limit)
}

// handleTradesCommand 处理最近交易查询指令（内部函数）
func handleTradesCommand(ctx *CommandContext, symbol string, limit int) string {
	firstTrader, err := getFirstTrader(ctx)
	if err != nil {
		return fmt.Sprintf("❌ %s", err.Error())
	}
	
	// 获取存储接口
	store := firstTrader.GetStore()
	if store == nil {
		return "❌ 无法访问交易记录存储"
	}
	
	// 获取最近交易
	positionStore := store.Position()
	if positionStore == nil {
		return "❌ 无法访问交易记录存储"
	}
	
	trades, err := positionStore.GetRecentTrades(firstTrader.GetID(), limit)
	if err != nil {
		return fmt.Sprintf("❌ 获取交易记录失败: %v", err)
	}
	
	if len(trades) == 0 {
		msg := fmt.Sprintf("📊 <b>%s 最近交易记录</b>\n\n", firstTrader.GetName())
		msg += "暂无交易记录"
		return msg
	}
	
	// 过滤币种（如果指定）
	if symbol != "" {
		symbol = market.Normalize(symbol)
		var filteredTrades []map[string]interface{}
		for _, trade := range trades {
			if tradeSymbol, ok := trade["symbol"].(string); ok && tradeSymbol == symbol {
				filteredTrades = append(filteredTrades, trade)
			}
		}
		trades = filteredTrades
		if len(trades) == 0 {
			return fmt.Sprintf("❌ 未找到 %s 的交易记录", symbol)
		}
	}
	
	// 格式化消息
	msg := fmt.Sprintf("📊 <b>%s 最近交易记录</b>\n\n", firstTrader.GetName())
	if symbol != "" {
		msg += fmt.Sprintf("币种: <b>%s</b>\n", symbol)
	}
	msg += fmt.Sprintf("显示数量: %d\n\n", len(trades))
	
	for i, trade := range trades {
		symbolStr, _ := trade["symbol"].(string)
		sideStr, _ := trade["side"].(string)
		entryPrice, _ := trade["entry_price"].(float64)
		exitPrice, _ := trade["exit_price"].(float64)
		realizedPnL, _ := trade["realized_pnl"].(float64)
		pnlPct, _ := trade["pnl_pct"].(float64)
		holdDuration, _ := trade["hold_duration"].(string)
		
		sideEmoji := "📈"
		if sideStr == "short" {
			sideEmoji = "📉"
		}
		
		pnlEmoji := "🟢"
		if realizedPnL < 0 {
			pnlEmoji = "🔴"
		}
		
		msg += fmt.Sprintf("%d. %s <b>%s</b> %s\n", i+1, sideEmoji, symbolStr, sideStr)
		msg += fmt.Sprintf("   开仓: $%s\n", market.FormatPriceWithDynamicPrecision(entryPrice))
		msg += fmt.Sprintf("   平仓: $%s\n", market.FormatPriceWithDynamicPrecision(exitPrice))
		msg += fmt.Sprintf("   盈亏: %s $%.2f (%.2f%%)\n", pnlEmoji, realizedPnL, pnlPct)
		if holdDuration != "" {
			msg += fmt.Sprintf("   持仓时长: %s\n", holdDuration)
		}
		msg += "\n"
	}
	
	return msg
}

// handleTraderStatusCommandWithOTP 处理交易员状态查询指令（带用户 OTP 验证）
func handleTraderStatusCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	
	// 解析参数：traderID（可选）
	traderID := ""
	if len(args) > 0 {
		traderID = args[0]
	}
	
	return handleTraderStatusCommand(ctx, user.GetID(), traderID)
}

// handleTraderStatusCommand 处理交易员状态查询指令（内部函数）
func handleTraderStatusCommand(ctx *CommandContext, userID, traderID string) string {
	if ctx.Store == nil {
		return "❌ 无法访问交易员存储"
	}
	
	// 获取用户的所有交易员
	traders, err := ctx.Store.Trader().List(userID)
	if err != nil {
		return fmt.Sprintf("❌ 获取交易员列表失败: %v", err)
	}
	
	if len(traders) == 0 {
		return "❌ 未找到任何交易员"
	}
	
	// 如果指定了交易员ID，只显示该交易员
	if traderID != "" {
		for _, t := range traders {
			id := t.GetID()
			name := t.GetName()
			isRunning := t.IsRunning()
			
			if id == traderID || strings.HasPrefix(id, traderID) {
				// 获取实时状态
				if at, err := ctx.TraderManager.GetTrader(id); err == nil {
					status := at.GetStatus()
					if running, ok := status["is_running"].(bool); ok {
						isRunning = running
					}
				}
				
				statusEmoji := "🟢"
				statusText := "运行中"
				if !isRunning {
					statusEmoji = "🔴"
					statusText = "已停止"
				}
				
				msg := fmt.Sprintf("🤖 <b>交易员状态</b>\n\n")
				msg += fmt.Sprintf("名称: <b>%s</b>\n", name)
				msg += fmt.Sprintf("ID: <code>%s</code>\n", id)
				msg += fmt.Sprintf("状态: %s <b>%s</b>\n", statusEmoji, statusText)
				
				return msg
			}
		}
		return fmt.Sprintf("❌ 未找到交易员: %s", traderID)
	}
	
	// 显示所有交易员
	msg := fmt.Sprintf("🤖 <b>交易员列表</b>\n\n")
	msg += fmt.Sprintf("共 %d 个交易员:\n\n", len(traders))
	
	for i, t := range traders {
		id := t.GetID()
		name := t.GetName()
		isRunning := t.IsRunning()
		
		// 获取实时状态
		if at, err := ctx.TraderManager.GetTrader(id); err == nil {
			status := at.GetStatus()
			if running, ok := status["is_running"].(bool); ok {
				isRunning = running
			}
		}
		
		statusEmoji := "🟢"
		statusText := "运行中"
		if !isRunning {
			statusEmoji = "🔴"
			statusText = "已停止"
		}
		
		msg += fmt.Sprintf("%d. <b>%s</b> %s\n", i+1, name, statusEmoji)
		msg += fmt.Sprintf("   ID: <code>%s</code>\n", id)
		msg += fmt.Sprintf("   状态: %s\n\n", statusText)
	}
	
	return msg
}

// handleStartTraderCommandWithOTP 处理启用交易员指令（带用户 OTP 验证）
func handleStartTraderCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	
	if len(args) < 1 {
		return "❌ 请指定交易员ID\n示例: /start-trader user@example.com trader_id_123 123456"
	}
	
	traderID := args[0]
	return handleStartTraderCommand(ctx, user.GetID(), traderID)
}

// handleStartTraderCommand 处理启用交易员指令（内部函数）
func handleStartTraderCommand(ctx *CommandContext, userID, traderID string) string {
	if ctx.Store == nil {
		return "❌ 无法访问交易员存储"
	}
	
	// 验证交易员属于该用户
	traders, err := ctx.Store.Trader().List(userID)
	if err != nil {
		return fmt.Sprintf("❌ 获取交易员列表失败: %v", err)
	}
	
	var found bool
	var traderName string
	for _, t := range traders {
		id := t.GetID()
		name := t.GetName()
		
		if id == traderID || strings.HasPrefix(id, traderID) {
			found = true
			traderName = name
			traderID = id // 使用完整ID
			break
		}
	}
	
	if !found {
		return fmt.Sprintf("❌ 未找到交易员: %s\n\n请确认交易员ID是否正确，或使用 /trader 查看所有交易员", traderID)
	}
	
	// 检查交易员是否已在运行
	if at, err := ctx.TraderManager.GetTrader(traderID); err == nil {
		status := at.GetStatus()
		if running, ok := status["is_running"].(bool); ok && running {
			return fmt.Sprintf("✅ 交易员 <b>%s</b> 已在运行中", traderName)
		}
	}
	
	// 加载用户交易员（确保最新配置）
	if err := ctx.TraderManager.LoadUserTradersFromStore(ctx.Store, userID); err != nil {
		return fmt.Sprintf("❌ 加载交易员配置失败: %v", err)
	}
	
	// 获取交易员并启动
	trader, err := ctx.TraderManager.GetTrader(traderID)
	if err != nil {
		return fmt.Sprintf("❌ 获取交易员失败: %v\n\n请检查交易员的AI模型、交易所和策略配置", err)
	}
	
	// 启动交易员
	go func() {
		if err := trader.Run(); err != nil {
			// 使用 fmt 打印错误，因为 logger 可能不可用
			fmt.Printf("❌ Trader %s runtime error: %v\n", trader.GetName(), err)
		}
	}()
	
	// 更新数据库状态
	if err := ctx.Store.Trader().UpdateStatus(userID, traderID, true); err != nil {
		fmt.Printf("⚠️ Failed to update trader status: %v\n", err)
	}
	
	return fmt.Sprintf("✅ 交易员 <b>%s</b> 已启动", traderName)
}

// handleStopTraderCommandWithOTP 处理停用交易员指令（带用户 OTP 验证）
func handleStopTraderCommandWithOTP(ctx *CommandContext, update *tgbotapi.Update, user UserInterface) string {
	args := strings.Fields(update.Message.Text)
	
	if len(args) < 1 {
		return "❌ 请指定交易员ID\n示例: /stop-trader user@example.com trader_id_123 123456"
	}
	
	traderID := args[0]
	return handleStopTraderCommand(ctx, user.GetID(), traderID)
}

// handleStopTraderCommand 处理停用交易员指令（内部函数）
func handleStopTraderCommand(ctx *CommandContext, userID, traderID string) string {
	if ctx.Store == nil {
		return "❌ 无法访问交易员存储"
	}
	
	// 验证交易员属于该用户
	traders, err := ctx.Store.Trader().List(userID)
	if err != nil {
		return fmt.Sprintf("❌ 获取交易员列表失败: %v", err)
	}
	
	var found bool
	var traderName string
	for _, t := range traders {
		id := t.GetID()
		name := t.GetName()
		
		if id == traderID || strings.HasPrefix(id, traderID) {
			found = true
			traderName = name
			traderID = id // 使用完整ID
			break
		}
	}
	
	if !found {
		return fmt.Sprintf("❌ 未找到交易员: %s\n\n请确认交易员ID是否正确，或使用 /trader 查看所有交易员", traderID)
	}
	
	// 获取交易员
	trader, err := ctx.TraderManager.GetTrader(traderID)
	if err != nil {
		return fmt.Sprintf("❌ 交易员未在运行中: %s", traderID)
	}
	
	// 检查是否已停止
	status := trader.GetStatus()
	if running, ok := status["is_running"].(bool); ok && !running {
		return fmt.Sprintf("✅ 交易员 <b>%s</b> 已停止", traderName)
	}
	
	// 停止交易员
	trader.Stop()
	
	// 更新数据库状态
	if err := ctx.Store.Trader().UpdateStatus(userID, traderID, false); err != nil {
		fmt.Printf("⚠️ Failed to update trader status: %v\n", err)
	}
	
	return fmt.Sprintf("✅ 交易员 <b>%s</b> 已停止", traderName)
}
