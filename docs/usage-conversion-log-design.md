# 用量协议转换审计日志设计

> 状态：设计稿，暂不改业务代码。

## 1. 目标与现状

网关可能将客户端请求协议转换为上游协议，例如 OpenAI 格式请求转发至 Claude。此时同一次调用会出现两种用量表示：

- 上游原始 usage：Claude 的 `input_tokens`、`cache_creation_input_tokens` 等；
- 客户端最终 usage：OpenAI 的 `prompt_tokens`、`completion_tokens` 等。

现有 `other.upstream_responses.usage` 已在 Claude 响应转换前写入，因此其内容就是未经转换的上游原始 usage。新设计不重复保存这份原始数据，只在确有协议转换时原样保存“最终返回客户端的 usage”。

## 2. 字段职责

| 位置 | 内容 | 是否新增 |
| --- | --- | --- |
| `other.upstream_responses` | 上游原始响应（含原始 usage） | 否，已有 |
| `other.usage_conversion` | 转换后实际返回给客户端的完整 usage | 是 |
| 日志表现有 token、quota、费用字段 | 计费和聚合使用的业务结果 | 否，保持现状 |
| `other.request_conversion` | 请求格式转换链的展示信息 | 否，保持现状 |

刻意不在新字段中存储 `source_usage`、内部归一化 usage、计费快照或完整 `changes` 列表：它们要么已存在，要么会和日志主字段重复。

## 3. 建议的数据结构

适用于所有已接入 relay 的模型和渠道，但只在发生实际协议转换且最终响应确实带有非空的用量对象时写入。字段值是最终响应中用量对象的原样 JSON 值：OpenAI 协议取 `usage` 的值，Gemini 原生协议取 `usageMetadata` 的值，其他协议按其实际用量字段处理。

```json
{
  "prompt_tokens": 1300,
  "completion_tokens": 300,
  "total_tokens": 1600,
  "claude_cache_creation_5_m_tokens": 200,
  "claude_cache_creation_1_h_tokens": 100
}
```

不添加版本、来源/目标格式、包装层、flags 或差异说明；也不裁剪、归一化、补零或重新计算字段。不同模型的用量字段和值可以不同，均以最终响应实际序列化的 JSON 为准。因此 `other.usage_conversion` 的值与客户端响应中对应的用量对象可直接进行结构和值对照，协议来源仍可由 `other.upstream_responses` 与既有 `other.request_conversion` 判断。

以当前 Gemini 实现为例，原生响应使用 `usageMetadata`，其字段包括 `promptTokenCount`、`toolUsePromptTokenCount`、`candidatesTokenCount`、`thoughtsTokenCount`、`cachedContentTokenCount`、`totalTokenCount` 以及按模态拆分的明细数组。它们不应先映射为 OpenAI 字段再写入 `usage_conversion`；只有 Gemini 转为 OpenAI 客户端响应时，`usage_conversion` 才保存该 OpenAI 响应实际返回的 `usage`。

当前已识别的其他异构用量形态如下。该表用于实施覆盖，不用于定义固定 schema：

| 最终客户端协议或端点 | 用量对象位置或典型字段 | 处理原则 |
| --- | --- | --- |
| Anthropic Claude 原生 | `usage`；`input_tokens`、`output_tokens`、缓存读写、`cache_creation`、`server_tool_use` 等 | 原样保存 Claude usage |
| Cohere 原生 | `meta.billed_units`；`input_tokens`、`output_tokens` | 原样保存该嵌套用量对象 |
| 阿里 DashScope 原生 | `usage`；`input_tokens`、`output_tokens`、`total_tokens`，部分模型有 `image_count` | 原样保存 DashScope usage |
| Coze 原生 | `data.usage`；`token_count`、`input_count`、`output_count` | 原样保存嵌套 usage 对象 |
| 讯飞星火原生 | `payload.usage.text` | 原样保存最内层实际用量对象 |
| OpenAI Realtime | `response.usage`；`input_tokens`、`output_tokens`、`total_tokens` 及音频等 detail | 原样保存 Realtime usage |
| 图片、TTS、任务等特殊端点 | 可能为 `input_tokens`/`output_tokens`、`usage_characters`，或按模态/任务维度拆分 | 以最终端点真实返回的用量对象为准；若没有对象则不记录 |

