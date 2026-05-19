# 流式请求中断时的计费机制与渠道支持说明

> 文档目的：说明 Gravitex 平台在用户中断流式请求时的计费处理流程，以及各渠道对"本地计费"和"上游返回"两种 token 计数模式的支持情况。
> 最后更新：2026-05-18

---

## 一、核心概念：本地计费 vs 上游返回

Gravitex 平台在流式请求结束后，需要通过 token 数量来计算实际消耗的配额。token 数量的来源分两种：

| 计费模式 | 前端显示 | 判断标志 | 触发条件 |
|---------|---------|---------|---------|
| **本地计费** | `本地计费` | `admin_info.local_count_tokens = true` | 上游流式响应中**未返回** `usage` 字段，由平台根据响应文本本地估算 token |
| **上游返回** | `上游返回` | `admin_info.local_count_tokens` 不存在或为 `false` | 上游流式响应中**返回了** `usage` 字段，直接使用上游的 token 计数 |

**标志设置路径**：
- 常量定义：`constant/context_key.go:56` → `ContextKeyLocalCountTokens`
- 标志写入：`service/usage_helpr.go:23` → `ResponseText2Usage()` 函数中置为 `true`
- 日志记录：`service/log_info_generate.go:68-70` → 写入 `admin_info["local_count_tokens"]`
- 前端展示：`web/src/hooks/usage-logs/useUsageLogsData.jsx:715` → 根据标志决定显示文案

---

## 二、流式请求的完整计费生命周期

### 2.1 预扣费阶段

请求开始时，系统根据 **prompt token + max_tokens** 估算出最大可能消耗，预扣用户配额。

```
controller/relay.go:163  →  service.PreConsumeBilling()
                              └─ service/billing_session.go:189  →  BillingSession.preConsume()
```

预扣额度计算在 `relay/helper/price.go:67-203` (`ModelPriceHelper`)，综合考虑：
- 模型倍率（`ModelRatio`、`CompletionRatio`）
- 用户组倍率（`HandleGroupRatio`）
- 阶梯计费表达式（`BillingExpr`）
- 图像/音频倍率（`ImageRatio`、`AudioRatio`）

### 2.2 流式传输阶段

代理通过 `relay/helper/stream_scanner.go` 中的 `StreamScannerHandler` 进行 SSE 流转发，核心机制：

- 从上游读取 SSE 数据块，逐块转发给客户端
- 同时将所有已接收的文本内容累积到 `responseTextBuilder`
- 如果上游返回了 `usage` chunk，记录在 `streamItems` 中

**中断检测**（`stream_scanner.go:232-234`）：
```go
case <-c.Request.Context().Done():
    info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
    return
```

当客户端断开连接时：
1. `StreamStatus.EndReason` 设为 `"client_gone"`
2. 停止从上游读取新数据
3. **已接收的数据块全部保留**，不会丢弃
4. 进入结算阶段

流式状态记录在日志的 `stream_status` 字段中（`service/log_info_generate.go:97-122`）：

| 字段 | 说明 |
|------|------|
| `status` | `"ok"` / `"error"` |
| `end_reason` | `"done"` / `"client_gone"` / `"timeout"` / `"eof"` / `"scanner_error"` 等 |

### 2.3 结算阶段

流结束后（无论正常结束还是被中断），执行结算流程：

```
handler/adaptor.DoResponse()  →  获取 usage 对象
    └─ service/text_quota.go:361  →  PostTextConsumeQuota()
        ├─ 计算实际消耗的配额（根据 usage 中的 token 数 × 倍率）
        ├─ 如果总 token 为 0 → 退还全部预扣额度
        ├─ 否则 → actualQuota - preConsumedQuota = delta
        │   ├─ delta > 0 → 补扣差额
        │   └─ delta < 0 → 退还差额
        └─ service/billing.go:34  →  SettleBilling() 完成结算
```

