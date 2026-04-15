# Claude Prompt Caching 使用与计费说明

## 一、什么是 Prompt Caching

Anthropic 的 Prompt Caching 允许你将大段重复内容（系统提示词、长文档、工具定义等）缓存在 Claude 服务端。后续请求如果包含相同内容，会命中缓存而非重新处理，从而**大幅降低输入成本**。

缓存涉及三种 token 类型：

| Token 类型 | 字段 | 说明 |
|---|---|---|
| 普通输入 | `input_tokens` | 未缓存、正常处理的输入 |
| 缓存写入 | `cache_creation_input_tokens` | 首次写入缓存时产生（成本高于普通输入） |
| 缓存读取 | `cache_read_input_tokens` | 命中已有缓存时产生（成本远低于普通输入） |

## 二、5 分钟缓存 vs 1 小时缓存

Anthropic 提供两档缓存 TTL（生存时间）：

| 缓存类型 | TTL | `cache_control` 参数 | 写入成本 | 适用场景 |
|---|---|---|---|---|
| **5 分钟缓存** | 5 min | `{"type": "ephemeral"}` | 输入价的 **1.25 倍** | 高频短周期对话，如实时聊天 |
| **1 小时缓存** | 1 hour | `{"type": "ephemeral", "ttl": "1h"}` | 输入价的 **2.0 倍** | 低频长周期场景，如批量分析 |

> 读取缓存的成本相同，都是输入价的 **0.1 倍**（即 10%），与 TTL 档位无关。

## 三、计费倍率详情

本网关中的倍率配置（`setting/ratio_setting/cache_ratio.go`）：

| Token 类型 | 倍率 Key | Claude 默认值 | 含义 |
|---|---|---|---|
| 缓存读取 | `cache_ratio` | **0.1** | 普通输入价格的 10% |
| 缓存写入 (5m) | `create_cache_ratio` | **1.25** | 普通输入价格的 125% |
| 缓存写入 (1h) | `create_cache_ratio × 1.6` | **2.0** | 普通输入价格的 200% |

1h 的乘数 `1.6` 来自 `relay/helper/price.go`：

```go
const claudeCacheCreation1hMultiplier = 6 / 3.75 // = 1.6
```

### 计费公式

```
quota = (
    input_tokens                                    × 1.0     (普通输入，全价)
  + cache_read_input_tokens                         × 0.1     (缓存读取，一折)
  + cache_creation_input_tokens (5m)                × 1.25    (5m 缓存写入)
  + cache_creation_input_tokens (1h)                × 2.0     (1h 缓存写入)
  + output_tokens                                   × completion_ratio
) × model_ratio × group_ratio
```

### 计费举例

假设一次请求返回的 usage：

```
input_tokens = 62
cache_read_input_tokens = 3544
cache_creation_input_tokens (5m) = 586
output_tokens = 95  (completion_ratio = 5)
```

计算：

```
= 62 × 1.0 + 3544 × 0.1 + 586 × 1.25 + 95 × 5
= 62 + 354.4 + 732.5 + 475
= 1623.9
≈ 1624 (向下取整)
```

再乘以 `model_ratio × group_ratio` 得到最终扣费额度。

## 四、如何使用

### 前提条件

- 使用 Claude Messages API 格式（`/v1/messages` 端点）
- 网关侧**不需要任何额外开关**，`cache_control` 字段会直接透传给 Anthropic

### 4.1 创建 5 分钟缓存（默认）

在 system 或 message 的 content block 上添加 `cache_control`：

```bash
curl -X POST https://your-gateway.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "你是一个专业的代码审查助手。以下是项目的完整代码规范文档（约 5000 字）...... 这里放大段不常变动的系统提示词，确保内容超过 1024 token 才会被缓存。",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "请审查以下代码片段..."
      }
    ]
  }'
```

**返回的 usage 示例（首次请求，写入缓存）：**

```json
{
  "usage": {
    "input_tokens": 50,
    "cache_creation_input_tokens": 5200,
    "cache_read_input_tokens": 0,
    "output_tokens": 300,
    "cache_creation": {
      "ephemeral_5m_input_tokens": 5200,
      "ephemeral_1h_input_tokens": 0
    }
  }
}
```

**5 分钟内再次发送相同请求（命中缓存）：**

```json
{
  "usage": {
    "input_tokens": 50,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 5200,
    "output_tokens": 280
  }
}
```

> 此时 5200 个 token 按 `cache_read` 的 0.1 倍率计费，仅需原价 10%。

### 4.2 创建 1 小时缓存

添加 `ttl` 字段指定 1 小时：

