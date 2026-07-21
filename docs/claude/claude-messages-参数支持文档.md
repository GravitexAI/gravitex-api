# Claude Messages API 参数支持文档（Sonnet / Haiku / Opus）

> 用途：说明 `/v1/messages` 端点各请求参数的类型、取值、以及 **Sonnet / Haiku / Opus 各模型的差异**，供接入方构造正确请求、避免类型错误。
>
> 数据来源：
> - Anthropic 官方 API 文档（`platform.claude.com/docs/en/api/messages`）
> - Anthropic 官方模型总览（`platform.claude.com/docs/en/docs/about-claude/models/overview`）
> - Anthropic Extended / Adaptive Thinking 官方文档
> - 本网关 DTO 定义（`dto/claude.go` → `ClaudeRequest` / `Thinking`）
>
> 更新时间：2026-07-15

---

## 0. 快速避坑（最常见的报错）

**`thinking.type` 是字符串枚举，不是布尔值。**

```jsonc
// ❌ 错误：会报 json: cannot unmarshal bool into ...thinking.type of type string
"thinking": { "type": false }

// ✅ 正确
"thinking": { "type": "disabled" }        // 关闭思考
"thinking": { "type": "adaptive" }        // 自适应思考（Opus 4.8 / Sonnet 5）
"thinking": { "type": "enabled", "budget_tokens": 4096 }  // 手动思考（Haiku 4.5）
```

> 不需要思考时，**最省事的做法是直接删掉整个 `thinking` 字段**。

---

## 1. 模型能力矩阵（当前主力模型）

| 能力 | Claude Opus 4.8 | Claude Sonnet 5 | Claude Haiku 4.5 |
|------|-----------------|-----------------|------------------|
| **API 模型 ID** | `claude-opus-4-8` | `claude-sonnet-5` | `claude-haiku-4-5-20251001` |
| **API 别名** | `claude-opus-4-8` | `claude-sonnet-5` | `claude-haiku-4-5` |
| **上下文窗口** | 1M tokens | 1M tokens | 200k tokens |
| **最大输出** | 128k tokens | 128k tokens | 64k tokens |
| **Extended thinking**（手动 `enabled`+budget） | ❌ 不支持 | ❌ 不支持 | ✅ 支持 |
| **Adaptive thinking**（`adaptive` 自适应） | ✅ 支持 | ✅ 支持 | ❌ 不支持 |
| **定价（输入/输出，每 MTok）** | $5 / $25 | $3 / $15 ¹ | $1 / $5 |

> ¹ Sonnet 5 有引导期优惠价 $2 / $10（每 MTok），截至 2026-08-31。

### ⚠️ 关键差异：Opus/Sonnet 与 Haiku 的思考模式是**相反的**

| 想开启思考 | Opus 4.8 / Sonnet 5 | Haiku 4.5 |
|-----------|--------------------|-----------|
| 正确写法 | `{ "type": "adaptive" }` | `{ "type": "enabled", "budget_tokens": N }` |
| 会报错的写法 | `{ "type": "enabled", ... }`（新模型不支持手动 budget） | `{ "type": "adaptive" }`（Haiku 不支持自适应） |
| 关闭思考 | `{ "type": "disabled" }` | `{ "type": "disabled" }` |

> Opus 4.8 / Sonnet 5 等新模型**不支持** `type: "enabled"` + `budget_tokens`，必须用 `adaptive` 或 `disabled`。
> Haiku 4.5 **不支持** `adaptive`，要开思考只能用 `enabled` + `budget_tokens`。

<details>
<summary>其他模型思考支持（Fable 5 / 旧版模型）</summary>

| 模型 | Extended thinking | Adaptive thinking |
|------|------------------|-------------------|
| Claude Fable 5 (`claude-fable-5`) | ❌ | ✅（始终开启） |
| Claude Opus 4.7 (`claude-opus-4-7`) | ❌ | ✅ |
| Claude Opus 4.6 (`claude-opus-4-6`) | ✅ | ✅ |
| Claude Sonnet 4.6 (`claude-sonnet-4-6`) | ✅ | ✅ |
| Claude Sonnet 4.5 (`claude-sonnet-4-5`) | ✅ | ❌ |

</details>

---

## 1.5 版本演进与参数支持差异

这一节说明各能力**从哪个版本引入、哪些版本不再支持哪些参数**。

### 1.5.1 思考模式的版本演进

| 阶段 | 代表版本 | Manual（`enabled`+budget） | Adaptive（`adaptive`） | 说明 |
|------|---------|--------------------------|----------------------|------|
| **初代思考** | 3.7 系列、Opus/Sonnet 4.5、**Haiku 4.5** | ✅ 唯一方式 | ❌ 不支持 | 只能手动设 budget_tokens |
| **引入 Adaptive** | **Opus 4.6 / Sonnet 4.6** | ✅ 仍可用（已弃用⚠️） | ✅ 需显式开启（默认关） | 两种模式并存，官方开始推荐 adaptive |
| **移除 Manual** | **Opus 4.7 / Opus 4.8 / Sonnet 5** | ❌ 传了报 **400** | ✅ | 手动 budget 被硬性拒绝 |
| **Adaptive 常开** | Fable 5 / Mythos 5 | ❌ | ✅ 始终开启，**不支持 disabled** | 无法关闭思考 |