同一基础模型经由不同客户端协议调用时，保存内容也不同。例如 Gemini 通过 OpenAI 兼容接口返回时保存 OpenAI `usage`，通过 Gemini 原生接口返回时保存 `usageMetadata`。因此实现不能根据模型名选择字段，必须以最终响应格式和实际序列化出的用量对象为准。

## 4. 写入条件

`other.usage_conversion` 必须同时满足以下条件才写入：

1. 新增 `LogUsageConversionEnabled=true`；
2. 本次确实发生了响应协议转换，例如最终上游为 Claude、客户端响应为 OpenAI；
3. 已生成最终客户端用量对象；
4. 已在响应转换路径捕获最终客户端用量对象。

以下情况不写入：

- 未发生协议转换；
- `LogUsageConversionEnabled=false`；
- 最终响应未返回用量对象，或该对象为空；
- 重试过程中的非最终响应。

因此，普通 OpenAI→OpenAI、Claude→Claude 等调用不会新增日志体积。`usage_conversion` 可独立于 `upstream_responses` 开启：前者用于保存客户端最终 usage，后者仍按原有开关保存完整上游响应。

## 5. 典型场景

### 5.1 OpenAI 请求调用 Claude 上游

若同时开启 `LogUpstreamResponsesEnabled`，原始值位于已有字段：

```json
{
  "other": {
    "upstream_responses": {
      "usage": {
        "input_tokens": 1000,
        "cache_read_input_tokens": 500,
        "cache_creation_input_tokens": 300,
        "output_tokens": 300,
        "cache_creation": {
          "ephemeral_5m_input_tokens": 200,
          "ephemeral_1h_input_tokens": 100
        }
      }
    }
  }
}
```

新增字段只保存 OpenAI 客户端实际看到的 `usage` 值：

```json
{
  "usage_conversion": {
    "prompt_tokens": 1800,
    "completion_tokens": 300,
    "total_tokens": 2100,
    "claude_cache_creation_5_m_tokens": 200,
    "claude_cache_creation_1_h_tokens": 100
  }
}
```

`input_tokens`、缓存读写量及 TTL 明细仅在开启上游响应记录时保存在 `upstream_responses.usage`；转换后的 OpenAI 字段则按响应原样保存在 `usage_conversion` 中。

### 5.2 Claude 请求调用 OpenAI 上游

开启上游响应记录时，上游 OpenAI 原始 usage 保留于 `upstream_responses.usage`；`usage_conversion` 则按独立开关原样保存转回 Claude 格式后实际发给客户端的 usage。

### 5.3 Gemini 或其他协议转换

Gemini 原生客户端响应的用量对象是 `usageMetadata`，因此保存其值：

```json
{
  "usage_conversion": {
    "promptTokenCount": 1000,
    "toolUsePromptTokenCount": 50,
    "candidatesTokenCount": 300,
    "thoughtsTokenCount": 100,
    "cachedContentTokenCount": 200,
    "totalTokenCount": 1450,
    "promptTokensDetails": [
      { "modality": "TEXT", "tokenCount": 800 },
      { "modality": "IMAGE", "tokenCount": 200 }
    ]
  }
}
```

若 Gemini 上游转换为 OpenAI 客户端协议，则保存最终 OpenAI `usage` 值，而不是上例的 `usageMetadata`。反之，OpenAI 上游转换为 Gemini 原生客户端协议，则保存最终 `usageMetadata` 值。供应商私有字段在最终响应中出现就原样存入；仅存在于上游原始响应的字段仅在开启上游响应记录时保留于 `upstream_responses.usage`。

## 6. 运行时采集与写入点

