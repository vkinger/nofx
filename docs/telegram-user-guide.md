# Telegram Bot 用户使用指南

## 一、用户使用流程概览

### 完整流程图

```
首次使用
  ↓
1. 在 Telegram 中搜索系统 bot
  ↓
2. 点击 "Start" 或发送 /start
  ↓
3. 收到欢迎消息和操作指南
  ↓
4. 在 Web 界面完成注册和 2FA 设置（如果尚未完成）
  ↓
5. 发送 /login 命令进行登录
  ↓
6. 系统自动配置 Telegram ChatID
  ↓
7. 开始接收自动推送通知
  ↓
8. 使用各种命令管理交易
```

## 二、详细使用流程

### 阶段 1：首次使用（新用户）

#### 步骤 1：找到系统 Bot

1. **打开 Telegram 应用**
2. **搜索系统 Bot**：
   - 在 Telegram 搜索框中输入系统 bot 的用户名（例如：`@nofx_bot`）
   - 或通过管理员提供的 bot 链接直接打开

#### 步骤 2：启动 Bot

1. **点击 "Start" 按钮**：
   - Telegram 会自动发送 `/start` 命令
   - 或手动发送 `/start` 命令

2. **收到欢迎消息**：
   ```
   🤖 欢迎使用 NOFX 交易机器人

   📋 快速开始：

   1️⃣ 登录（首次使用需要）：
      /login [邮箱] [OTP码]
      示例: /login user@example.com 123456

   2️⃣ 查看账户：
      /account [邮箱] [OTP码]
      或登录后直接: /account

   3️⃣ 查看价格（无需登录）：
      /price BTCUSDT

   📢 自动推送功能：
   登录成功后，系统会自动推送以下信息：
   - 📈 交易决策通知（开仓/平仓）
   - 📊 账户摘要和持仓详情
   - 🛡️ 风控系统通知（止损/回撤）

   💡 提示：
   - 首次使用请先执行 /login 命令进行登录
   - 登录成功后，您的 Telegram ChatID 会自动配置
   - 之后您将自动接收交易通知，无需手动查询
   - 发送 /help 查看完整帮助信息
   ```

#### 步骤 3：完成 Web 界面注册和 2FA 设置

**如果尚未注册**：

1. **访问 Web 界面**：
   - 打开系统提供的 Web 界面地址
   - 例如：`https://your-domain.com`

2. **注册账户**：
   - 填写邮箱和密码
   - 完成注册流程

3. **设置 2FA（必需）**：
   - 在 Web 界面中启用 2FA
   - 使用 Google Authenticator 扫描二维码
   - 验证并完成 2FA 设置

**如果已注册但未设置 2FA**：
- 登录 Web 界面
- 在设置中启用 2FA
- 使用 Google Authenticator 扫描二维码

**重要提示**：
- ✅ 2FA 设置是**必需的**，没有 2FA 无法使用 Telegram 命令
- ✅ 使用 Google Authenticator 或其他 TOTP 应用
- ✅ 保存好 OTP Secret，以防丢失设备

#### 步骤 4：首次登录（重要：这是保存 ChatID 的关键步骤）

**重要区别**：
- **点开对话框**（发送 `/start`）：只是收到欢迎消息，**不会保存 ChatID**
- **执行登录命令**（发送 `/login`）：这才是真正的"登录"，**会保存 ChatID**

1. **打开 Google Authenticator**：
   - 找到对应的账户（NOFX）
   - 获取当前的 6 位数字验证码
   - 验证码每 30 秒变化一次

2. **发送登录命令**：
   ```
   /login user@example.com 123456
   ```
   - `user@example.com`：你的注册邮箱
   - `123456`：Google Authenticator 中的当前验证码

3. **系统处理**：
   - 验证邮箱是否存在
   - 验证用户是否完成 2FA 设置
   - 验证 OTP 是否正确
   - **自动保存你的 Telegram ChatID**（这是关键步骤）
   - 创建 Session（30秒免验证）

4. **收到登录成功消息**：
   ```
   ✅ 登录成功！

   账户: user@example.com
   Telegram ChatID: 987654321
   免验证时长: 30秒

   💡 30秒内使用其他指令无需输入邮箱和OTP。
   💡 您的 Telegram ChatID 已自动配置，后续将只向您推送通知。
   ```

**重要提示**：
- ✅ **只有执行 `/login` 命令后，ChatID 才会被保存**
- ✅ 仅点开对话框（发送 `/start`）**不会保存 ChatID**，无法接收推送
- ✅ 登录成功后，你的 Telegram ChatID 会自动保存到数据库
- ✅ 之后系统会自动向你推送交易通知（即使关闭对话框或 Session 过期）
- ✅ 30秒内使用其他命令无需重新输入 OTP

### 阶段 2：日常使用

