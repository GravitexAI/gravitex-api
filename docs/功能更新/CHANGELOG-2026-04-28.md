# 功能更新日志 — 2026-04-28 (main → main-merge-alpha)

**合并范围：** main 分支 2026-04-11 ~ 2026-04-28，共 101 个 commit
**变更规模：** 173 个文件，+17,532 行，-3,613 行

---

## 一、重大新功能

### 1. 阶梯计费表达式系统（Tiered Billing Expression）

用一个表达式字符串完整描述一个模型的全部计费规则，取代分散的倍率表。支持多档阶梯、缓存/图片/音频分维度定价、请求条件乘数。

**新增文件：**

| 文件 | 说明 |
|------|------|
| `pkg/billingexpr/` | 表达式引擎（编译、执行、结算、类型定义） |
| `setting/billing_setting/tiered_billing.go` | 存储层 |
| `service/tiered_settle.go` | 结算服务 |
| `relay/helper/billing_expr_request.go` | 请求上下文构建 |
| `web/src/pages/Setting/Ratio/components/TieredPricingEditor.jsx` | 可视化编辑器 |
| `web/src/pages/Setting/Ratio/components/requestRuleExpr.js` | 请求规则表达式构建器 |
| `web/src/components/table/model-pricing/modal/components/DynamicPricingBreakdown.jsx` | 动态定价展示组件 |
| `web/src/constants/billing.constants.js` | 计费常量 |

**修改文件：**

| 文件 | 变更 |
|------|------|
| `relay/helper/price.go` | 新增 `modelPriceHelperTiered()` 预扣费入口 |
| `service/text_quota.go` | 集成 tiered 结算 + `ToolCallSurchargeQuota` 字段 |
| `service/quota.go` | WSS/Audio 路径集成 `TryTieredSettle()` |
| `service/log_info_generate.go` | 新增 `InjectTieredBillingInfo()` |
| `relay/common/relay_info.go` | 新增 `TieredBillingSnapshot`、`BillingRequestInput` 字段 |
| `web/src/helpers/render.jsx` | 日志中 tiered_expr 模式渲染 |
| `web/src/hooks/usage-logs/useUsageLogsData.jsx` | billing_mode === 'tiered_expr' 过滤 |

> 详细使用说明见 [阶梯计费表达式使用指南](./阶梯计费表达式使用指南.md)

---

### 2. 工具调用计费系统（Tool Call Billing）

对 OpenAI / Claude / Gemini 内置工具（web_search、file_search、image_generation、google_search）单独定价，计费结果叠加到 token 费用之上。

**新增文件：**

| 文件 | 说明 |
|------|------|
| `service/tool_billing.go` | 工具计费逻辑 |
| `web/src/pages/Setting/Ratio/ToolPriceSettings.jsx` | 管理后台配置 UI |

**修改文件：**

| 文件 | 变更 |
|------|------|
| `setting/operation_setting/tools.go` | 工具价格存储、匹配算法、GPT Image 定价矩阵 |
| `service/text_quota.go` | `calculateTextToolCallSurcharge()` 集成 |

> 详细使用说明见 [工具调用计费使用指南](./工具调用计费使用指南.md)

---

### 3. Waffo Pancake 支付网关

**新增文件：**

| 文件 | 说明 |
|------|------|
| `service/waffo_pancake.go` | 支付核心服务（RSA 签名/验签） |
| `controller/topup_waffo_pancake.go` | HTTP 控制器（创建订单 + webhook） |
| `setting/payment_waffo_pancake.go` | 配置变量 |
| `controller/payment_webhook_availability.go` | 全网关可用性统一检查 |
| `web/src/pages/Setting/Payment/SettingsPaymentGatewayWaffoPancake.jsx` | 管理后台配置 UI |

**特性：**
- RSA-PKCS1v15-SHA256 请求签名 + webhook 签名验证
- 独立 sandbox / production 密钥对
- 5 分钟防重放攻击窗口
- 多币种支持（默认 USD）

---

### 4. 上游定价同步重构

**修改文件：** `controller/ratio_sync.go`、`web/src/pages/Setting/Ratio/UpstreamRatioSync.jsx`

- 支持从 `basellm.github.io` 和 `models.dev` 两个预设源同步
- OpenRouter 渠道自动使用 openrouter 端点
- 新增同步 `billing_mode` 和 `billing_expr` 字段
- 差异表格增加模型搜索、字段类型过滤（9 种）
- 批量选择 + 冲突确认弹窗（价格/倍率类型切换时警告）

---

## 二、新模型支持

### DeepSeek V4 推理模式

**修改文件：** `relay/channel/deepseek/adaptor.go`、`relay/channel/deepseek/constants.go`、`setting/reasoning/suffix.go`

- 支持 `-none`（禁用思考）和 `-max`（最大推理）后缀
- 同时支持 OpenAI 格式和 Claude 格式请求转换
- URL 路由：Claude 格式 → `/anthropic/v1/messages`，FIM → `/beta/completions`

