# Telegram 配置分析文档

## 当前实现方案下的配置要求

### 一、配置方式

系统通过**环境变量**（`.env` 文件）配置 Telegram 功能。

### 二、配置优先级

系统按以下优先级读取配置：

1. **多 Bot 配置**（推荐）：`TELEGRAM_BOTS` 环境变量（JSON 格式）
2. **单 Bot 配置**（向后兼容）：`TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID`
3. **策略级配置**（已废弃）：策略配置中的 Telegram 设置

### 三、配置方案

#### 方案 A：多 Bot 配置（推荐，适用于按用户推送）

**环境变量配置：**

```bash
# 启用 Telegram 功能
TELEGRAM_ENABLED=true

# 多个 Telegram Bot 配置（JSON 格式）
TELEGRAM_BOTS='[
  {
    "token": "系统bot1的token",
    "chat_id": 系统bot1的ChatID,
    "webhook_url": "https://your-domain.com/api/telegram/webhook"
  },
  {
    "token": "系统bot2的token",
    "chat_id": 系统bot2的ChatID,
    "webhook_url": "https://your-domain.com/api/telegram/webhook"
  }
]'

# Webhook URL（所有 bot 共享，如果 bot 配置中没有 webhook_url，则使用此值）
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**配置说明：**

| 字段 | 说明 | 是否必需 | 示例 |
|------|------|----------|------|
| `token` | Bot Token（从 @BotFather 获取） | ✅ 必需 | `"123456789:ABCdefGHIjklMNOpqrsTUVwxyz"` |
| `chat_id` | Bot 的 Chat ID（用于识别 bot） | ✅ 必需 | `123456789` |
| `webhook_url` | Webhook URL（可选，优先使用此值） | ⚠️ 可选 | `"https://your-domain.com/api/telegram/webhook"` |

**注意事项：**

1. **`chat_id` 的作用**：
   - 在**多 Bot 配置**中，`chat_id` 主要用于**识别不同的 bot**
   - 系统通过 `chat_id` 匹配消息来源，路由到对应的 webhook 实例
   - **不是**用于限制消息接收者（因为现在支持按用户推送）

2. **`webhook_url` 的优先级**：
   - 如果 `TELEGRAM_BOTS` 中某个 bot 配置了 `webhook_url`，优先使用该值
   - 如果 bot 配置中没有 `webhook_url`，则使用 `TELEGRAM_WEBHOOK_URL` 环境变量
   - 如果两者都没有，webhook 将无法启动

3. **所有 bot 共享同一个 webhook URL**：
   - 所有 bot 的 webhook 都指向同一个 URL：`/api/telegram/webhook`
   - 系统通过消息中的 `chat.id` 字段识别消息来源，路由到对应的 bot 实例

#### 方案 B：单 Bot 配置（向后兼容）

**环境变量配置：**

```bash
# 启用 Telegram 功能
TELEGRAM_ENABLED=true

# 单个 Bot 配置（已废弃，建议使用 TELEGRAM_BOTS）
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**配置说明：**

| 环境变量 | 说明 | 是否必需 | 示例 |
|----------|------|----------|------|
| `TELEGRAM_ENABLED` | 是否启用 Telegram 功能 | ✅ 必需 | `true` |
| `TELEGRAM_BOT_TOKEN` | Bot Token | ✅ 必需 | `123456789:ABC...` |
| `TELEGRAM_CHAT_ID` | Chat ID | ✅ 必需 | `123456789` |
| `TELEGRAM_WEBHOOK_URL` | Webhook URL | ✅ 必需 | `https://your-domain.com/api/telegram/webhook` |

### 四、配置示例

#### 示例 1：单 Bot 配置（最简单）

```bash
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_CHAT_ID=123456789
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

#### 示例 2：多 Bot 配置（推荐）

```bash
TELEGRAM_ENABLED=true
TELEGRAM_BOTS='[{"token":"123456789:ABCdefGHIjklMNOpqrsTUVwxyz","chat_id":123456789,"webhook_url":"https://your-domain.com/api/telegram/webhook"}]'
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

#### 示例 3：多 Bot 配置（使用统一的 webhook_url）

```bash
TELEGRAM_ENABLED=true
TELEGRAM_BOTS='[{"token":"123456789:ABCdefGHIjklMNOpqrsTUVwxyz","chat_id":123456789},{"token":"987654321:XYZabcDEFghiJKLmnoPQRstuv","chat_id":987654321}]'
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

#### 示例 4：多 Bot 配置（混合方式，部分 bot 指定 webhook_url）

```bash
TELEGRAM_ENABLED=true
TELEGRAM_BOTS='[
  {
    "token": "123456789:ABCdefGHIjklMNOpqrsTUVwxyz",
    "chat_id": 123456789,
    "webhook_url": "https://custom-domain.com/api/telegram/webhook"
  },
  {
    "token": "987654321:XYZabcDEFghiJKLmnoPQRstuv",
    "chat_id": 987654321
  }
]'
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**说明：**
- Bot 1 使用自定义的 `webhook_url`：`https://custom-domain.com/api/telegram/webhook`
- Bot 2 使用统一的 `TELEGRAM_WEBHOOK_URL`：`https://your-domain.com/api/telegram/webhook`

### 五、配置加载流程

1. **读取环境变量**：
   - 系统启动时，`config.Init()` 读取所有环境变量
   - `TELEGRAM_BOTS` 作为 JSON 字符串存储

