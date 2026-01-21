package notification

import (
	"fmt"
	"nofx/auth"
	"nofx/kernel"
	"nofx/market"
	"strconv"
	"strings"

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
}

// TraderInterface 交易员接口（避免循环导入）
type TraderInterface interface {
	GetName() string
	GetAccountInfo() (map[string]interface{}, error)
	GetPositions() ([]map[string]interface{}, error)
	ExecuteDecision(*kernel.Decision) error
	GetTrader() interface {
		SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error
		SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error
	}
}

// TraderManagerInterface 交易员管理器接口（避免循环导入）
type TraderManagerInterface interface {
	GetAllTraders() map[string]TraderInterface
}

// CommandContext 指令处理上下文
type CommandContext struct {
	TraderManager TraderManagerInterface
	UserStore     UserStoreInterface
}

// CreateCommandHandlers 创建指令处理器
func CreateCommandHandlers(ctx *CommandContext) map[string]CommandHandler {
	handlers := make(map[string]CommandHandler)

	// /account - 查看账户及持仓（需要邮箱和OTP）
	handlers["/account"] = func(update *tgbotapi.Update) string {
		return handleCommandWithUserIDAndOTP(ctx, update, handleAccountCommandWithOTP)
	}

	// /price - 查看币种价格（无需 OTP）
	handlers["/price"] = func(update *tgbotapi.Update) string {
		args := strings.Fields(update.Message.Text)
		if len(args) < 1 {
			return "❌ 请指定币种\n示例: /price BTCUSDT"
		}
		return handlePriceCommand(ctx, args[0])
	}

	// /sl - 设置止损（需要邮箱和OTP）
	handlers["/sl"] = func(update *tgbotapi.Update) string {
		return handleCommandWithUserIDAndOTP(ctx, update, handleStopLossCommandWithOTP)
	}

	// /tp - 设置止盈（需要邮箱和OTP）
	handlers["/tp"] = func(update *tgbotapi.Update) string {
		return handleCommandWithUserIDAndOTP(ctx, update, handleTakeProfitCommandWithOTP)
	}

	// /close - 平仓（需要邮箱和OTP）
	handlers["/close"] = func(update *tgbotapi.Update) string {
		return handleCommandWithUserIDAndOTP(ctx, update, handleCloseCommandWithOTP)
	}

	// /help - 帮助
	handlers["/help"] = func(update *tgbotapi.Update) string {
		return `📋 <b>可用指令：</b>

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
- 只有 /price 和 /help 指令无需验证码
- 其他所有指令都需要提供邮箱和 Google Authenticator 验证码
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

// handleCommandWithEmailAndOTP 处理需要邮箱和OTP验证的指令
func handleCommandWithUserIDAndOTP(ctx *CommandContext, update *tgbotapi.Update, handler func(*CommandContext, *tgbotapi.Update, UserInterface) string) string {
	args := strings.Fields(update.Message.Text)

	if len(args) < 2 {
		return "❌ 参数不足。操作指令需要邮箱和 Google Authenticator 验证码。\n示例: /account user@example.com 123456\n示例: /sl user@example.com BTCUSDT 42000 123456"
	}

	// 第一个参数是邮箱，最后一个参数是 OTP
	email := args[0]
	otpCode := args[len(args)-1]

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

	// 移除邮箱和OTP参数，保留中间的操作参数
	// 格式: /account email OTP -> (空)
	// 格式: /sl email symbol price OTP -> symbol price
	// 格式: /close email symbol side OTP -> symbol side
	if len(args) > 2 {
		update.Message.Text = strings.Join(args[1:len(args)-1], " ")
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
	msg += fmt.Sprintf("📊 价格: $%.2f\n", marketData.CurrentPrice)

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
			msg += fmt.Sprintf("📊 区间最高: $%.2f\n", high)
			msg += fmt.Sprintf("📊 区间最低: $%.2f", low)
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
