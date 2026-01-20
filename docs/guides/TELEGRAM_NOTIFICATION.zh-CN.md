# Telegram 通知配置指南

本指南将帮助您配置 Telegram Bot，以便在 AI 交易决策时接收实时通知。

## 功能特性

- ✅ **开仓通知**：包含价格、数量、杠杆、仓位大小、止损、止盈、信心度
- ✅ **平仓通知**：包含价格、数量、开仓价、盈亏
- ✅ **账户摘要**：总权益、可用余额、已用保证金、总盈亏、持仓数量
- ✅ **持仓详情**：每个持仓的符号、方向、数量、杠杆、开仓价、标记价、未实现盈亏
- ✅ **错误通知**：执行失败时发送错误信息

## 配置步骤

### 1. 创建 Telegram Bot

1. 打开 Telegram，搜索 `@BotFather`
2. 发送 `/newbot` 命令
3. 按照提示设置 Bot 名称和用户名
4. BotFather 会返回一个 **Bot Token**，格式类似：`1234567890:ABCdefGHIjklMNOpqrsTUVwxyz`
5. **保存这个 Token**，稍后会用到

### 2. 获取 Chat ID

有两种方法获取您的 Chat ID：

#### 方法一：通过 Bot 对话获取

1. 在 Telegram 中搜索您刚创建的 Bot（使用 BotFather 返回的用户名）
2. 点击 "Start" 或发送 `/start` 开始对话
3. 访问以下 URL（将 `YOUR_BOT_TOKEN` 替换为您的 Bot Token）：
   ```
   https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates
   ```
4. 在返回的 JSON 中找到 `"chat":{"id":123456789}`，这个数字就是您的 **Chat ID**

#### 方法二：使用 @userinfobot

1. 在 Telegram 中搜索 `@userinfobot`
2. 发送任意消息
3. Bot 会返回您的 User ID（这就是 Chat ID）

### 3. 配置 Telegram Bot（推荐方式：环境变量）

#### 方式一：通过环境变量配置（推荐）

1. 在项目根目录找到或创建 `.env` 文件
2. 添加以下配置：

```bash
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here
```

**完整示例：**

```bash
# Telegram Bot Configuration
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=1234567890:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_CHAT_ID=123456789
```

3. 保存文件并重启应用
4. 所有交易员将共享此 Telegram Bot 配置

**优点：**
- ✅ 集中管理，所有交易员共享同一配置
- ✅ 便于维护和更新
- ✅ 符合 12-factor app 原则
- ✅ 配置与代码分离，更安全

#### 方式二：通过策略配置（已弃用，保留用于向后兼容）

> ⚠️ **注意**：此方式已弃用，建议使用环境变量配置。如果环境变量中已配置 Telegram，策略配置将被忽略。

在策略配置的 JSON 中添加 Telegram 配置：

```json
{
  "telegram": {
    "enabled": true,
    "token": "YOUR_BOT_TOKEN",
    "chat_id": YOUR_CHAT_ID
  }
}
```

### 4. 验证配置

1. 保存配置（`.env` 文件或策略配置）
2. 启动或重启交易员
3. 查看日志，应该看到类似以下信息：
   ```
   📱 [Trader Name] Using Telegram config from environment variables
   ✓ Telegram bot initialized: @your_bot_name
   ✓ [Trader Name] Telegram notifications enabled
   ```

如果使用策略配置（已弃用），会看到：
```
📱 [Trader Name] Using Telegram config from strategy (deprecated, use .env instead)
```

## 通知示例

### 开仓通知示例

```
📈 My Trader - BTCUSDT open_long
💰 价格: 43250.00
📊 数量: 0.02311500
⚡ 杠杆: 10x
💵 仓位: $1000.00
🛑 止损: 42000.00
🎯 止盈: 45000.00
🎲 信心度: 85%
```

### 平仓通知示例

```
🔄 My Trader - ETHUSDT close_long
💰 价格: 2650.00
📊 数量: 0.37735849
📥 开仓价: 2600.00
📈 盈亏: $18.87
```

### 账户信息示例

```
📊 My Trader - 账户信息

💼 总权益: $10500.00
💰 可用余额: $2000.00
📌 已用保证金: $8500.00
📈 总盈亏: $500.00 (4.76%)
📋 持仓数量: 2
```

### 持仓信息示例

```
📋 My Trader - 持仓信息

1. 📈 BTCUSDT long
   数量: 0.02311500 | 杠杆: 10x
   开仓价: 43250.00 | 标记价: 43500.00
   📈 未实现盈亏: $5.78

2. 📉 ETHUSDT short
   数量: 0.37735849 | 杠杆: 5x
   开仓价: 2650.00 | 标记价: 2630.00
   📈 未实现盈亏: $7.55
```

## 故障排除

### Bot 未初始化

**问题：** 日志中看不到 "Telegram bot initialized" 消息

**解决方案：**
- 检查 Bot Token 是否正确
- 确认 Token 格式正确（应包含冒号）
- 检查网络连接

### 未收到通知

**问题：** Bot 已初始化，但未收到通知

**解决方案：**
1. 确认 Chat ID 正确（必须是数字，不是字符串）
2. 确认已与 Bot 开始对话（发送 `/start`）
3. 检查策略配置中 `enabled` 为 `true`
4. 查看日志中是否有错误信息

### 权限错误

**问题：** 收到 "operation not permitted" 错误

**解决方案：**
- 这通常是系统权限问题，不影响 Bot 功能
- 如果 Bot 已成功初始化，通知功能应该正常工作

## 注意事项

1. **安全性**：Bot Token 具有完全访问权限，请妥善保管，不要泄露
2. **频率**：每次 AI 决策后都会发送通知，请确保不会产生过多消息
3. **隐私**：通知中包含交易信息，请确保 Telegram 账户安全
4. **测试**：建议先用小金额测试，确认通知功能正常后再正式使用

## 技术支持

如遇到问题，请：
1. 查看日志文件中的错误信息
2. 检查 Telegram Bot API 状态
3. 访问 [NOFX 开发者社区](https://t.me/nofx_dev_community) 寻求帮助