> **一句话总结版本分水岭：**
> - **Adaptive 从 `4.6` 代（Opus 4.6 / Sonnet 4.6）开始引入**；4.5 及更早（含 Haiku 4.5）**不支持** adaptive。
> - **Manual（`enabled`+`budget_tokens`）从 `4.7` 代（Opus 4.7）开始被移除**，传了直接 400；只有 4.6 代及更早、以及 Haiku 4.5 还支持。

### 1.5.2 各版本 thinking 行为对照

| 模型 | 不传 thinking 时 | `enabled`+budget | `adaptive` | `disabled` |
|------|-----------------|------------------|-----------|-----------|
| **Opus 4.8** | 关闭 | ❌ 400 | ✅ 需显式传 | ✅ |
| **Opus 4.7** | 关闭 | ❌ 400 | ✅ 需显式传 | ✅ |
| **Sonnet 5** | **默认 adaptive 开启** | ❌ 400 | ✅ | ✅ 需显式关 |
| **Haiku 4.5** | 关闭 | ✅ | ❌ 400 | ✅ |
| Opus 4.6 / Sonnet 4.6 | 关闭 | ✅（弃用⚠️） | ✅ 需显式传 | ✅ |
| Opus 4.5 / Sonnet 4.5 | 关闭 | ✅ | ❌ | ✅ |
| Fable 5 / Mythos 5 | **始终 adaptive** | ❌ | ✅ | ❌ 不支持关闭 |

### 1.5.3 ⚠️ 采样参数限制（易踩坑，与思考无关）

**Fable 5、Mythos 5、Opus 4.8、Opus 4.7、Sonnet 5** 会**拒绝非默认的 `temperature` / `top_p` / `top_k`**，传了非默认值直接报 **400**——无论是否开启思考、每个请求都生效。

| 采样参数 | Opus 4.8 / 4.7 / Sonnet 5 / Fable 5 | Opus 4.6 及更早 / Haiku 4.5 |
|---------|-------------------------------------|----------------------------|
| `temperature` | ⚠️ 只能用默认值（否则 400） | ✅ 可自定义 0.0–1.0 |
| `top_p` | ⚠️ 只能用默认值（否则 400） | ✅ 可自定义 |
| `top_k` | ⚠️ 只能用默认值（否则 400） | ✅ 可自定义 |

> 接入新模型（4.7+）时，**不要透传自定义 temperature/top_p/top_k**，否则会 400。

### 1.5.4 `effort` 参数的版本差异

`effort`（配合 adaptive 使用，取值 `low`/`medium`/`high`/`xhigh`/`max`）：

| 项目 | 支持版本 |
|------|---------|
| `effort` 基础档位（low~max） | 所有支持 adaptive 的模型 |
| `xhigh` 档位 | 仅 **Fable 5 / Mythos 5 / Opus 4.8 / Opus 4.7 / Sonnet 5** |
| 默认 `high` | Opus 4.8（全平台）、Sonnet 5（API / Claude Code） |

### 1.5.5 `thinking.display` 默认值的版本变化

| 模型 | `display` 默认值 | 影响 |
|------|-----------------|------|
| Opus 4.6 / Sonnet 4.6 及更早 Claude 4 | `"summarized"` | 默认返回可读思考摘要 |
| **Sonnet 5 / Opus 4.8 / Opus 4.7 / Fable 5 / Mythos 5** | `"omitted"` | 思考正文默认**为空**，需显式传 `display: "summarized"` 才能看到 |

> ⚠️ 这是从 Opus 4.6 到新模型的**静默变更**：老代码升级到新模型后，思考正文会突然变空，需要主动加 `display: "summarized"`。

### 1.5.6 其他版本变更

- **分词器变更（Opus 4.7 起）**：Opus 4.7 及之后（含 Opus 4.8 / Sonnet 5 / Fable 5）使用新分词器，同样文本**约多产生 30% token**，影响计费与上下文占用估算。
- **模型 ID 格式（4.6 代起）**：从 4.6 代开始改用无日期格式（如 `claude-opus-4-8`），但**仍是固定快照**，不是滚动指针。

---

