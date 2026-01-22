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

// SendMessage 发送消息
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

// FormatDecisionMessage 格式化决策消息
func FormatDecisionMessage(traderName, symbol, action string, details map[string]interface{}) string {
	var emoji string
	switch action {
	case "open_long":
		emoji = "📈"
	case "open_short":
		emoji = "📉"
	case "close_long", "close_short":
		emoji = "🔄"
	case "hold":
		emoji = "⏸"
	case "wait":
		emoji = "⏳"
	default:
		emoji = "ℹ️"
	}

	msg := fmt.Sprintf("%s <b>%s</b> - %s %s\n", emoji, traderName, symbol, action)

	if price, ok := details["price"].(float64); ok && price > 0 {
		msg += fmt.Sprintf("💰 价格: %s\n", market.FormatPriceWithDynamicPrecision(price))
	}
	if quantity, ok := details["quantity"].(float64); ok && quantity > 0 {
		msg += fmt.Sprintf("📊 数量: %.8f\n", quantity)
	}
	if leverage, ok := details["leverage"].(int); ok && leverage > 0 {
		msg += fmt.Sprintf("⚡ 杠杆: %dx\n", leverage)
	}
	if positionSize, ok := details["position_size_usd"].(float64); ok && positionSize > 0 {
		msg += fmt.Sprintf("💵 仓位: $%.2f\n", positionSize)
	}
	if stopLoss, ok := details["stop_loss"].(float64); ok && stopLoss > 0 {
		msg += fmt.Sprintf("🛑 止损: %s\n", market.FormatPriceWithDynamicPrecision(stopLoss))
	}
	if takeProfit, ok := details["take_profit"].(float64); ok && takeProfit > 0 {
		msg += fmt.Sprintf("🎯 止盈: %s\n", market.FormatPriceWithDynamicPrecision(takeProfit))
	}
	if confidence, ok := details["confidence"].(int); ok {
		msg += fmt.Sprintf("🎲 信心度: %d%%\n", confidence)
	}
	if entryPrice, ok := details["entry_price"].(float64); ok && entryPrice > 0 {
		msg += fmt.Sprintf("📥 开仓价: %s\n", market.FormatPriceWithDynamicPrecision(entryPrice))
	}
	if pnl, ok := details["pnl"].(float64); ok {
		pnlEmoji := "📈"
		if pnl < 0 {
			pnlEmoji = "📉"
		}
		msg += fmt.Sprintf("%s 盈亏: $%.2f\n", pnlEmoji, pnl)
	}
	if errorMsg, ok := details["error"].(string); ok && errorMsg != "" {
		msg += fmt.Sprintf("❌ 错误: %s\n", errorMsg)
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