```bash
curl -X POST https://your-gateway.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "你是一个法律文档分析助手。以下是完整的合同法条款（约 20000 字）...... 这里放超长文档，适合低频但长时间内反复引用的内容。",
        "cache_control": {
          "type": "ephemeral",
          "ttl": "1h"
        }
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "请分析第三条第二款的法律风险。"
      }
    ]
  }'
```

**返回的 usage 示例（首次请求，写入 1h 缓存）：**

```json
{
  "usage": {
    "input_tokens": 45,
    "cache_creation_input_tokens": 18000,
    "cache_read_input_tokens": 0,
    "output_tokens": 500,
    "cache_creation": {
      "ephemeral_5m_input_tokens": 0,
      "ephemeral_1h_input_tokens": 18000
    }
  }
}
```

> 18000 个 token 按 1h 的 2.0 倍率计费（比 5m 的 1.25 贵 60%），但后续 1 小时内重复使用都只按 0.1 读取。

### 4.3 缓存读取（命中已有缓存）

缓存读取无需任何特殊操作。只要请求中的 `cache_control` 标记的内容和之前写入的内容**完全一致**，且在 TTL 范围内，Anthropic 就会自动命中缓存。

```bash
# 与 4.1 完全相同的请求，在 5 分钟内再次发送
curl -X POST https://your-gateway.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "你是一个专业的代码审查助手。以下是项目的完整代码规范文档（约 5000 字）...... 和之前一模一样的内容。",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "请审查另一段代码..."
      }
    ]
  }'
```

**返回的 usage：**

```json
{
  "usage": {
    "input_tokens": 55,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 5200,
    "output_tokens": 320
  }
}
```

### 4.4 混合使用：多个缓存块

可以在同一请求中对不同内容块设置不同的缓存策略：

```bash
curl -X POST https://your-gateway.com/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "你是一个全栈开发助手。基础系统指令...",
        "cache_control": {
          "type": "ephemeral",
          "ttl": "1h"
        }
      },
      {
        "type": "text",
        "text": "以下是当前项目的代码上下文（可能频繁变化）...",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {
        "role": "user",
        "content": "帮我优化这段代码。"
      }
    ]
  }'
```

> 基础系统指令用 1h 缓存（不常变），项目上下文用 5m 缓存（可能频繁更新）。

## 五、使用注意事项

### 最小 token 要求

Anthropic 要求 `cache_control` 标记的内容块至少达到 **1024 个 token** 才会实际创建缓存，否则会正常处理但不产生缓存效果。

### 缓存命中条件

- 标记 `cache_control` 的内容必须**逐字完全一致**（包括空格、换行）
- 必须在 TTL 时间窗口内
- 同一 Anthropic API Key 的请求之间共享缓存

### 缓存内容的刷新

- 每次命中缓存时，TTL 会被**刷新**（5m 缓存重新计时 5 分钟）
- 如果一直有请求命中，缓存可以持续存在

### 成本优化建议

| 场景 | 建议 |
|---|---|
| 高频对话（如客服机器人） | 使用 **5m 缓存**，持续命中会自动续期 |
| 低频批量任务（如文档分析） | 使用 **1h 缓存**，虽然写入贵 60%，但 1 小时内都能命中 |
| 超长系统提示词 + 变化的用户消息 | 对 system 设缓存，messages 不设缓存 |
| 多轮对话 | 对早期消息轮次设缓存，新消息不缓存 |

## 六、管理员配置

缓存倍率可在后台管理面板调整：

- **系统设置 → 模型倍率** 中可按模型修改 `cache_ratio`（读取）和 `create_cache_ratio`（写入 5m）
- 1h 倍率 = `create_cache_ratio × 1.6`，自动派生，不可单独配置
- 如果新增 Claude 模型未在默认倍率表中，回退值为：
  - `cache_ratio`: **1.0**（全价，需手动配为 0.1）
  - `create_cache_ratio`: **1.25**（正确，无需修改）

> **注意**：新增 Claude 模型时，务必在 `cache_ratio` 中配置 `0.1`，否则缓存读取不会享受折扣。

## 七、通过 OpenAI 兼容接口使用

如果你通过 `/v1/chat/completions`（OpenAI 格式）访问 Claude 渠道，缓存功能同样可用。网关会在转换层将 OpenAI 格式转为 Claude 原生格式，`cache_control` 字段会被保留并透传。

```bash
curl -X POST https://your-gateway.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-your-api-key" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "system",
        "content": [
          {
            "type": "text",
            "text": "你是一个专业的助手。以下是大段系统提示词...",
            "cache_control": {"type": "ephemeral"}
          }
        ]
      },
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'
```

返回的 usage 中会包含 `prompt_tokens_details.cached_tokens` 等字段来反映缓存命中情况。