#### 自动推送通知

**重要**：推送通知**不依赖 Session**，只要用户曾经登录过（ChatID 已保存），就能持续收到推送通知，即使 Session 过期也不影响。

登录成功后，系统会自动推送以下通知：

1. **交易决策通知**：
   ```
   📈 Default Trader - BTCUSDT open_long
   💰 价格: $43,250.50
   📊 数量: 0.00100000
   ⚡ 杠杆: 10x
   💵 仓位: $432.51
   🛑 止损: $41,087.98
   🎯 止盈: $45,413.02
   🎲 信心度: 85%
   ```

2. **账户摘要**：
   ```
   📊 Default Trader - 账户信息

   💼 总权益: $10,432.50
   💰 可用余额: $8,500.00
   📌 已用保证金: $1,932.50
   📈 总盈亏: $432.50 (4.15%)
   📋 持仓数量: 2
   ```

3. **持仓详情**：
   ```
   📋 Default Trader - 持仓信息

   1. 📈 BTCUSDT LONG
      💰 数量: 0.00100000 | 杠杆: 10x
      💵 开仓价: $43,250.50 | 标记价: $43,280.00 (📈0.07%)
      💎 持仓价值: $432.80
      🔒 已用保证金: $43.28
      📈 未实现盈亏: $29.50 (6.82%)
      ⚠️ 强平价: $38,925.45 (距离: 10.06%)
   ```

4. **风控系统通知**：
   ```
   🛡️ Default Trader - 风控平仓
   📌 策略: 回撤止损
   💰 币种: BTCUSDT long
   📥 开仓价: $43,250.50
   📤 平仓价: $42,100.00
   📊 数量: 0.00100000
   💵 保证金: $43.25
   📉 盈亏: -$115.00 (-2.66%)
   📊 峰值利润: 8.50%
   📉 回撤: 11.16%
   ```

5. **回撤监控警告**：
   ```
   ⚠️ Default Trader - 回撤监控警告
   💰 币种: BTCUSDT long
   📊 当前利润: 2.50%
   📈 峰值利润: 8.50%
   📉 回撤: 6.00%
   🛑 阈值: 5.00%
   ```

**重要提示**：
- ✅ 所有通知都是**自动推送**的，无需手动查询
- ✅ 通知只发送给你（基于你的 Telegram ChatID）
- ✅ 通知是实时的，交易执行后立即推送
- ✅ **推送不依赖 Session**：只要曾经登录过（ChatID 已保存），就能持续收到推送，即使 Session 过期也不影响
- ✅ **只有操作命令才需要 Session 或 OTP**：推送通知是单向的，不需要验证

#### 使用命令

**30秒内（Session 有效）**：

登录后 30 秒内，使用命令无需输入邮箱和 OTP：

```
/account          # 查看账户和持仓
/price BTCUSDT    # 查看价格（无需验证）
/sl BTCUSDT 42000 # 设置止损
/tp BTCUSDT 45000 # 设置止盈
/close BTCUSDT long # 平仓
/trades BTCUSDT 10 # 查看最近交易记录
/trader trader_id_123 # 查看交易员状态
/start-trader trader_id_123 # 启用交易员
/stop-trader trader_id_123 # 停用交易员
/help             # 查看完整帮助
```

**30秒后（Session 过期）**：

Session 过期后，有两种方式使用命令：

**方式 1：重新登录**
```
/login user@example.com 123456
```
- 登录后 30 秒内可以免验证使用其他命令

**方式 2：在命令末尾提供邮箱和 OTP**
```
/account user@example.com 123456
/sl BTCUSDT 42000 user@example.com 123456
/tp BTCUSDT 45000 user@example.com 123456
/close BTCUSDT long user@example.com 123456
```

**命令示例**：

1. **查看账户信息**：
   ```
   /account
   ```
   或（Session 过期后）：
   ```
   /account user@example.com 123456
   ```

2. **查看价格**（无需验证）：
   ```
   /price BTCUSDT
   /price ETHUSDT
   ```

3. **设置止损**：
   ```
   /sl BTCUSDT 42000
   ```
   或（Session 过期后）：
   ```
   /sl BTCUSDT 42000 user@example.com 123456
   ```

4. **设置止盈**：
   ```
   /tp BTCUSDT 45000
   ```
   或（Session 过期后）：
   ```
   /tp BTCUSDT 45000 user@example.com 123456
   ```

5. **平仓**：
   ```
   /close BTCUSDT long
   ```
   或（Session 过期后）：
   ```
   /close BTCUSDT long user@example.com 123456
   ```

6. **查看交易记录**：
   ```
   /trades BTCUSDT 10
   ```
   或（Session 过期后）：
   ```
   /trades BTCUSDT 10 user@example.com 123456
   ```

