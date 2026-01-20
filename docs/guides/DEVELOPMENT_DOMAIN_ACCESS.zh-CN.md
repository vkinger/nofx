# 开发环境域名访问配置指南

本文档介绍如何在开发环境中配置域名访问，以便测试 Telegram Webhook 等功能。由于 Telegram Webhook 要求使用 HTTPS 和公网可访问的地址，本地开发环境需要通过隧道工具将本地服务暴露到公网。

## 目录

- [概述](#概述)
- [方法一：使用 ngrok（推荐）](#方法一使用-ngrok推荐)
- [方法二：使用 localtunnel（免费，无需注册）](#方法二使用-localtunnel免费无需注册)
- [方法三：使用 Cloudflare Tunnel（免费，固定域名）](#方法三使用-cloudflare-tunnel免费固定域名)
- [Webhook 配置](#webhook-配置)
- [测试验证](#测试验证)
- [常见问题](#常见问题)
- [快速参考](#快速参考)

## 概述

### 为什么需要域名访问？

Telegram Webhook 有以下要求：
- ✅ 必须使用 HTTPS（不支持 HTTP）
- ✅ 必须是公网可访问的地址（不能是 localhost）
- ✅ 端口必须是 443、80、88 或 8443 之一

本地开发环境通常运行在 `localhost:8080`，无法直接满足这些要求。因此需要使用隧道工具将本地服务暴露为 HTTPS 公网地址。

### 工具对比

| 工具 | 免费版 | 固定域名 | 需要注册 | 推荐场景 |
|------|--------|----------|----------|----------|
| **ngrok** | ✅ | ❌（付费版支持） | 可选 | 快速测试，功能完整 |
| **localtunnel** | ✅ | ❌ | ❌ | 简单测试，无需注册 |
| **Cloudflare Tunnel** | ✅ | ✅ | ✅ | 长期开发，需要固定域名 |

## 方法一：使用 ngrok（推荐）

ngrok 是最流行的内网穿透工具，功能完善，易于使用。

### 1. 安装 ngrok

#### macOS
```bash
brew install ngrok/ngrok/ngrok
```

#### Linux
```bash
# 下载并安装
wget https://bin.equinox.io/c/bNyj1mQVY4c/ngrok-v3-stable-linux-amd64.tgz
tar -xzf ngrok-v3-stable-linux-amd64.tgz
sudo mv ngrok /usr/local/bin/

# 验证安装
ngrok version
```

#### Windows
- 从 [ngrok.com/download](https://ngrok.com/download) 下载
- 或使用 Chocolatey: `choco install ngrok`

### 2. 注册账号（可选但推荐）

虽然可以不注册使用，但注册后可以获得：
- 更长的会话时间
- 更多功能
- 固定域名（付费版）

```bash
# 1. 访问 https://dashboard.ngrok.com/signup 注册账号
# 2. 获取 authtoken
# 3. 配置 token
ngrok config add-authtoken YOUR_AUTH_TOKEN
```

### 3. 启动本地服务

确保你的应用运行在正确的端口（默认 8080）：

```bash
# 启动应用
go run main.go
# 或
./nofx
```

### 4. 启动 ngrok 隧道

```bash
# 暴露 8080 端口
ngrok http 8080
```

**输出示例：**
```
Session Status                online
Account                       your-email@example.com
Version                       3.x.x
Region                        United States (us)
Latency                       45ms
Web Interface                 http://127.0.0.1:4040
Forwarding                    https://abc123.ngrok.io -> http://localhost:8080

Connections                   ttl     opn     rt1     rt5     p50     p90
                              0       0       0.00    0.00    0.00    0.00
```

### 5. 获取 HTTPS URL

从输出中获取 `Forwarding` 行的 HTTPS URL，例如：
```
https://abc123.ngrok.io
```

### 6. 使用固定域名（付费版）

如果需要固定域名（避免每次重启 URL 变化）：

```bash
# 使用固定域名（需要付费账号）
ngrok http 8080 --domain=your-fixed-domain.ngrok.io
```

### 7. 查看请求日志

访问 `http://localhost:4040` 可以查看：
- 所有请求详情
- 请求/响应内容
- 实时流量监控

### 优点
- ✅ 功能完善，稳定可靠
- ✅ 提供 Web 界面查看请求
- ✅ 支持固定域名（付费版）
- ✅ 文档完善，社区活跃

### 缺点
- ❌ 免费版 URL 每次重启会变化
- ❌ 固定域名需要付费

## 方法二：使用 localtunnel（免费，无需注册）

localtunnel 是一个完全免费的工具，无需注册即可使用。

### 1. 安装 localtunnel

```bash
npm install -g localtunnel
```

**注意：** 需要先安装 Node.js

### 2. 启动本地服务

```bash
# 确保应用运行在 8080 端口
go run main.go
```

### 3. 启动隧道

```bash
# 暴露 8080 端口
lt --port 8080
```

**输出示例：**
```
your url is: https://random-name.loca.lt
```

### 4. 使用自定义子域名（可选）

```bash
# 使用自定义子域名（如果可用）
lt --port 8080 --subdomain my-nofx-dev
```

**输出：**
```
your url is: https://my-nofx-dev.loca.lt
```

### 5. 保持隧道运行

localtunnel 会显示一个警告页面，需要点击 "Continue" 按钮才能继续使用。

### 优点
- ✅ 完全免费，无需注册
- ✅ 安装简单（npm 安装）
- ✅ 支持自定义子域名

### 缺点
- ❌ 每次启动 URL 可能变化
- ❌ 需要手动点击确认页面
- ❌ 功能相对简单

## 方法三：使用 Cloudflare Tunnel（免费，固定域名）

Cloudflare Tunnel（原 Argo Tunnel）提供免费的固定域名，适合长期开发使用。

### 1. 安装 cloudflared

#### macOS
```bash
brew install cloudflared
```

#### Linux
```bash
# 下载最新版本
wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
chmod +x cloudflared-linux-amd64
sudo mv cloudflared-linux-amd64 /usr/local/bin/cloudflared
```

#### Windows
- 从 [GitHub Releases](https://github.com/cloudflare/cloudflared/releases) 下载
- 或使用 Chocolatey: `choco install cloudflared`

### 2. 快速启动（临时 URL）

```bash
# 快速启动，获得临时 URL
cloudflared tunnel --url http://localhost:8080
```

**输出示例：**
```
+--------------------------------------------------------------------------------------------+
|  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable): |
|  https://random-name.trycloudflare.com                                                     |
+--------------------------------------------------------------------------------------------+
```

### 3. 使用固定域名（推荐）

#### 步骤 1：登录 Cloudflare

```bash
cloudflared tunnel login
```

这会打开浏览器，选择你的域名并授权。

#### 步骤 2：创建隧道

```bash
# 创建命名隧道
cloudflared tunnel create nofx-dev
```

#### 步骤 3：配置路由

```bash
# 将子域名路由到隧道
cloudflared tunnel route dns nofx-dev dev.yourdomain.com
```

**注意：** 需要将域名 DNS 托管在 Cloudflare。

#### 步骤 4：创建配置文件

创建 `~/.cloudflared/config.yml`：

```yaml
tunnel: <TUNNEL_ID>
credentials-file: ~/.cloudflared/<TUNNEL_ID>.json

ingress:
  - hostname: dev.yourdomain.com
    service: http://localhost:8080
  - service: http_status:404
```

#### 步骤 5：运行隧道

```bash
cloudflared tunnel run nofx-dev
```

### 4. 使用 Cloudflare 免费域名

如果你没有自己的域名，可以使用 Cloudflare 提供的免费域名：

```bash
# 使用 Cloudflare 提供的免费域名
cloudflared tunnel --url http://localhost:8080 --hostname your-app.trycloudflare.com
```

### 优点
- ✅ 完全免费
- ✅ 支持固定域名
- ✅ 无需公网 IP
- ✅ 自动 HTTPS

### 缺点
- ❌ 配置相对复杂
- ❌ 固定域名需要 Cloudflare 账号
- ❌ 需要域名 DNS 托管在 Cloudflare

## Webhook 配置

### 1. 获取 HTTPS URL

根据你选择的方法，获取 HTTPS URL：

- **ngrok**: `https://abc123.ngrok.io`
- **localtunnel**: `https://random-name.loca.lt`
- **Cloudflare Tunnel**: `https://dev.yourdomain.com` 或 `https://random-name.trycloudflare.com`

### 2. 配置环境变量

在 `.env` 文件中配置：

```bash
# Telegram 基础配置
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_CHAT_ID=your_chat_id_here

# Webhook URL（使用隧道工具提供的 HTTPS URL）
TELEGRAM_WEBHOOK_URL=https://abc123.ngrok.io/api/telegram/webhook
# 或
TELEGRAM_WEBHOOK_URL=https://random-name.loca.lt/api/telegram/webhook
# 或
TELEGRAM_WEBHOOK_URL=https://dev.yourdomain.com/api/telegram/webhook
```

### 3. 重启应用

```bash
# 停止当前应用（Ctrl+C）
# 重新启动
go run main.go
```

### 4. 验证配置

查看应用日志，应该看到：

```
✓ Telegram webhook bot initialized: @your_bot_name
✓ Telegram webhook started: https://abc123.ngrok.io/api/telegram/webhook
✓ Telegram Webhook initialized and welcome message sent
```

## 测试验证

### 1. 测试 Webhook 端点

使用 curl 测试 Webhook 是否可访问：

```bash
curl -X POST https://abc123.ngrok.io/api/telegram/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "message": {
      "chat": {"id": YOUR_CHAT_ID},
      "text": "/help"
    }
  }'
```

**注意：** 此测试会触发 IP 白名单检查，如果从非 Telegram IP 访问，会返回 403（这是正常的）。

### 2. 检查 Webhook 状态

通过 Telegram Bot API 检查 Webhook 信息：

```bash
curl https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo
```

**正常响应示例：**
```json
{
  "ok": true,
  "result": {
    "url": "https://abc123.ngrok.io/api/telegram/webhook",
    "has_custom_certificate": false,
    "pending_update_count": 0
  }
}
```

### 3. 在 Telegram 中测试

1. 打开 Telegram，找到你的 Bot
2. 发送 `/help` 指令
3. 应该收到欢迎消息和使用说明

### 4. 测试其他指令

- `/account` - 查看账户信息
- `/price BTCUSDT` - 查看价格
- `/help` - 显示帮助

## 常见问题

### 问题 1：URL 每次重启都变化

**原因：** 免费版工具每次启动会生成新的随机 URL

**解决方案：**
- 使用 ngrok 付费版获得固定域名
- 使用 Cloudflare Tunnel 配置固定域名
- 每次更新 `.env` 文件并重启应用

### 问题 2：Webhook 初始化失败

**错误信息：** `failed to set webhook`

**可能原因：**
- 隧道工具未运行
- URL 格式错误
- 网络连接问题

**解决方案：**
```bash
# 检查隧道是否运行
# ngrok: 访问 http://localhost:4040
# localtunnel: 检查终端输出
# cloudflared: 检查终端输出

# 测试 URL 是否可访问
curl https://your-tunnel-url/api/telegram/webhook
```

### 问题 3：收到 403 Forbidden

**原因：** IP 白名单阻止了请求

**解决方案：**
- 确保使用隧道工具（请求会从 Telegram IP 转发）
- 或临时禁用 IP 白名单（仅开发环境）

### 问题 4：隧道连接断开

**原因：** 网络不稳定或隧道工具超时

**解决方案：**
- 检查网络连接
- 重新启动隧道工具
- 使用更稳定的工具（如 ngrok 付费版）

### 问题 5：localtunnel 需要点击确认

**原因：** localtunnel 的安全机制

**解决方案：**
- 在浏览器中访问提供的 URL
- 点击 "Continue" 按钮
- 之后就可以正常使用

## 快速参考

### ngrok 常用命令

```bash
# 启动 HTTP 隧道
ngrok http 8080

# 使用固定域名（付费版）
ngrok http 8080 --domain=your-domain.ngrok.io

# 查看 Web 界面
open http://localhost:4040

# 查看配置
ngrok config check
```

### localtunnel 常用命令

```bash
# 启动隧道
lt --port 8080

# 使用自定义子域名
lt --port 8080 --subdomain my-app

# 指定区域
lt --port 8080 --region eu
```

### Cloudflare Tunnel 常用命令

```bash
# 快速启动（临时 URL）
cloudflared tunnel --url http://localhost:8080

# 登录
cloudflared tunnel login

# 创建隧道
cloudflared tunnel create my-tunnel

# 运行隧道
cloudflared tunnel run my-tunnel

# 列出隧道
cloudflared tunnel list
```

### 环境变量配置模板

```bash
# .env 文件
TELEGRAM_ENABLED=true
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_CHAT_ID=123456789

# ngrok
TELEGRAM_WEBHOOK_URL=https://abc123.ngrok.io/api/telegram/webhook

# localtunnel
# TELEGRAM_WEBHOOK_URL=https://random-name.loca.lt/api/telegram/webhook

# Cloudflare Tunnel
# TELEGRAM_WEBHOOK_URL=https://dev.yourdomain.com/api/telegram/webhook
```

## 自动化脚本

### ngrok 自动配置脚本

创建 `scripts/start-ngrok.sh`：

```bash
#!/bin/bash

# 启动 ngrok
echo "🚀 Starting ngrok..."
ngrok http 8080 > /dev/null &
NGROK_PID=$!

# 等待 ngrok 启动
sleep 3

# 获取 ngrok URL
NGROK_URL=$(curl -s http://localhost:4040/api/tunnels | grep -o 'https://[^"]*\.ngrok\.io' | head -1)

if [ -z "$NGROK_URL" ]; then
    echo "❌ Failed to get ngrok URL"
    kill $NGROK_PID 2>/dev/null
    exit 1
fi

echo "✅ Ngrok URL: $NGROK_URL"
echo "📝 Update your .env file:"
echo "   TELEGRAM_WEBHOOK_URL=$NGROK_URL/api/telegram/webhook"
echo ""
echo "Press Ctrl+C to stop ngrok"

# 等待中断信号
trap "kill $NGROK_PID 2>/dev/null; exit" INT TERM
wait $NGROK_PID
```

使用方法：
```bash
chmod +x scripts/start-ngrok.sh
./scripts/start-ngrok.sh
```

## 推荐方案

### 快速测试
使用 **ngrok** 或 **localtunnel**，快速启动，无需复杂配置。

### 长期开发
使用 **Cloudflare Tunnel**，配置固定域名，避免每次更新配置。

### 生产环境
使用自己的域名和 SSL 证书，配置 Nginx 反向代理。

## 相关文档

- [Telegram Webhook 配置指南](TELEGRAM_WEBHOOK_CONFIG.zh-CN.md)
- [Telegram 通知配置](TELEGRAM_NOTIFICATION.zh-CN.md)
- [Nginx 配置说明](../nginx/nginx.conf)

---

**最后更新**：2024年