**中断场景的关键行为**：`usage` 对象基于**中断前已成功接收的流式数据块**构建，用户只需要为实际传输的 token 付费，未传输的部分不收费。

### 2.4 错误/异常路径

如果适配器在处理过程中返回错误（非正常流结束），`controller/relay.go:169-178` 中的 `defer` 处理器会调用 `relayInfo.Billing.Refund(c)` **全额退还预扣额度**。

---

## 三、各渠道 token 计数模式详解

### 3.1 支持透传 StreamOptions 的渠道（可能上游返回 usage）

这些渠道在 `relay/common/relay_info.go:318-337` 的白名单中，代理会保留请求中的 `stream_options: {include_usage: true}`，让上游有机会在流中返回 `usage`：

| 序号 | 常量名 | 渠道 | 实际是否返回 usage |
|------|--------|------|-------------------|
| 1 | `ChannelTypeOpenAI` | OpenAI | 支持（理论上有，但中断时收不到，详见已知问题） |
| 2 | `ChannelTypeAnthropic` | Anthropic | 支持（`message_delta` 事件中返回） |
| 3 | `ChannelTypeAws` | AWS Bedrock | 取决于具体模型 |
| 4 | `ChannelTypeGemini` | Gemini | 非流式返回；流式回退本地估算 |
| 5 | `ChannelCloudflare` | Cloudflare | 回退本地估算 |
| 6 | `ChannelTypeAzure` | Azure | 支持 |
| 7 | `ChannelTypeVolcEngine` | 火山引擎 | 取决于模型 |
| 8 | `ChannelTypeOllama` | Ollama | 支持（`done` chunk 中返回） |
| 9 | `ChannelTypeXai` | xAI | 回退本地估算 |
| 10 | `ChannelTypeDeepSeek` | DeepSeek | 支持（`last_chunk` 中返回） |
| 11 | `ChannelTypeBaiduV2` | 百度 V2 | 取决于模型 |
| 12 | `ChannelTypeZhipu_v4` | 智谱 V4 | 支持 |
| 13 | `ChannelTypeAli` | 阿里 | 支持 |
| 14 | `ChannelTypeSubmodel` | Submodel | 取决于上游 |
| 15 | `ChannelTypeCodex` | Codex | 取决于上游 |
| 16 | `ChannelTypeMoonshot` | Moonshot | 支持 |
| 17 | `ChannelTypeMiniMax` | MiniMax | 取决于模型 |
| 18 | `ChannelTypeSiliconFlow` | SiliconFlow | 支持 |

**重要**：即使渠道在白名单内，如果上游实际未返回 `usage`，系统会回退到本地估算（`relay-openai.go:181-188`）：
```go
if !containStreamUsage {
    usage = service.ResponseText2Usage(c, responseTextBuilder.String(), ...)
}
```

### 3.2 不支持 StreamOptions 的渠道（强制本地计费）

以下渠道**不在**白名单中，`StreamOptions` 在 `compatible_handler.go:55` 被直接剔除，上游永远不会返回 usage，**强制走本地计费**：

| 渠道 |
|------|
| Coze |
| Dify |
| 腾讯 |
| Palm |
| 以及其他所有未列入白名单的渠道 |

```go
// compatible_handler.go:55
if !info.SupportStreamOptions || !lo.FromPtrOr(request.Stream, false) {
    request.StreamOptions = nil
}
```

### 3.3 本地 token 估算方法

`service/usage_helpr.go:22-28` 中的 `ResponseText2Usage()`：
```go
func ResponseText2Usage(c *gin.Context, responseText string, modeName string, promptTokens int) *dto.Usage {
    common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
    usage.CompletionTokens = EstimateTokenByModel(modeName, responseText)
    // ...
}
```

`EstimateTokenByModel` 位于 `service/token_counter.go:262`，根据不同模型的 tokenizer 特性估算 token 数。

---

## 四、流式中断时 token 计数的已知问题