7. **查看交易员状态**：
   ```
   /trader trader_id_123
   ```
   或（Session 过期后）：
   ```
   /trader trader_id_123 user@example.com 123456
   ```

8. **启用/停用交易员**：
   ```
   /start-trader trader_id_123
   /stop-trader trader_id_123
   ```
   或（Session 过期后）：
   ```
   /start-trader trader_id_123 user@example.com 123456
   /stop-trader trader_id_123 user@example.com 123456
   ```

### 阶段 3：重新登录

**何时需要重新登录**：

1. **Session 过期**（30秒后）
2. **关闭并重新打开 Telegram 对话框**
3. **更换设备或重新安装 Telegram**

**重新登录流程**：

1. **打开 Google Authenticator**：
   - 获取当前的 6 位数字验证码

2. **发送登录命令**：
   ```
   /login user@example.com 123456
   ```

3. **收到登录成功消息**：
   ```
   ✅ 登录成功！

   账户: user@example.com
   Telegram ChatID: 987654321
   免验证时长: 30秒

   💡 30秒内使用其他指令无需输入邮箱和OTP。
   💡 您的 Telegram ChatID 已自动配置，后续将只向您推送通知。
   ```

**重要提示**：
- ✅ 重新登录后，你的 Telegram ChatID 会更新（如果更换了设备）
- ✅ 系统会自动使用新的 ChatID 推送通知
- ✅ 无需手动配置 ChatID

## 三、常见使用场景

### 场景 1：查看账户状态

**步骤**：
1. 发送 `/account` 命令
2. 如果 Session 有效，立即返回账户信息
3. 如果 Session 过期，需要提供邮箱和 OTP：
   ```
   /account user@example.com 123456
   ```

**返回信息**：
- 总权益、可用余额、已用保证金
- 总盈亏（含百分比）
- 持仓数量
- 每个持仓的详细信息

### 场景 2：设置止损

**步骤**：
1. 查看当前持仓：`/account`
2. 确定要设置止损的币种和价格
3. 发送止损命令：
   ```
   /sl BTCUSDT 42000
   ```
   或（Session 过期后）：
   ```
   /sl BTCUSDT 42000 user@example.com 123456
   ```

**返回信息**：
```
✅ 已设置 BTCUSDT long 止损: $42000.00
```

### 场景 3：紧急平仓

**步骤**：
1. 确定要平仓的币种和方向（long 或 short）
2. 发送平仓命令：
   ```
   /close BTCUSDT long
   ```
   或（Session 过期后）：
   ```
   /close BTCUSDT long user@example.com 123456
   ```

**返回信息**：
```
✅ 已平仓 BTCUSDT long
💰 价格: $43,280.00
📊 数量: 0.00100000
📥 开仓价: $43,250.50
📈 盈亏: $29.50
```

### 场景 4：查看价格

**步骤**：
1. 发送价格查询命令（无需验证）：
   ```
   /price BTCUSDT
   ```

**返回信息**：
```
💰 BTCUSDT 当前价格

📊 价格: $43,280.50
📈 4h 涨跌: 2.50%
📈 1h 涨跌: 0.50%
📊 区间最高: $43,500.00
📊 区间最低: $42,800.00
```

## 四、常见问题（FAQ）

### Q1: 为什么需要设置 2FA？

**A**: 2FA（双因素认证）是系统的安全机制，确保只有合法用户可以使用命令。没有 2FA，系统无法验证你的身份。

### Q2: Session 过期后怎么办？

**A**: 有两种方式：
1. **重新登录**：`/login user@example.com 123456`
2. **在命令末尾提供邮箱和 OTP**：`/account user@example.com 123456`

### Q3: 为什么需要每次输入 OTP？

**A**: 为了安全。Session 30秒后过期，需要重新验证。这是正常的安全机制。

### Q4: 可以关闭自动推送吗？

**A**: 目前不支持关闭自动推送。如果你不想接收通知，可以：
- 在 Telegram 中静音该对话
- 或联系管理员禁用你的 Telegram ChatID

### Q5: 更换设备后需要重新配置吗？

**A**: 不需要。只需：
1. 在新设备上打开 Telegram
2. 找到系统 bot
3. 发送 `/login` 命令重新登录
4. 系统会自动更新你的 ChatID

### Q6: 忘记邮箱或 OTP 怎么办？

**A**: 
- **忘记邮箱**：联系管理员
- **忘记 OTP**：在 Web 界面重新设置 2FA

### Q7: 为什么收不到通知？

**A**: 可能的原因：
1. **从未执行过 `/login` 命令**：
   - ❌ 仅点开对话框（发送 `/start`）**不会保存 ChatID**
   - ✅ 必须执行 `/login` 命令才能保存 ChatID
   - 如果从未执行过 `/login`，ChatID 为 0，无法推送