2. **解析配置**：
   - `config.GetTelegramBotConfigs()` 解析配置
   - 优先解析 `TELEGRAM_BOTS`（JSON 格式）
   - 如果 `TELEGRAM_BOTS` 为空，尝试使用单 Bot 配置（向后兼容）

3. **初始化 Webhook**：
   - `main.go` 中调用 `notification.NewMultiTelegramWebhook()`
   - 为每个 bot 创建 `TelegramWebhook` 实例
   - 所有 bot 共享同一个 webhook URL

4. **启动 Webhook**：
   - 调用 `multiWebhook.StartAllWebhooks(webhookURL)`
   - 为每个 bot 设置 webhook（使用统一的 URL）

### 六、按用户推送的配置要求

**对于按用户推送功能，最小配置要求：**

```bash
# 至少配置一个系统 bot
TELEGRAM_ENABLED=true
TELEGRAM_BOTS='[
  {
    "token": "系统bot的token",
    "chat_id": 系统bot的ChatID,
    "webhook_url": "https://your-domain.com/api/telegram/webhook"
  }
]'
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**关键点：**

1. **系统只需要一个 bot**：
   - 用户通过 `/login` 命令自动配置自己的 `telegram_chat_id`
   - 系统使用这个 bot 向所有用户发送通知
   - 不需要为每个用户配置单独的 bot

2. **`chat_id` 的作用**：
   - 在配置中，`chat_id` 用于识别系统 bot
   - 系统通过 `chat_id` 匹配消息来源，路由到对应的 webhook
   - **不是**用于限制消息接收者

3. **用户 ChatID 自动配置**：
   - 用户首次使用 `/login` 命令时，系统自动保存用户的 `telegram_chat_id`
   - 后续推送直接使用用户保存的 `telegram_chat_id`
   - 无需在配置文件中为每个用户配置

### 七、Webhook URL 要求

Telegram Webhook 必须满足以下要求：

1. **必须使用 HTTPS**（不支持 HTTP）
2. **端口限制**：只支持 `443`、`80`、`88`、`8443`
3. **公网可访问**：不能使用 `localhost` 或内网地址
4. **SSL 证书**：必须配置有效的 SSL 证书

**开发环境建议：**
- 使用 `ngrok` 或 `localtunnel` 创建 HTTPS 隧道
- 参考：`docs/guides/TELEGRAM_WEBHOOK_CONFIG.zh-CN.md`

### 八、配置验证

系统启动时会输出配置信息：

```
✓ MultiTelegramWebhook initialized with 1 webhook(s), all using unified URL: https://your-domain.com/api/telegram/webhook
```

如果配置有误，会输出警告：

```
⚠️ No valid Telegram webhooks configured
⚠️ TELEGRAM_WEBHOOK_URL is not set. Telegram Webhook will not be initialized.
```

### 九、常见问题

#### Q1: 为什么需要配置 `chat_id`？

**A:** `chat_id` 用于：
1. **识别 bot**：系统通过 `chat_id` 匹配消息来源，路由到对应的 webhook 实例
2. **向后兼容**：保持与旧版本配置的兼容性
3. **多 Bot 支持**：当配置多个 bot 时，用于区分不同的 bot

**注意**：`chat_id` **不是**用于限制消息接收者。系统现在支持按用户推送，会使用用户保存的 `telegram_chat_id` 发送消息。

#### Q2: 用户需要配置自己的 bot 吗？

**A:** 不需要。用户只需要：
1. 在 Telegram 中搜索系统 bot（例如：`@nofx_bot`）
2. 点击 "Start" 或发送 `/start`
3. 发送 `/login` 命令进行登录
4. 系统自动保存用户的 `telegram_chat_id`，后续自动推送

#### Q3: 可以配置多个 bot 吗？

**A:** 可以，但不必要。对于按用户推送功能：
- **推荐**：只配置一个系统 bot
- **可选**：配置多个 bot（例如：不同环境使用不同的 bot）

#### Q4: `webhook_url` 在配置中是否必需？

**A:** 不是必需的，但至少需要以下之一：
1. `TELEGRAM_BOTS` 中某个 bot 的 `webhook_url` 字段
2. `TELEGRAM_WEBHOOK_URL` 环境变量

如果两者都没有，webhook 将无法启动。

#### Q5: 所有 bot 必须使用同一个 webhook URL 吗？

**A:** 是的。当前实现中，所有 bot 的 webhook 都指向同一个 URL：`/api/telegram/webhook`。系统通过消息中的 `chat.id` 字段识别消息来源，路由到对应的 bot 实例。

### 十、配置最佳实践

1. **使用多 Bot 配置**（`TELEGRAM_BOTS`）：
   - 更灵活，支持多个 bot
   - 向后兼容单 Bot 配置

2. **统一使用 `TELEGRAM_WEBHOOK_URL`**：
   - 在 `TELEGRAM_BOTS` 中不设置 `webhook_url`
   - 统一使用 `TELEGRAM_WEBHOOK_URL` 环境变量
   - 便于管理和维护

3. **最小配置（按用户推送）**：
   ```bash
   TELEGRAM_ENABLED=true
   TELEGRAM_BOTS='[{"token":"系统bot的token","chat_id":系统bot的ChatID}]'
   TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
   ```

4. **生产环境建议**：
   - 使用固定的域名和 SSL 证书
   - 不要使用开发环境的临时 URL（如 ngrok）
   - 确保 webhook URL 稳定可用