### Claude Opus 4.6 / 4.7 / Sonnet 4.6

**修改文件：** `relay/channel/claude/constants.go`、`relay/channel/claude/relay-claude.go`、`relay/channel/aws/constants.go`

| 新模型 | 努力级别变体 |
|--------|------------|
| `claude-opus-4-6` | -max / -high / -medium / -low |
| `claude-sonnet-4-6` | — |
| `claude-opus-4-7` | -max / -xhigh / -high / -medium / -low / -thinking |

**Opus 4.7 特殊处理：**
- 使用 `thinking.type = "adaptive"` + `thinking.display = "summarized"`
- 自动清除 temperature / top_p / top_k（不兼容非默认值）

**AWS Bedrock 映射：** 三个新模型均已添加 us / ap / eu 跨区域推理支持。

### Claude speed 透传控制

**修改文件：** `dto/channel_settings.go`、`web/src/components/table/channels/modals/EditChannelModal.jsx`

- 渠道设置新增 `allow_speed` 开关（仅 Claude 渠道类型 14）
- 默认关闭，防止意外切换到 fast 推理速度模式

---

## 三、安全增强

### 支付网关跨域回调攻击防护

**修改文件：** `model/topup.go`、`controller/payment_webhook_availability.go`

- TopUp 记录新增 `PaymentProvider` 字段
- 每个 `Recharge*` 函数验证 provider 匹配后才执行
- 防止 A 网关的合法 webhook 充值到 B 网关的订单
- 所有网关统一 3 层检查：`isXxxTopUpEnabled()` → `isXxxWebhookConfigured()` → `isXxxWebhookEnabled()`

### SSRF 防护增强

**修改文件：** `common/ssrf_protection.go`

- 完整覆盖私有/保留 IPv4 和 IPv6 地址段（含 CGNAT 100.64/10、ULA fc00::/7 等）
- 支持域名 DNS 解析后 IP 过滤
- 端口范围解析（"8000-9000" 格式）
- 域名/IP 黑白名单模式

### Passkey 安全验证强化

**修改文件：** `controller/passkey.go`

- Passkey 注册/删除需要 2FA 预验证（如已启用 2FA）
- 时间窗口限制 + 方法匹配验证，防止会话重放
- 验证完成后清除安全验证状态

### 信息泄露防护

**修改文件：** `middleware/auth.go`、`controller/user.go`

- 登录错误区分数据库错误 vs 凭证无效，不返回原始错误字符串
- Token 认证中 DB 错误返回 500（非"无效 token"）
- `GetSelf` 接口清除 `user.Remark` 再返回
- Token 搜索 `sanitizeLikePattern` 防止 LIKE 注入

### Stripe 异步支付安全

**修改文件：** `controller/topup_stripe.go`

- 新增 `checkout.session.async_payment_succeeded` / `failed` / `expired` 事件处理
- PaymentProvider 校验防跨网关状态篡改
- 自定义 SuccessURL/CancelURL 经过可信域名白名单校验

---

## 四、用户管理改进

### 用户注册时间 & 最后登录时间

**修改文件：** `model/user.go`、`controller/user.go`、`web/src/components/table/users/UsersColumnDefs.jsx`

- 新增 `created_at`（GORM autoCreateTime）和 `last_login_at` 字段
- 每次密码 / OAuth / Passkey 登录后更新 `last_login_at`
- 管理后台用户表新增对应列

### 令牌表"最后使用时间"列

**修改文件：** `web/src/components/table/tokens/TokensColumnDefs.jsx`

### 管理员充值日志审计

**修改文件：** `model/topup.go`、`controller/topup.go`、`model/log.go`

- 充值日志显示管理员用户 ID
- 管理日志对普通用户隐藏管理员身份
- 充值搜索限制 30 天 + COUNT 上限 10000（防 DoS）
- Webhook 处理记录完整请求体、客户端 IP、签名头
- 新增 `NODE_NAME` 环境变量支持（审计日志标记节点）

### 禁用用户缓存清除

**修改文件：** `model/user_cache.go`

- 禁用用户时同时清除该用户所有 token 的 Redis 缓存，确保立即生效

### Legacy Token Key 兼容

**修改文件：** `model/token.go`

- 支持更长的旧格式 token key
- 新增跨数据库迁移测试

---

## 五、渠道管理改进

### 获取模型弹窗显示已删除模型

**修改文件：** `web/src/components/table/channels/modals/ModelSelectModal.jsx`

- 三 Tab 布局：新获取的模型 / 已有的模型 / 上游已删除的模型
- 按厂商分组折叠面板 + 批量勾选
- 重定向模型标记提示

### 上游模型检查加载 model mapping

**修改文件：** `controller/channel_upstream_update.go`

### Codex 凭证刷新

**修改文件：** `service/codex_credential_refresh_task.go`

- 自动禁用的渠道刷新 codex 凭证
- 渠道自动测试改用 stream 模式
- Codex Usage Modal 布局优化

### 多密钥管理弹窗索引修复