2. **ChatID 未配置**：只有执行 `/login` 命令后，ChatID 才会被保存到数据库
3. **交易员未运行**：检查交易员状态
4. **网络问题**：检查 Telegram 连接

**重要区别**：
- **点开对话框**（`/start`）：只是收到欢迎消息，**不会保存 ChatID**
- **执行登录**（`/login`）：这才是真正的"登录"，**会保存 ChatID**

**注意**：推送通知**不依赖 Session**，只要曾经执行过 `/login` 命令（ChatID 已保存），即使 Session 过期也能收到推送。

### Q8: 可以同时使用多个设备吗？

**A**: 可以。但需要注意：
- 每个设备有独立的 ChatID
- 最后登录的设备会更新你的 ChatID
- 通知只会发送到最新的 ChatID

### Q9: 如何查看所有可用命令？

**A**: 发送 `/help` 命令查看完整帮助信息。

### Q10: 命令执行失败怎么办？

**A**: 检查：
1. **Session 是否过期**：如果过期，提供邮箱和 OTP
2. **OTP 是否正确**：确保使用 Google Authenticator 中的当前验证码
3. **命令格式是否正确**：参考 `/help` 中的示例
4. **交易员是否运行**：使用 `/trader` 命令检查状态

## 五、最佳实践

### 1. 保护账户安全

- ✅ **不要分享 OTP**：OTP 码是私密的，不要告诉任何人
- ✅ **定期更换密码**：在 Web 界面定期更换密码
- ✅ **保护 OTP Secret**：如果丢失设备，及时在 Web 界面重新设置 2FA

### 2. 高效使用命令

- ✅ **利用 Session**：登录后 30 秒内可以免验证使用多个命令
- ✅ **批量操作**：在 Session 有效期内执行多个操作
- ✅ **查看帮助**：使用 `/help` 查看完整命令列表

### 3. 监控交易

- ✅ **关注通知**：及时查看自动推送的交易通知
- ✅ **定期检查账户**：使用 `/account` 命令定期检查账户状态
- ✅ **设置止损止盈**：使用 `/sl` 和 `/tp` 命令设置止损止盈

### 4. 故障排查

- ✅ **检查 Session**：如果命令失败，先检查 Session 是否过期
- ✅ **重新登录**：如果遇到问题，尝试重新登录
- ✅ **查看帮助**：使用 `/help` 查看命令格式

## 六、快速参考

### 常用命令

| 命令 | 说明 | 是否需要验证 |
|------|------|------------|
| `/start` | 显示欢迎消息 | 否 |
| `/help` | 显示完整帮助 | 否 |
| `/price [币种]` | 查看价格 | 否 |
| `/login [邮箱] [OTP]` | 登录 | 是 |
| `/account` | 查看账户 | Session 或 OTP |
| `/sl [币种] [价格]` | 设置止损 | Session 或 OTP |
| `/tp [币种] [价格]` | 设置止盈 | Session 或 OTP |
| `/close [币种] [方向]` | 平仓 | Session 或 OTP |
| `/trades [币种] [数量]` | 查看交易记录 | Session 或 OTP |
| `/trader [ID]` | 查看交易员状态 | Session 或 OTP |

### 命令格式

**基本格式**：
```
/命令 [参数1] [参数2] ...
```

**带验证格式**（Session 过期后）：
```
/命令 [参数1] [参数2] ... [邮箱] [OTP]
```

**示例**：
```
/account user@example.com 123456
/sl BTCUSDT 42000 user@example.com 123456
/close BTCUSDT long user@example.com 123456
```

## 七、总结

### 用户使用流程总结

1. **首次使用**：
   - 找到系统 bot → 发送 `/start` → 完成 2FA 设置 → 发送 `/login` → 开始使用

2. **日常使用**：
   - 接收自动推送通知 → 使用命令管理交易 → Session 过期后重新登录

3. **重新登录**：
   - 发送 `/login` 命令 → 系统自动更新 ChatID → 继续使用

### 关键要点

- ✅ **2FA 是必需的**：没有 2FA 无法使用命令
- ✅ **ChatID 自动配置**：登录时自动保存，无需手动配置
- ✅ **Session 30秒有效**：登录后 30 秒内免验证（**仅用于操作命令**）
- ✅ **自动推送通知**：登录后自动接收交易通知（**不依赖 Session**）
- ✅ **推送与操作分离**：
  - **推送通知**：只需曾经登录过（ChatID 已保存），即使 Session 过期也能收到
  - **操作命令**：需要有效的 Session 或提供 OTP
- ✅ **支持多设备**：可以在多个设备上使用

### 安全提示

- 🔒 **保护 OTP**：不要分享 OTP 码
- 🔒 **定期更换密码**：保持账户安全
- 🔒 **及时更新 2FA**：如果丢失设备，及时重新设置