## 2. 请求参数总表（`POST /v1/messages`）

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | ✅ | 模型 ID，如 `claude-opus-4-8` |
| `messages` | array | ✅ | 对话消息数组，user/assistant 交替 |
| `max_tokens` | integer | ✅ | 生成的最大 token 数；设为 `0` 可仅预热缓存不生成 |
| `system` | string 或 array | 可选 | 系统提示词，替代 message 里的 system 角色 |
| `stream` | boolean | 可选 | 是否 SSE 流式返回 |
| `temperature` | number | 可选 | 随机度 0.0–1.0，默认 `1.0` ⚠️4.7+ 新模型只能用默认值（见 §1.5.3） |
| `top_p` | number | 可选 | 核采样累积概率阈值 ⚠️4.7+ 新模型只能用默认值（见 §1.5.3） |
| `top_k` | integer | 可选 | 只从概率最高的 K 个 token 中采样 ⚠️4.7+ 新模型只能用默认值（见 §1.5.3） |
| `stop_sequences` | array&lt;string&gt; | 可选 | 自定义停止序列 |
| `tools` | array | 可选 | 工具/函数定义（客户端工具或服务端工具） |
| `tool_choice` | object | 可选 | 工具调用策略（见 §4） |
| `thinking` | object | 可选 | 扩展思考配置（见 §3） |
| `metadata` | object | 可选 | 请求元信息，含 `user_id`（用于滥用检测） |
| `output_config` | object | 可选 | 结构化输出配置（JSON schema、`effort`） |
| `service_tier` | string | 可选 | `"auto"` 或 `"standard_only"`，优先级/容量选择 ⚠️网关默认过滤 |
| `container` | string | 可选 | 容器标识，跨请求复用 |
| `inference_geo` | string | 可选 | 推理地理区域（数据驻留）⚠️网关默认过滤 |
| `cache_control` | object | 可选 | 顶层 prompt cache 控制 |

---

## 3. `thinking` 参数详解

`thinking` 是一个对象，核心字段是字符串枚举 `type`：

```jsonc
// 手动开启（仅 Extended thinking 模型，如 Haiku 4.5）
{
  "type": "enabled",
  "budget_tokens": 4096,   // 必填；最小 1024；且必须 < max_tokens
  "display": "summarized"  // 可选："summarized" | "omitted"
}

// 自适应（仅 Adaptive thinking 模型，如 Opus 4.8 / Sonnet 5）
{
  "type": "adaptive",
  "display": "summarized"  // 可选
}

// 关闭
{
  "type": "disabled"
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | `"enabled"` / `"disabled"` / `"adaptive"` |
| `budget_tokens` | integer | `type=enabled` 时必填；**最小 1024，且 < `max_tokens`**；计入 `max_tokens` 上限 |
| `display` | string | `"summarized"`（Claude 4 默认）/ `"omitted"`（Claude 5+ 默认，隐藏思考正文但保留签名） |

---

## 4. `tool_choice` 取值

```jsonc
{ "type": "auto" }                      // 模型自行决定是否用工具（可选 disable_parallel_tool_use）
{ "type": "any" }                       // 强制使用某个工具（任意）
{ "type": "tool", "name": "工具名" }     // 强制使用指定工具
{ "type": "none" }                      // 禁止使用工具
```

`auto` / `any` / `tool` 均可附带 `"disable_parallel_tool_use": true` 关闭并行工具调用。

---

## 5. 本网关特有的兼容字段

网关在 `ClaudeRequest`（`dto/claude.go`）中对部分**非标准客户端字段**做了兼容转换，转发上游前会清理：

| 字段 | 来源 | 处理方式 |
|------|------|---------|
| `reasoning` | OpenRouter 风格 `{enabled, effort, max_tokens}` | 翻译成 Claude `thinking`，转发前清除 |
| `effort`（顶层） | 部分客户端简写 | 合并进 `output_config.effort`，转发前清除（Anthropic 拒绝顶层 `effort`） |
| `enable_thinking` / `thinking_budget` | OpenAI 兼容层字段 | 映射为 Claude thinking 配置 |

### 默认过滤字段（需渠道开关放行）

以下字段默认被网关过滤，需在渠道配置里显式开启才透传上游：

| 字段 | 渠道开关 |
|------|---------|
| `inference_geo` | `allow_inference_geo` |
| `speed` | `allow_speed` |
| `service_tier` | `allow_service_tier` |

> `effort` 参数默认值：Opus 4.8 在所有平台默认 `high`；Sonnet 5 在 API / Claude Code 默认 `high`。需要别的档位请显式传 `effort`。

---

## 6. 完整请求示例

**Opus 4.8（自适应思考 + 流式）：**
```json
{
  "model": "claude-opus-4-8",
  "messages": [{ "role": "user", "content": "你好" }],
  "max_tokens": 4096,
  "stream": true,
  "thinking": { "type": "adaptive" }
}
```

**Haiku 4.5（手动思考）：**
```json
{
  "model": "claude-haiku-4-5",
  "messages": [{ "role": "user", "content": "证明素数无穷" }],
  "max_tokens": 8192,
  "thinking": { "type": "enabled", "budget_tokens": 4096 }
}
```

**Sonnet 5（普通对话，不带思考）：**
```json
{
  "model": "claude-sonnet-5",
  "messages": [{ "role": "user", "content": "总结这段文字" }],
  "max_tokens": 2048
}
```
