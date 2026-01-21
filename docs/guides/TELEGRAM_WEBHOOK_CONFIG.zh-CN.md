# Telegram Bot Webhook 配置指南

本文档详细说明如何配置和使用 Telegram Bot Webhook 功能，包括通知和指令控制。

## 目录

- [功能概述](#功能概述)
- [前置准备](#前置准备)
- [基础配置](#基础配置)
- [域名部署](#域名部署)
  - [开发环境域名访问](#开发环境域名访问)
  - [生产环境域名部署](#生产环境域名部署)
- [Webhook 配置](#webhook-配置)
- [安全设置](#安全设置)
- [指令使用](#指令使用)
- [故障排查](#故障排查)
- [高级配置](#高级配置)

## 功能概述

Telegram Bot Webhook 提供以下功能：

1. **交易通知**：自动推送开仓、平仓、止盈止损等交易信息
2. **账户查询**：查看账户余额和持仓信息（无需验证）
3. **价格查询**：查询指定币种的当前价格（无需验证）
4. **止盈止损设置**：通过指令设置止盈止损（需要用户ID和2FA验证码）
5. **平仓操作**：通过指令快速平仓（需要用户ID和2FA验证码）

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

### 3. 获取用户ID

用户ID可以从 Web 界面获取：

1. 登录 Web 界面
2. 在用户信息或账户设置中查看用户ID
3. 用户ID格式通常为：`user_abc123` 或类似格式

**注意**：用户ID是系统内部标识，用于区分不同用户账户。

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

## 域名部署

### Webhook URL 要求

Telegram Webhook 必须满足以下要求：

1. **必须使用 HTTPS**（不支持 HTTP）
2. **端口限制**：只支持 `443`、`80`、`88`、`8443`
3. **公网可访问**：不能使用 `localhost` 或内网地址
4. **SSL 证书**：必须配置有效的 SSL 证书

### 开发环境域名访问

本地开发环境通常运行在 `localhost:8080`，无法直接满足 Telegram Webhook 的要求。需要使用隧道工具将本地服务暴露为 HTTPS 公网地址。

#### 方法一：使用 ngrok（推荐）

ngrok 是最流行的内网穿透工具，功能完善，易于使用。

**1. 安装 ngrok**

```bash
# macOS
brew install ngrok/ngrok/ngrok

# Linux
wget https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-amd64.tgz
tar -xzf ngrok-v3-stable-linux-amd64.tgz
sudo mv ngrok /usr/local/bin/
```

**2. 注册账号（可选但推荐）**

```bash
# 1. 访问 https://dashboard.ngrok.com/signup 注册账号
# 2. 获取 authtoken
# 3. 配置 token
ngrok config add-authtoken YOUR_AUTH_TOKEN
```

**3. 启动本地服务**

```bash
# 确保应用运行在 8080 端口
go run main.go
```

**4. 启动 ngrok 隧道**

```bash
# 暴露 8080 端口
ngrok http 8080
```

**输出示例：**
```
Forwarding    https://abc123.ngrok.io -> http://localhost:8080
```

**5. 配置环境变量**

```bash
TELEGRAM_WEBHOOK_URL=https://abc123.ngrok.io/api/telegram/webhook
```

**6. 使用固定域名（付费版）**

```bash
# 使用固定域名（需要付费账号）
ngrok http 8080 --domain=your-fixed-domain.ngrok.io
```

**优点**：
- ✅ 功能完善，稳定可靠
- ✅ 提供 Web 界面查看请求（http://localhost:4040）
- ✅ 支持固定域名（付费版）

**缺点**：
- ❌ 免费版 URL 每次重启会变化
- ❌ 固定域名需要付费

#### 方法二：使用 localtunnel（免费，无需注册）

localtunnel 是一个完全免费的工具，无需注册即可使用。

**1. 安装 localtunnel**

```bash
npm install -g localtunnel
```

**2. 启动隧道**

```bash
# 暴露 8080 端口
lt --port 8080
```

**输出示例：**
```
your url is: https://random-name.loca.lt
```

**3. 使用自定义子域名（可选）**

```bash
# 使用自定义子域名（如果可用）
lt --port 8080 --subdomain my-nofx-dev
```

**4. 配置环境变量**

```bash
TELEGRAM_WEBHOOK_URL=https://random-name.loca.lt/api/telegram/webhook
```

**注意**：localtunnel 会显示一个警告页面，需要点击 "Continue" 按钮才能继续使用。

**优点**：
- ✅ 完全免费，无需注册
- ✅ 安装简单（npm 安装）
- ✅ 支持自定义子域名

**缺点**：
- ❌ 每次启动 URL 可能变化
- ❌ 需要手动点击确认页面

#### 方法三：使用 Cloudflare Tunnel（免费，固定域名）

Cloudflare Tunnel 提供免费的固定域名，适合长期开发使用。

**1. 安装 cloudflared**

```bash
# macOS
brew install cloudflared

# Linux
wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
chmod +x cloudflared-linux-amd64
sudo mv cloudflared-linux-amd64 /usr/local/bin/cloudflared
```

**2. 快速启动（临时 URL）**

```bash
# 快速启动，获得临时 URL
cloudflared tunnel --url http://localhost:8080
```

**输出示例：**
```
Your quick Tunnel has been created! Visit it at:
https://random-name.trycloudflare.com
```

**3. 使用固定域名（推荐）**

```bash
# 步骤 1：登录 Cloudflare
cloudflared tunnel login

# 步骤 2：创建隧道
cloudflared tunnel create nofx-dev

# 步骤 3：配置路由（需要域名 DNS 托管在 Cloudflare）
cloudflared tunnel route dns nofx-dev dev.yourdomain.com

# 步骤 4：运行隧道
cloudflared tunnel run nofx-dev
```

**4. 配置环境变量**

```bash
TELEGRAM_WEBHOOK_URL=https://dev.yourdomain.com/api/telegram/webhook
```

**优点**：
- ✅ 完全免费
- ✅ 支持固定域名
- ✅ 无需公网 IP
- ✅ 自动 HTTPS

**缺点**：
- ❌ 配置相对复杂
- ❌ 固定域名需要 Cloudflare 账号

### 生产环境域名部署

生产环境建议使用自己的域名和 SSL 证书，配置 Nginx 反向代理。

#### 使用域名（推荐）

```bash
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

**优点**：
- 可以使用 Let's Encrypt 免费证书
- 配置简单，维护方便
- 符合最佳实践

#### 使用 IP 地址（不推荐）

```bash
TELEGRAM_WEBHOOK_URL=https://123.45.67.89:443/api/telegram/webhook
```

**要求**：
- SSL 证书的 CN（Common Name）必须设置为该 IP 地址
- 需要使用自签名证书
- 配置相对复杂

**不推荐**：除非无法使用域名，否则建议使用域名方式。

#### Nginx 反向代理配置

由于 Telegram 只支持特定端口（443、80、88、8443），而 API 服务器通常运行在其他端口（如 8080），需要通过 Nginx 反向代理。

**完整配置示例：**

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
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
    
    # HTTP 到 HTTPS 重定向
    if ($scheme != "https") {
        return 301 https://$host$request_uri;
    }
    
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

# HTTP 服务器（重定向到 HTTPS）
server {
    listen 80;
    server_name your-domain.com;
    return 301 https://$host$request_uri;
}
```

#### 使用 Let's Encrypt 获取免费 SSL 证书

**1. 安装 certbot**

```bash
# Ubuntu/Debian
sudo apt-get update
sudo apt-get install certbot python3-certbot-nginx

# CentOS/RHEL
sudo yum install certbot python3-certbot-nginx
```

**2. 获取证书**

```bash
# 自动配置 Nginx
sudo certbot --nginx -d your-domain.com

# 或手动获取证书
sudo certbot certonly --nginx -d your-domain.com
```

**3. 自动续期**

Let's Encrypt 证书有效期为 90 天，certbot 会自动配置续期任务：

```bash
# 测试续期（不实际续期）
sudo certbot renew --dry-run

# 查看续期任务
sudo systemctl status certbot.timer
```

#### Docker Compose 部署

如果使用 Docker Compose 部署，Nginx 配置示例：

```yaml
version: '3.8'

services:
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ./nginx/conf.d:/etc/nginx/conf.d:ro
      - /etc/letsencrypt:/etc/letsencrypt:ro
      - ./frontend/dist:/usr/share/nginx/html:ro
    depends_on:
      - api
    restart: unless-stopped

  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      - TELEGRAM_ENABLED=true
      - TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
      - TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID}
      - TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
    restart: unless-stopped
```

## Webhook 配置

### 配置环境变量

在 `.env` 文件中配置 Webhook URL：

```bash
# Telegram 基础配置
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# Webhook URL（根据部署方式选择）
# 开发环境（ngrok）
TELEGRAM_WEBHOOK_URL=https://abc123.ngrok.io/api/telegram/webhook

# 开发环境（localtunnel）
# TELEGRAM_WEBHOOK_URL=https://random-name.loca.lt/api/telegram/webhook

# 开发环境（Cloudflare Tunnel）
# TELEGRAM_WEBHOOK_URL=https://dev.yourdomain.com/api/telegram/webhook

# 生产环境（自己的域名）
# TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/telegram/webhook
```

### 验证配置

启动应用后，查看日志确认 Webhook 已正确配置：

```
✓ Telegram webhook bot initialized: @your_bot_name
✓ Telegram webhook started: https://your-domain.com/api/telegram/webhook
✓ Telegram Webhook initialized and welcome message sent
```

### 测试 Webhook

使用 curl 测试 Webhook 端点：

```bash
curl -X POST https://your-domain.com/api/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{"message":{"chat":{"id":123456789},"text":"/help"}}'
```

**注意**：此测试会触发 IP 白名单检查，如果从非 Telegram IP 访问，会返回 403（这是正常的）。

通过 Telegram Bot API 检查 Webhook 状态：

```bash
curl https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo
```

## 安全设置

### 内置安全功能

系统已实现以下安全措施：

1. **IP 白名单**：只允许来自 Telegram 官方 IP 段的请求
   - `149.154.160.0/20`（Telegram 主要 IP 段）
   - `91.108.4.0/22`（Telegram 备用 IP 段）

2. **速率限制**：每个 IP 每分钟最多 30 个请求

3. **2FA 验证**：所有操作类指令都需要提供用户ID和 Google Authenticator 验证码

### 安全层级

```
请求 → IP 白名单检查 → 速率限制检查 → 2FA 验证 → 处理请求
```

### 额外安全建议

1. **防火墙配置**：在服务器防火墙中限制只允许 Telegram IP 段访问 443 端口
2. **Nginx IP 限制**：在 Nginx 配置中添加 IP 白名单（见上面的配置示例）
3. **定期更新**：保持系统和依赖库的更新
4. **保护用户ID**：不要公开分享你的用户ID

## 指令使用

### 可用指令列表

| 指令 | 说明 | 是否需要验证 | 示例 |
|------|------|--------------|------|
| `/account` | 查看账户及持仓信息 | 否 | `/account` |
| `/price [币种]` | 查看币种当前价格 | 否 | `/price BTCUSDT` |
| `/sl [用户ID] [币种] [止损价] [OTP码]` | 设置止损 | 是 | `/sl user_abc123 BTCUSDT 42000 123456` |
| `/tp [用户ID] [币种] [止盈价] [OTP码]` | 设置止盈 | 是 | `/tp user_abc123 BTCUSDT 45000 123456` |
| `/close [用户ID] [币种] [方向] [OTP码]` | 平仓 | 是 | `/close user_abc123 BTCUSDT long 123456` |
| `/help` | 显示帮助信息 | 否 | `/help` |

### 指令详细说明

#### 1. 查看账户信息（无需验证）

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

**注意**：此指令无需提供用户ID或验证码，任何人都可以查询。

#### 2. 查看价格（无需验证）

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

**注意**：此指令无需提供用户ID或验证码。

#### 3. 设置止损（需要用户ID和OTP）

```
/sl user_abc123 BTCUSDT 42000 123456
```

**功能**：为指定币种的持仓设置止损价格

**参数**：
- `用户ID`：你的用户ID（从 Web 界面获取）
- `币种`：交易对符号
- `止损价`：止损触发价格（数字，无需单位）
- `OTP码`：Google Authenticator 验证码（6位数字）

**说明**：
- 需要先有该币种的持仓
- 系统会自动识别持仓方向（long/short）
- 止损价格必须符合交易所的价格精度要求
- OTP 码来自你的 Google Authenticator 应用

**安全**：此指令需要验证你的身份，确保只有账户所有者可以操作。

#### 4. 设置止盈（需要用户ID和OTP）

```
/tp user_abc123 BTCUSDT 45000 123456
```

**功能**：为指定币种的持仓设置止盈价格

**参数**：
- `用户ID`：你的用户ID（从 Web 界面获取）
- `币种`：交易对符号
- `止盈价`：止盈触发价格（数字，无需单位）
- `OTP码`：Google Authenticator 验证码（6位数字）

**说明**：
- 需要先有该币种的持仓
- 系统会自动识别持仓方向（long/short）
- 止盈价格必须符合交易所的价格精度要求
- OTP 码来自你的 Google Authenticator 应用

**安全**：此指令需要验证你的身份，确保只有账户所有者可以操作。

#### 5. 平仓（需要用户ID和OTP）

```
/close user_abc123 BTCUSDT long 123456
```

**功能**：平掉指定币种和方向的持仓

**参数**：
- `用户ID`：你的用户ID（从 Web 界面获取）
- `币种`：交易对符号
- `方向`：`long`（做多）或 `short`（做空）
- `OTP码`：Google Authenticator 验证码（6位数字）

**说明**：
- 会平掉该币种指定方向的所有持仓
- 请谨慎操作，确保输入正确
- OTP 码来自你的 Google Authenticator 应用

**安全**：此指令需要验证你的身份，确保只有账户所有者可以操作。

### 使用提示

1. **币种格式**：使用标准交易对格式，如 `BTCUSDT`、`ETHUSDT`
2. **价格格式**：直接使用数字，无需单位或符号
3. **方向格式**：使用小写 `long` 或 `short`
4. **用户ID格式**：从 Web 界面获取，通常为 `user_` 开头的字符串
5. **OTP 码**：6位数字，来自 Google Authenticator 等 2FA 应用
6. **错误处理**：如果指令格式错误，Bot 会返回错误提示

### 多用户支持

当前实现支持多用户使用同一个 Bot：

- **查询指令**（`/account`、`/price`）：所有用户都可以使用，无需验证
- **操作指令**（`/sl`、`/tp`、`/close`）：每个用户需要提供自己的用户ID和OTP码
- **无需绑定**：不需要预先绑定 Chat ID，每次操作时提供用户ID即可

**优势**：
- 多个用户可以共享同一个 Bot
- 无需预先配置，使用更灵活
- 安全性更高（每次操作都需要验证）

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

#### 4. OTP 验证失败

**症状**：操作指令返回 "OTP 验证码错误"

**可能原因**：
- OTP 码已过期（通常 30 秒有效）
- 用户ID错误
- 账户未启用 2FA

**解决方案**：
- 确保使用最新的 OTP 码（Google Authenticator 会自动更新）
- 检查用户ID是否正确
- 确认账户已在 Web 界面完成 2FA 设置

#### 5. 用户不存在错误

**症状**：操作指令返回 "用户不存在"

**解决方案**：
- 检查用户ID是否正确（从 Web 界面获取）
- 确认用户ID格式正确（通常为 `user_` 开头）
- 如果用户ID包含空格，确保在指令中正确引用

#### 6. SSL 证书错误

**症状**：Webhook 设置失败，提示证书相关错误

**解决方案**：
- 使用 Let's Encrypt 获取免费证书（推荐）
- 如果使用 IP 地址，确保证书的 CN 设置为该 IP
- 检查证书是否过期
- 确认证书链完整

#### 7. 速率限制触发

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

### 自定义指令

要添加自定义指令：

1. 在 `notification/telegram_commands.go` 的 `CreateCommandHandlers` 中添加新指令
2. 实现对应的处理函数
3. 注册指令处理器

### 2FA 设置

如果账户未启用 2FA，需要先在 Web 界面完成设置：

1. 登录 Web 界面
2. 进入账户设置
3. 启用 2FA（Google Authenticator）
4. 保存密钥和验证码
5. 完成验证后即可使用操作指令

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