**修改文件：** `web/src/components/table/channels/modals/MultiKeyManageModal.jsx`

- 索引值从 1 开始显示（之前从 0 开始）

### 阿里云原生 messages 模型匹配

**修改文件：** `relay/channel/ali/adaptor.go`

- 配置 native messages 模型匹配规则

---

## 六、仪表盘 & UI 改进

### 仪表盘用户消耗图表

**修改文件：** `web/src/hooks/dashboard/useDashboardCharts.jsx`

- **用户消耗排行（横向柱状图）：** Top 10 用户额度消耗排名
- **用户消耗趋势（面积图）：** 按用户分组的消耗时间趋势

### 订阅卡显示下次额度重置时间

**修改文件：** `web/src/components/topup/SubscriptionPlansCard.jsx`

### 个人设置页面组件化重构

**修改文件：** `web/src/components/settings/PersonalSetting.jsx`

- 拆分为独立子组件：UserInfoHeader、AccountManagement、NotificationSettings 等
- Passkey 管理集成 2FA 预验证流程

### GroupTable 输入光标跳转修复

**修改文件：** `web/src/pages/Setting/Ratio/components/GroupTable.jsx`

### 管理员配额调整日志记录用户名

**修改文件：** `controller/topup.go`

---

## 七、Bug 修复

| 修复内容 | 文件 |
|---------|------|
| 错误日志 `isStream` 使用实际状态而非硬编码 false | `controller/relay.go` |
| Azure responses/compact URL 路由支持 | `relay/channel/openai/adaptor.go` |
| Claude 空字符串 content 处理优化 | `relay/channel/claude/relay-claude.go` |
| Claude 请求 TopP 设为 nil | `relay/channel/claude/relay-claude.go` |
| GPT-5.5 completion ratio 修正 | 倍率配置 |
| 图片模型仅价格模型使用 N 倍率 | `relay/image_handler.go` |
| 阶梯计费结算边界情况修复 | `service/tiered_settle.go` |
| OpenAI Responses Instructions 字段改用 json.RawMessage | `dto/openai_response.go` |
| tool arguments 支持原始 JSON 对象 | 解析逻辑 |
| 渠道亲和禁用重试消息优化 | 重试逻辑 |
| 模型定价显示类型修正 | 定价渲染 |
| Gemini ToolConfig 增加 IncludeServerSideToolInvocations | `dto/gemini.go` |
| 配置更新 JSON map 反序列化修复 | `setting/config/config.go` |
| 阶梯定价渲染中按缓存 token 可用性过滤变量 | 前端渲染 |
| 充值卡缺失 Tag 导入 | `web/src/components/topup/RechargeCard.jsx` |
| 倍率/价格全模型序列化确保同步延迟回退 | 定价设置 |

---

## 八、依赖更新

| 依赖 | 变更 |
|------|------|
| `github.com/jackc/pgx/v5` | 5.7.1 → 5.9.2 |
| `axios` (前端) | 1.13.5 → 1.15.0 |
| `@xmldom/xmldom` (electron) | 0.8.12 → 0.8.13 |

---

## 九、运维 & 配置

| 改进 | 说明 |
|------|------|
| Docker Compose 默认 Redis 密码 | `docker-compose.yml` |
| Nightly Docker 镜像工作流 | `.github/workflows/docker-image-nightly.yml` |
| `NODE_NAME` 环境变量 | 审计日志标记部署节点名称 |
| 豆包 Seed 1.8 定价档位 | 倍率预设新增 |

---

## 十、合并冲突解决记录

共 12 个冲突文件，以 main-merge-alpha 为准解决：

| 文件 | 策略 |
|------|------|
| `.gitignore` | 保留 alpha 的 `.env`，加入 main 的 `.test`、`skills-lock.json` |
| `common/init.go` | 保留 RuoYi 变量，加入 `NodeName` |
| `controller/relay.go` | 保留重试日志链路，采纳 isStream 修复 |
| `dto/channel_settings.go` | 保留 alpha 全部字段（BytePlus、AzureModelApiVersions），加入 `AllowSpeed` |
| `middleware/auth.go` | 保留 RuoYi 条件判断，采纳 i18n 消息翻译 |
| `relay/channel/openai/adaptor.go` | 采纳 compact 模式支持，保留 alpha 的 apiVersion |
| `relay/helper/price.go` | 保留视频计费逻辑，合入 tiered expr 计费系统 |
| `service/quota.go` (x2) | 保留 system_request_id + priceChain，合入 tiered billing info |
| `service/text_quota.go` | 保留 Gemini 图文分离字段，加入 `ToolCallSurchargeQuota` |
| `UsageLogsColumnDefs.jsx` | 合并 type===7 + admin 管理日志 IP 显示 |
| `render.jsx` | 保留 alpha 独立参数签名（含 Gemini 字段） |
| `useUsageLogsData.jsx` | 保留视频按秒计费分支，加入 tiered_expr 过滤 |
| `zh-TW.json` (x4) | 保留品牌翻译，加入 main 新增条目 |