建议在 `relay/common.RelayInfo` 增加一个仅供本次请求使用的审计快照，仅包含最终客户端用量对象。不要在该快照中再放原始用量、协议名称、flags 或其他派生信息。

处理顺序：

1. 各上游 handler 接到响应后，沿用现有逻辑保存原始用量对象到 `UpstreamResponses`；
2. 响应转换器构造目标协议用量对象时，将该对象原样作为审计快照；
3. 日志生成阶段统一判断写入条件，并把快照序列化到 `other.usage_conversion`；
4. 重试或切换渠道时，仅保留最终成功响应对应的原始 usage 与审计快照。

这样避免从已序列化的响应体反解析 usage，也避免各渠道在日志层各自实现一套差异判断。

## 7. 配置设计

新增布尔配置项 `LogUsageConversionEnabled`，默认 `false`，只控制 `other.usage_conversion`：

| `LogUpstreamResponsesEnabled` | `LogUsageConversionEnabled` | 记录内容 |
| --- | --- | --- |
| false | false | 两者均不记录 |
| true | false | 仅按现有逻辑记录 `upstream_responses` |
| false | true | 仅记录符合写入条件的 `usage_conversion` |
| true | true | 两者均记录，可直接对照原始与最终 usage |

该项应沿用现有系统 Option 的读取、缓存刷新和管理接口模式；是否提供前端配置页可在实施时单独决定，不影响后端开关生效。

## 8. 兼容性与风险控制

- 不新增数据库列，继续写入日志 `other` JSON；
- 不改变 API 响应、计费、额度扣减和既有日志字段；
- 旧日志没有 `usage_conversion` 时按“该能力上线前或未发生转换”处理；
- 不给 `usage_conversion` 定义固定字段或版本号，随客户端协议用量对象的实际结构保存；
- 任何模型特有字段、嵌套对象、数值类型和空值均按最终响应原样保存；不因字段未知而丢弃、补充或改名；OpenAI 的 `usage` 与 Gemini 的 `usageMetadata` 不要求统一字段集合；
- 流式、非流式都以最终成功完成时的用量对象为准；
- `usage_conversion` 只包含最终客户端用量对象，不包含响应正文；上游完整响应仍继续遵循 `LogUpstreamResponsesEnabled` 的安全边界。

## 9. 验证用例

实施时至少覆盖：

1. OpenAI→Claude 非流式：原始 Claude usage 仅出现一次，`usage_conversion` 与客户端 OpenAI usage 完全一致；
2. OpenAI→Claude 流式：结束事件后生成相同审计字段；
3. Claude→OpenAI：原始 OpenAI usage 与 `usage_conversion` 中的 Claude 客户端 usage 可对应；
4. Gemini 等多字段映射：`usage_conversion` 与最终目标协议用量对象完全一致；
5. 无协议转换：不产生 `usage_conversion`；
6. `LogUsageConversionEnabled=false`：不产生 `usage_conversion`；
7. `LogUpstreamResponsesEnabled=false`、`LogUsageConversionEnabled=true`：仍正常写入 `usage_conversion`；
8. 重试成功：日志中只有最终成功渠道的转换记录；
9. 既有 `request_conversion`、日志 token/费用字段与接口响应保持不变。
10. Gemini 原生协议：`usage_conversion` 与最终 `usageMetadata` 的值逐字段一致，不映射为 OpenAI usage；
11. 不同模型返回不同用量字段：日志副本与各自最终客户端响应逐字段一致；无用量对象的响应不写入该字段。

## 10. 后续实施顺序

1. 在 Option 定义、初始化和缓存刷新链路中接入 `LogUsageConversionEnabled`，默认关闭；
2. 在共用 relay 上下文定义最终客户端 usage 的轻量审计快照；
3. 在现有 response converter 中填充快照；
4. 在日志 `other` 组装处按新开关和本设计写入；
5. 补充上述定向测试，并用真实 OpenAI→Claude 流式与非流式请求核对日志。
