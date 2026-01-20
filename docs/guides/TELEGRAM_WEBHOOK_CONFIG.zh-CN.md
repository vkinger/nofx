# Telegram Bot Webhook 配置指南

本文档详细说明如何配置和使用 Telegram Bot Webhook 功能，包括通知和指令控制。

## 目录

- [功能概述](#功能概述)
- [前置准备](#前置准备)
- [基础配置](#基础配置)
- [Webhook 配置](#webhook-配置)
- [安全设置](#安全设置)
- [指令使用](#指令使用)
- [故障排查](#故障排查)
- [高级配置](#高级配置)

## 功能概述

Telegram Bot Webhook 提供以下功能：

1. **交易通知**：自动推送开仓、平仓、止盈止损等交易信息
2. **账户查询**：查看账户余额和持仓信息
3. **价格查询**：查询指定币种的当前价格
4. **止盈止损设置**：通过指令设置止盈止损
5. **平仓操作**：通过指令快速平仓

## 前置准备

### 1. 创建 Telegram Bot

1. 在 Telegram 中搜索 `@BotFather`
2. 发送 `/newbot` 创建新机器人
3. 按提示设置机器人名称和用户名
4. 获取 Bot Token（格式：`123456789:ABCdefGHIjklMNOpqrsTUVwxyz`）

### 2. 获取 Chat ID

**方法一：使用 @userinfobot**
1. 在 Telegram 中搜索 `@userinfobot`
2. 发送任意消息
3. 机器人会返回你的 Chat ID（数字格式）

**方法二：通过 API 获取**
1. 发送消息给你的 Bot
2. 访问：`https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
3. 在返回的 JSON 中找到 `chat.id` 字段

## 基础配置

### 环境变量配置

在 `.env` 文件中添加以下配置：

```bash
# Telegram Bot 基础配置
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# Webhook URL（必需）
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

### 配置说明

| 配置项 | 说明 | 是否必需 | 示例 |
|--------|------|----------|------|
| `TELEGRAM_ENABLED` | 是否启用 Telegram 功能 | 是 | `true` |
| `TELEGRAM_BOT_TOKEN` | Bot Token（从 @BotFather 获取） | 是 | `123456789:ABC...` |
| `TELEGRAM_CHAT_ID` | 你的 Chat ID（数字） | 是 | `123456789` |
| `TELEGRAM_WEBHOOK_URL` | Webhook URL（用于接收指令） | 是 | `https://your-domain.com/api/telegram/webhook` |

## Webhook 配置

### Webhook URL 要求

Telegram Webhook 必须满足以下要求：

1. **必须使用 HTTPS**（不支持 HTTP）
2. **端口限制**：只支持 `443`、`80`、`88`、`8443`
3. **公网可访问**：不能使用 `localhost` 或内网地址
4. **SSL 证书**：必须配置有效的 SSL 证书

### 使用域名（推荐）

```bash
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**优点**：
- 可以使用 Let's Encrypt 免费证书
- 配置简单，维护方便
- 符合最佳实践

**配置步骤**：
1. 确保域名已解析到服务器 IP
2. 使用 Nginx 配置反向代理和 SSL
3. 参考下面的 Nginx 配置示例

### 使用 IP 地址

```bash
TELEGRAM_WEBHOOK_URL=https://123.45.67.89:443/api/telegram/webhook
```

**要求**：
- SSL 证书的 CN（Common Name）必须设置为该 IP 地址
- 需要使用自签名证书
- 配置相对复杂

**不推荐**：除非无法使用域名，否则建议使用域名方式。

### Nginx 反向代理配置

由于 Telegram 只支持特定端口（443、80、88、8443），而 API 服务器通常运行在其他端口（如 8080），需要通过 Nginx 反向代理。

#### 配置示例

```nginx
server {
    listen 443 ssl http2;
    server_name your-domain.com;  # 替换为你的域名
    
    # SSL 证书配置（使用 Let's Encrypt）
    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;
    
    # SSL 安全配置
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    
    # Telegram Webhook 端点（带 IP 白名单）
    location /api/telegram/webhook {
        # 限制只允许 Telegram 官方 IP 段（可选，代码中已实现）
        # allow 149.154.160.0/20;
        # allow 91.108.4.0/22;
        # deny all;
        
        proxy_pass http://127.0.0.1:8080;  # 后端 API 服务器地址
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # 超时设置
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
    
    # 其他 API 端点
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    
    # 前端静态文件
    location / {
        root /path/to/frontend/dist;
        try_files $uri $uri/ /index.html;
    }
}
```

#### 使用 Let's Encrypt 获取免费 SSL 证书

```bash
# 安装 certbot
sudo apt-get update
sudo apt-get install certbot python3-certbot-nginx

# 获取证书
sudo certbot --nginx -d your-domain.com

# 自动续期（已自动配置）
sudo certbot renew --dry-run
```

## 安全设置

### 内置安全功能

系统已实现以下安全措施：

1. **IP 白名单**：只允许来自 Telegram 官方 IP 段的请求
   - `149.154.160.0/20`（Telegram 主要 IP 段）
   - `91.108.4.0/22`（Telegram 备用 IP 段）

2. **速率限制**：每个 IP 每分钟最多 30 个请求

3. **ChatID 验证**：只处理来自配置的 Chat ID 的消息

### 安全层级

```
请求 → IP 白名单检查 → 速率限制检查 → ChatID 验证 → 处理请求
```

### 额外安全建议

1. **防火墙配置**：在服务器防火墙中限制只允许 Telegram IP 段访问 443 端口
2. **Nginx IP 限制**：在 Nginx 配置中添加 IP 白名单（见上面的配置示例）
3. **定期更新**：保持系统和依赖库的更新

## 指令使用

### 可用指令列表

| 指令 | 说明 | 示例 |
|------|------|------|
| `/account` | 查看账户及持仓信息 | `/account` |
| `/price [币种]` | 查看币种当前价格 | `/price BTCUSDT` |
| `/sl [币种] [止损价]` | 设置止损 | `/sl BTCUSDT 42000` |
| `/tp [币种] [止盈价]` | 设置止盈 | `/tp BTCUSDT 45000` |
| `/close [币种] [方向]` | 平仓 | `/close BTCUSDT long` |
| `/help` | 显示帮助信息 | `/help` |

### 指令详细说明

#### 1. 查看账户信息

```
/account
```

**功能**：显示账户余额、可用资金、已用保证金、总盈亏和持仓列表

**返回信息**：
- 总权益
- 可用余额
- 已用保证金
- 总盈亏（金额和百分比）
- 持仓数量
- 每个持仓的详细信息（币种、方向、数量、开仓价、标记价、未实现盈亏）

#### 2. 查看价格

```
/price BTCUSDT
```

**功能**：查询指定币种的当前价格和市场信息

**参数**：
- `币种`：交易对符号（如 `BTCUSDT`、`ETHUSDT`）

**返回信息**：
- 当前价格
- 4小时涨跌
- 1小时涨跌
- 区间最高价
- 区间最低价

#### 3. 设置止损

```
/sl BTCUSDT 42000
```

**功能**：为指定币种的持仓设置止损价格

**参数**：
- `币种`：交易对符号
- `止损价`：止损触发价格（数字，无需单位）

**说明**：
- 需要先有该币种的持仓
- 系统会自动识别持仓方向（long/short）
- 止损价格必须符合交易所的价格精度要求

#### 4. 设置止盈

```
/tp BTCUSDT 45000
```

**功能**：为指定币种的持仓设置止盈价格

**参数**：
- `币种`：交易对符号
- `止盈价`：止盈触发价格（数字，无需单位）

**说明**：
- 需要先有该币种的持仓
- 系统会自动识别持仓方向（long/short）
- 止盈价格必须符合交易所的价格精度要求

#### 5. 平仓

```
/close BTCUSDT long
```

**功能**：平掉指定币种和方向的持仓

**参数**：
- `币种`：交易对符号
- `方向`：`long`（做多）或 `short`（做空）

**说明**：
- 会平掉该币种指定方向的所有持仓
- 请谨慎操作，确保输入正确

### 使用提示

1. **币种格式**：使用标准交易对格式，如 `BTCUSDT`、`ETHUSDT`
2. **价格格式**：直接使用数字，无需单位或符号
3. **方向格式**：使用小写 `long` 或 `short`
4. **错误处理**：如果指令格式错误，Bot 会返回错误提示

## 故障排查

### 常见问题

#### 1. Webhook 未初始化

**症状**：应用启动时提示 "TELEGRAM_WEBHOOK_URL is not set"

**解决方案**：
- 检查 `.env` 文件中是否设置了 `TELEGRAM_WEBHOOK_URL`
- 确保 URL 格式正确：`https://your-domain.com/api/telegram/webhook`
- 确保 URL 使用 HTTPS（不是 HTTP）

#### 2. Webhook URL 验证失败

**症状**：启动时提示 "invalid webhook URL"

**可能原因**：
- 使用了 HTTP 而不是 HTTPS
- 端口不是 443、80、88 或 8443
- 使用了 localhost 或内网地址
- URL 格式不正确

**解决方案**：
- 确保使用 HTTPS
- 使用正确的端口（推荐 443）
- 使用公网可访问的域名或 IP
- 检查 URL 路径是否正确

#### 3. 无法接收指令

**症状**：发送指令后没有响应

**检查步骤**：
1. 确认 Bot Token 和 Chat ID 配置正确
2. 检查 Webhook URL 是否可访问（使用浏览器或 curl 测试）
3. 查看应用日志，确认是否有错误信息
4. 确认 IP 白名单未阻止你的请求（如果从非 Telegram IP 测试）

#### 4. SSL 证书错误

**症状**：Webhook 设置失败，提示证书相关错误

**解决方案**：
- 使用 Let's Encrypt 获取免费证书（推荐）
- 如果使用 IP 地址，确保证书的 CN 设置为该 IP
- 检查证书是否过期
- 确认证书链完整

#### 5. 速率限制触发

**症状**：返回 "Rate limit exceeded"

**说明**：
- 系统限制每个 IP 每分钟最多 30 个请求
- 正常使用不会触发此限制
- 如果频繁触发，可能是受到攻击

**解决方案**：
- 等待 1 分钟后重试
- 检查是否有异常请求
- 如需调整限制，修改代码中的速率限制参数

### 日志检查

查看应用日志以获取详细错误信息：

```bash
# 查看应用日志
tail -f /path/to/nofx.log

# 或查看 Docker 日志
docker logs -f nofx-trading
```

### 测试 Webhook

使用 curl 测试 Webhook 端点：

```bash
# 测试端点是否可访问
curl -X POST https://your-domain.com/api/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{"message":{"chat":{"id":123456789},"text":"/help"}}'
```

**注意**：此测试会触发 IP 白名单检查，如果从非 Telegram IP 访问，会返回 403。

## 高级配置

### 调整速率限制

如果需要调整速率限制，修改 `api/server.go` 中的配置：

```go
// 在 telegramWebhookRateLimit 函数中
limiter := newRateLimiter(30, 1*time.Minute)
//                    ↑    ↑
//                    限制数量  时间窗口
```

### 禁用 IP 白名单（不推荐）

如果需要临时禁用 IP 白名单（仅用于测试），可以注释掉中间件：

```go
// 在 setupRoutes 中
api.POST("/telegram/webhook", 
    // s.telegramWebhookIPWhitelist(),  // 临时禁用
    s.telegramWebhookRateLimit(), 
    s.handleTelegramWebhook)
```

**警告**：禁用 IP 白名单会降低安全性，仅用于测试环境。

### 多用户支持

当前实现使用单个 Chat ID。如果需要支持多用户：

1. 修改配置为支持多个 Chat ID
2. 在 `HandleUpdate` 中检查多个 Chat ID
3. 根据 Chat ID 路由到不同的交易员实例

### 自定义指令

要添加自定义指令：

1. 在 `notification/telegram_commands.go` 的 `CreateCommandHandlers` 中添加新指令
2. 实现对应的处理函数
3. 注册指令处理器

## 相关文档

- [Telegram Bot API 官方文档](https://core.telegram.org/bots/api)
- [Telegram Webhook 文档](https://core.telegram.org/bots/webhooks)
- [Telegram IP 段说明](https://core.telegram.org/bots/webhooks#validating-webhook-requests)

## 支持

如遇到问题，请：

1. 查看本文档的故障排查部分
2. 检查应用日志
3. 查看 GitHub Issues
4. 联系技术支持

---

**最后更新**：2024年