### 4.1 OpenAI 通道：中断时拿不到 usage

OpenAI 的 SSE 事件顺序固定为：
```
delta chunks → finish_reason → usage chunk → [DONE]
                                      ↑
                             只在最后一刻才发出
```

即使设置了 `stream_options: {include_usage: true}`，只要流被中断，客户端永远收不到 `usage` chunk。此时 Gravitex 会**回退到本地估算**（`containStreamUsage = false`），估算值与上游实际计费可能存在偏差。详见 `docs/cancel/openai-streaming-cancel-billing.md`。

### 4.2 Anthropic 通道：message_delta 中包含 usage

Anthropic 在 `message_delta` 事件中返回 `usage`，即使流未完成也可能收到。但如果客户端在 `message_delta` 之前断开，同样回退到本地估算。

### 4.3 回退估算的精度风险

本地估算基于字符数/token 比率估算，对于包含特殊字符、emoji、多字节文本的场景可能存在偏差。偏差方向取决于模型 tokenizer 的具体行为：

- 英文文本：估算通常准确，误差 <5%
- 中文文本：可能偏低（中文字符的 token 数通常比英文多）
- 代码/特殊字符：可能偏高或偏低

---

## 五、与上游真实计费的差异说明

Gravitex 作为代理网关，有两层计费概念：

| 层级 | 说明 | 中断时的行为 |
|------|------|------------|
| **平台计费**（扣用户配额） | Gravitex 根据接收到的 token 向用户收费 | 只收已传输的 token |
| **上游计费**（扣渠道成本） | 上游 API 供应商向 Gravitex 收费 | 取决于供应商的策略 |

**重点**：平台计费和上游计费可能不一致。以 Anthropic/AWS Bedrock 为例，平台可能只收到部分流式数据，但上游仍按完整生成内容收费。这部分成本由平台承担，详见 `docs/cancel/claude_cancel.md` 和 `docs/cancel/claude_cancel_faq.md`。

---

## 六、相关代码索引

| 文件 | 职责 |
|------|------|
| `constant/context_key.go:56` | `ContextKeyLocalCountTokens` 常量定义 |
| `service/usage_helpr.go:22-28` | `ResponseText2Usage` — 本地 token 估算 + 设置标志 |
| `service/token_counter.go:262` | `EstimateTokenByModel` — 按模型估算 token |
| `service/log_info_generate.go:60-80` | 构建 admin_info，写入 local_count_tokens |
| `service/log_info_generate.go:97-122` | `appendStreamStatus` — 记录流式状态 |
| `service/text_quota.go:361-445` | `PostTextConsumeQuota` — 文本请求结算 |
| `service/billing.go:19-50` | `PreConsumeBilling` / `SettleBilling` |
| `service/billing_session.go:42-85` | `BillingSession.Settle` — 核心结算逻辑 |
| `relay/helper/stream_scanner.go:114-300` | `StreamScannerHandler` — 流式扫描引擎 |
| `relay/helper/stream_scanner.go:232-234` | 客户端断连检测 |
| `relay/common/stream_status.go:13-95` | `StreamStatus` / `StreamEndReason` 定义 |
| `relay/common/relay_info.go:318-337` | `streamSupportedChannels` 白名单 |
| `relay/common/relay_info.go:229-231` | 根据白名单设置 `SupportStreamOptions` |
| `relay/compatible_handler.go:55` | 剔除不支持渠道的 `StreamOptions` |
| `relay/channel/openai/relay-openai.go:106-194` | OpenAI 流式处理 + token 汇总 |
| `relay/channel/openai/helper.go:78-254` | `processTokens` / `handleLastResponse` |
| `relay/channel/claude/relay-claude.go:784-867` | Claude 流式处理 |
| `controller/relay.go:163-178` | 预扣费 + 异常退款 |
| `web/src/hooks/usage-logs/useUsageLogsData.jsx:713-723` | 前端"本地计费"/"上游返回"展示 |