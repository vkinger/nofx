package notification

import (
	"fmt"
	"nofx/logger"
	"nofx/market"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramNotifier Telegram通知服务
type TelegramNotifier struct {
	bot     *tgbotapi.BotAPI
	chatID  int64
	enabled bool
}

// NewTelegramNotifier 创建Telegram通知服务
func NewTelegramNotifier(token string, chatID int64) (*TelegramNotifier, error) {
	if token == "" || chatID == 0 {
		return &TelegramNotifier{enabled: false}, nil
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	logger.Infof("✓ Telegram bot initialized: @%s", bot.Self.UserName)

	return &TelegramNotifier{
		bot:     bot,
		chatID:  chatID,
		enabled: true,
	}, nil
}

// SendMessage 发送消息（发送到配置的 ChatID）
func (tn *TelegramNotifier) SendMessage(text string) error {
	if !tn.enabled {
		return nil
	}

	msg := tgbotapi.NewMessage(tn.chatID, text)
	msg.ParseMode = "HTML"

	_, err := tn.bot.Send(msg)
	if err != nil {
		logger.Errorf("Failed to send telegram message: %v", err)
		return err
	}
	return nil
}

// SendMessageToChatID 发送消息到指定的 ChatID（支持动态 ChatID）
func (tn *TelegramNotifier) SendMessageToChatID(chatID int64, text string) error {
	if !tn.enabled {
		return nil
	}

	if chatID == 0 {
		return nil
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"

	_, err := tn.bot.Send(msg)
	if err != nil {
		logger.Errorf("Failed to send telegram message to ChatID %d: %v", chatID, err)
		return err
	}
	return nil
}

// GetChatID 获取配置的 ChatID（用于按用户推送匹配）
func (tn *TelegramNotifier) GetChatID() int64 {
	return tn.chatID
}

// FormatDecisionMessage 格式化决策消息
func FormatDecisionMessage(traderName, symbol, action string, details map[string]interface{}) string {
	var emoji string
	var actionText string
	isOpenAction := false
	switch action {
	case "open_long":
		emoji = "📈"
		actionText = "开多"
		isOpenAction = true
	case "open_short":
		emoji = "📉"
		actionText = "开空"
		isOpenAction = true
	case "close_long":
		emoji = "🔄"
		actionText = "平多"
	case "close_short":
		emoji = "🔄"
		actionText = "平空"
	case "hold":
		emoji = "⏸"
		actionText = "持仓"
	case "wait":
		emoji = "⏳"
		actionText = "等待"
	default:
		emoji = "ℹ️"
		actionText = action
	}

	msg := fmt.Sprintf("%s <b>%s</b> - %s %s\n\n", emoji, traderName, symbol, actionText)

	// 账户信息（操作前）
	if avail, ok := details["available_balance"].(float64); ok && avail > 0 {
		msg += fmt.Sprintf("💰 可用余额: $%.2f\n", avail)
	}

	// 开仓操作：显示仓位信息
	if isOpenAction {
		if price, ok := details["price"].(float64); ok && price > 0 {
			msg += fmt.Sprintf("📍 开仓价: %s\n", market.FormatPriceWithDynamicPrecision(price))
		}
		if quantity, ok := details["quantity"].(float64); ok && quantity > 0 {
			msg += fmt.Sprintf("📊 数量: %.6f\n", quantity)
		}
		if leverage, ok := details["leverage"].(int); ok && leverage > 0 {
			msg += fmt.Sprintf("⚡ 杠杆: %dx\n", leverage)
		}
		if positionSize, ok := details["position_size_usd"].(float64); ok && positionSize > 0 {
			msg += fmt.Sprintf("💵 仓位价值: $%.2f\n", positionSize)
		}
		if stopLoss, ok := details["stop_loss"].(float64); ok && stopLoss > 0 {
			msg += fmt.Sprintf("🛑 止损: %s\n", market.FormatPriceWithDynamicPrecision(stopLoss))
		}
		if takeProfit, ok := details["take_profit"].(float64); ok && takeProfit > 0 {
			msg += fmt.Sprintf("🎯 止盈: %s\n", market.FormatPriceWithDynamicPrecision(takeProfit))
		}
		if confidence, ok := details["confidence"].(int); ok && confidence > 0 {
			msg += fmt.Sprintf("🎲 信心度: %d%%\n", confidence)
		}
	} else {
		// 平仓操作：显示盈亏信息
		if entryPrice, ok := details["entry_price"].(float64); ok && entryPrice > 0 {
			msg += fmt.Sprintf("📥 开仓价: %s\n", market.FormatPriceWithDynamicPrecision(entryPrice))
		}
		if price, ok := details["price"].(float64); ok && price > 0 {
			msg += fmt.Sprintf("📤 平仓价: %s\n", market.FormatPriceWithDynamicPrecision(price))
		}
		if quantity, ok := details["quantity"].(float64); ok && quantity > 0 {
			msg += fmt.Sprintf("📊 数量: %.6f\n", quantity)
		}
		if pnl, ok := details["pnl"].(float64); ok {
			pnlEmoji := "📈"
			if pnl < 0 {
				pnlEmoji = "📉"
			}
			pnlStr := fmt.Sprintf("%s 盈亏: $%.2f", pnlEmoji, pnl)
			if pnlPct, ok := details["pnl_pct"].(float64); ok {
				pnlStr += fmt.Sprintf(" (%.2f%%)", pnlPct)
			}
			msg += pnlStr + "\n"
		}
	}

	if errorMsg, ok := details["error"].(string); ok && errorMsg != "" {
		msg += fmt.Sprintf("\n❌ 错误: %s\n", errorMsg)
	}

	return msg
}

// FormatRiskControlCloseMessage 格式化风控系统平仓通知消息
func FormatRiskControlCloseMessage(traderName, symbol, side, strategy string, details map[string]interface{}) string {
	emoji := "🛡️"
	msg := fmt.Sprintf("%s <b>%s</b> - 风控平仓\n", emoji, traderName)
	msg += fmt.Sprintf("📌 策略: %s\n", strategy)
	msg += fmt.Sprintf("💰 币种: %s %s\n", symbol, side)

	if entryPrice, ok := details["entry_price"].(float64); ok && entryPrice > 0 {
		msg += fmt.Sprintf("📥 开仓价: %s\n", market.FormatPriceWithDynamicPrecision(entryPrice))
	}
	if exitPrice, ok := details["exit_price"].(float64); ok && exitPrice > 0 {
		msg += fmt.Sprintf("📤 平仓价: %s\n", market.FormatPriceWithDynamicPrecision(exitPrice))
	}
	if quantity, ok := details["quantity"].(float64); ok && quantity > 0 {
		msg += fmt.Sprintf("📊 数量: %.8f\n", quantity)
	}
	if margin, ok := details["margin"].(float64); ok && margin > 0 {
		msg += fmt.Sprintf("💵 保证金: $%.2f\n", margin)
	}
	if pnl, ok := details["pnl"].(float64); ok {
		pnlEmoji := "📈"
		if pnl < 0 {
			pnlEmoji = "📉"
		}
		msg += fmt.Sprintf("%s 盈亏: $%.2f", pnlEmoji, pnl)
		if pnlPct, ok := details["pnl_pct"].(float64); ok {
			msg += fmt.Sprintf(" (%.2f%%)", pnlPct)
		}
		msg += "\n"
	}
	if peakProfit, ok := details["peak_profit"].(float64); ok && peakProfit > 0 {
		msg += fmt.Sprintf("📊 峰值利润: %.2f%%\n", peakProfit)
	}
	if drawdown, ok := details["drawdown"].(float64); ok && drawdown > 0 {
		msg += fmt.Sprintf("📉 回撤: %.2f%%\n", drawdown)
	}

	return msg
}

// FormatDrawdownWarningMessage 格式化回撤监控警告消息
func FormatDrawdownWarningMessage(traderName, symbol, side string, details map[string]interface{}) string {
	emoji := "⚠️"
	msg := fmt.Sprintf("%s <b>%s</b> - 回撤监控警告\n", emoji, traderName)
	msg += fmt.Sprintf("💰 币种: %s %s\n", symbol, side)

	if currentProfit, ok := details["current_profit"].(float64); ok {
		msg += fmt.Sprintf("📊 当前利润: %.2f%%\n", currentProfit)
	}
	if peakProfit, ok := details["peak_profit"].(float64); ok && peakProfit > 0 {
		msg += fmt.Sprintf("📈 峰值利润: %.2f%%\n", peakProfit)
	}
	if drawdown, ok := details["drawdown"].(float64); ok && drawdown > 0 {
		msg += fmt.Sprintf("📉 回撤: %.2f%%\n", drawdown)
	}
	if threshold, ok := details["threshold"].(float64); ok && threshold > 0 {
		msg += fmt.Sprintf("🛑 阈值: %.2f%%\n", threshold)
	}
	if realPnl, ok := details["real_pnl"].(float64); ok {
		msg += fmt.Sprintf("💵 实际盈亏: %.2f%%\n", realPnl)
	}
	if realDrawdown, ok := details["real_drawdown"].(float64); ok && realDrawdown > 0 {
		msg += fmt.Sprintf("📉 实际回撤: %.2f%%\n", realDrawdown)
	}
	if warningType, ok := details["warning_type"].(string); ok {
		msg += fmt.Sprintf("⚠️ 类型: %s\n", warningType)
	}

	return msg
}

// FormatAccountInfoMessage 格式化账户信息消息
func FormatAccountInfoMessage(traderName string, accountInfo map[string]interface{}) string {
	msg := fmt.Sprintf("📊 <b>%s - 账户信息</b>\n\n", traderName)

	if equity, ok := accountInfo["total_equity"].(float64); ok {
		msg += fmt.Sprintf("💼 总权益: $%.2f\n", equity)
	}
	if available, ok := accountInfo["available_balance"].(float64); ok {
		msg += fmt.Sprintf("💰 可用余额: $%.2f\n", available)
	}
	if marginUsed, ok := accountInfo["margin_used"].(float64); ok {
		msg += fmt.Sprintf("📌 已用保证金: $%.2f\n", marginUsed)
	}
	if pnl, ok := accountInfo["total_pnl"].(float64); ok {
		pnlEmoji := "📈"
		if pnl < 0 {
			pnlEmoji = "📉"
		}
		msg += fmt.Sprintf("%s 总盈亏: $%.2f", pnlEmoji, pnl)
		if pnlPct, ok := accountInfo["total_pnl_pct"].(float64); ok {
			msg += fmt.Sprintf(" (%.2f%%)\n", pnlPct)
		} else {
			msg += "\n"
		}
	}
	if positionCount, ok := accountInfo["position_count"].(int); ok {
		msg += fmt.Sprintf("📋 持仓数量: %d\n", positionCount)
	}

	return msg
}

// FormatPositionsMessage 格式化持仓信息消息（详细版）
func FormatPositionsMessage(traderName string, positions []map[string]interface{}) string {
	if len(positions) == 0 {
		return fmt.Sprintf("📋 <b>%s - 持仓信息</b>\n\n无持仓", traderName)
	}

	msg := fmt.Sprintf("📋 <b>%s - 持仓信息</b>\n\n", traderName)

	for i, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		side, _ := pos["side"].(string)

		// 支持两种字段名格式：驼峰格式（positionAmt）和下划线格式（quantity）
		var quantity float64
		if qty, ok := pos["quantity"].(float64); ok {
			quantity = qty
		} else if qty, ok := pos["positionAmt"].(float64); ok {
			quantity = qty
		}
		if quantity < 0 {
			quantity = -quantity
		}

		// 支持两种字段名格式：驼峰格式（entryPrice）和下划线格式（entry_price）
		var entryPrice float64
		if ep, ok := pos["entry_price"].(float64); ok {
			entryPrice = ep
		} else if ep, ok := pos["entryPrice"].(float64); ok {
			entryPrice = ep
		}

		// 支持两种字段名格式：驼峰格式（markPrice）和下划线格式（mark_price）
		var markPrice float64
		if mp, ok := pos["mark_price"].(float64); ok {
			markPrice = mp
		} else if mp, ok := pos["markPrice"].(float64); ok {
			markPrice = mp
		}

		// 支持两种字段名格式：驼峰格式（unRealizedProfit）和下划线格式（unrealized_pnl）
		var unrealizedPnl float64
		if pnl, ok := pos["unrealized_pnl"].(float64); ok {
			unrealizedPnl = pnl
		} else if pnl, ok := pos["unRealizedProfit"].(float64); ok {
			unrealizedPnl = pnl
		}

		// 杠杆
		var leverage float64
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = lev
		} else if lev, ok := pos["leverage"].(int); ok {
			leverage = float64(lev)
		}

		// 获取额外信息（如果存在）- 支持两种格式
		var liquidationPrice float64
		if lp, ok := pos["liquidation_price"].(float64); ok {
			liquidationPrice = lp
		} else if lp, ok := pos["liquidationPrice"].(float64); ok {
			liquidationPrice = lp
		}

		var marginUsed float64
		if mu, ok := pos["margin_used"].(float64); ok {
			marginUsed = mu
		} else if mu, ok := pos["marginUsed"].(float64); ok {
			marginUsed = mu
		}

		var positionValue float64
		if pv, ok := pos["positionValue"].(float64); ok {
			positionValue = pv
		} else if pv, ok := pos["position_value"].(float64); ok {
			positionValue = pv
		}

		unrealizedPnlPct, _ := pos["unrealized_pnl_pct"].(float64)

		// 如果没有 positionValue，计算它
		if positionValue == 0 && markPrice > 0 && quantity > 0 {
			positionValue = quantity * markPrice
		}

		// 如果没有 marginUsed，计算它
		if marginUsed == 0 && leverage > 0 && positionValue > 0 {
			marginUsed = positionValue / leverage
		}

		// 计算价格变化百分比
		var priceChangePct float64
		if entryPrice > 0 {
			if side == "long" {
				priceChangePct = ((markPrice - entryPrice) / entryPrice) * 100
			} else {
				priceChangePct = ((entryPrice - markPrice) / entryPrice) * 100
			}
		}

		// 如果没有 unrealizedPnlPct，计算它
		if unrealizedPnlPct == 0 && marginUsed > 0 {
			unrealizedPnlPct = (unrealizedPnl / marginUsed) * 100
		}

		sideEmoji := "📈"
		if side == "short" {
			sideEmoji = "📉"
		}

		// 持仓标题
		msg += fmt.Sprintf("<b>%d. %s %s %s</b>\n", i+1, sideEmoji, symbol, strings.ToUpper(side))

		// 基础信息
		msg += fmt.Sprintf("   💰 数量: <b>%.8f</b> | 杠杆: <b>%.0fx</b>\n", quantity, leverage)

		// 价格信息（使用动态精度）
		priceChangeEmoji := "📈"
		if priceChangePct < 0 {
			priceChangeEmoji = "📉"
		}
		msg += fmt.Sprintf("   💵 开仓价: <b>%s</b> | 标记价: <b>%s</b> (%s%.2f%%)\n",
			market.FormatPriceWithDynamicPrecision(entryPrice),
			market.FormatPriceWithDynamicPrecision(markPrice),
			priceChangeEmoji, priceChangePct)

		// 持仓价值
		if positionValue > 0 {
			msg += fmt.Sprintf("   💎 持仓价值: <b>$%.2f</b>\n", positionValue)
		}

		// 保证金信息
		if marginUsed > 0 {
			msg += fmt.Sprintf("   🔒 已用保证金: <b>$%.2f</b>\n", marginUsed)
		}

		// 盈亏信息
		pnlEmoji := "📈"
		if unrealizedPnl < 0 {
			pnlEmoji = "📉"
		}
		if unrealizedPnlPct != 0 {
			msg += fmt.Sprintf("   %s 未实现盈亏: <b>$%.2f</b> (<b>%.2f%%</b>)\n",
				pnlEmoji, unrealizedPnl, unrealizedPnlPct)
		} else {
			msg += fmt.Sprintf("   %s 未实现盈亏: <b>$%.2f</b>\n", pnlEmoji, unrealizedPnl)
		}

		// 强平价（使用动态精度）
		if liquidationPrice > 0 {
			liqDistance := 0.0
			if side == "long" {
				liqDistance = ((markPrice - liquidationPrice) / markPrice) * 100
			} else {
				liqDistance = ((liquidationPrice - markPrice) / markPrice) * 100
			}
			msg += fmt.Sprintf("   ⚠️ 强平价: <b>%s</b> (距离: <b>%.2f%%</b>)\n",
				market.FormatPriceWithDynamicPrecision(liquidationPrice), liqDistance)
		}

		msg += "\n"
	}

	return msg
}
