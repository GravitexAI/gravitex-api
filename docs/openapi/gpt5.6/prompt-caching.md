# GPT-5.6 Prompt Caching 机制说明

> 调研时间：2026-07-10，来源：OpenAI 官方文档 `developers.openai.com/api/docs/guides/prompt-caching`、`/pricing`、`/guides/latest-model`。

## 1. 与老模型的区别

GPT-5.6（Sol / Terra / Luna）首次把"隐式缓存"和"显式缓存"统一成一套机制，和 GPT-5.5 及更早模型相比核心区别：

| 维度 | GPT-5.6 之前的模型 | GPT-5.6 及以后 |
|---|---|---|
| 缓存触发 | 完全自动，无字段可控 | 隐式（默认）+ 显式（手动断点）两种模式，通过 `prompt_cache_options.mode` 切换 |
| 缓存写入费用 | 免费（无额外收费） | 收费：按未缓存输入价的 **1.25×** 计费 |
| 缓存读取费用 | 按 cached-input 价（视模型 10%~50%） | 打折，见下方价格表（统一 10%） |
| 最小前缀长度 | 1024 tokens | 1024 tokens（不变） |
| 缓存存续时间 | in-memory 5-10 分钟（闲时最长 1 小时），部分模型支持 extended 24h | `prompt_cache_options.ttl` 固定 `30m`（唯一支持值，"至少存活 30 分钟"，可能更久但不保证） |
| `prompt_cache_retention` 参数 | 支持（`in_memory` / `24h`） | 已弃用，改用 `prompt_cache_options.ttl` |
| 响应用量字段 | `cached_tokens` | `cached_tokens`（读）+ 新增 `cache_write_tokens`（写） |
| `prompt_cache_key` | 可选 | 强制要求，否则匹配精度下降 |

## 2. 隐式缓存 vs 显式缓存

- **隐式（implicit，默认）**：OpenAI 自动对"最新一条消息"打一个缓存断点，无需任何额外字段。
- **显式（explicit）**：在某个 prompt 内容块（`input_text`/`input_image`/`input_file`/Chat Completions 的 `text`/`image_url`/`input_audio`/`file`/`refusal`）上手动加 `prompt_cache_breakpoint: {"mode": "explicit"}`，精确标记"这里往前的内容都要缓存"。可将 `prompt_cache_options.mode` 设为 `"explicit"`，禁用自动断点，只用手动打的点。

**价格上两者完全一样** —— 显式缓存不是另一种计价体系，只是让调用方能精确控制"缓存到哪一段"，读/写费率共用同一张价格表。区别只在命中率的可控性：隐式缓存依赖 hash 猜前缀，长 RAG/多段文档场景容易猜错；显式断点能保证某一段一定被单独视为可复用前缀。

限制：
- 每次请求最多产生 **4 个新缓存写入**；隐式模式下最新消息断点占 1 个写入槽，显式断点最多再写 3 个；纯显式模式下最多写 4 个。
- 读取时最多回溯匹配对话里最近 **50 个断点**，命中最长前缀。
- 只有 `explicit` 是 `prompt_cache_breakpoint.mode` 的合法值；打在不支持/不可缓存的块上返回 `400 invalid_request_error`。
- 老模型（GPT-5.6 之前）若收到 `prompt_cache_options`/`prompt_cache_breakpoint` 会直接报错拒绝。

## 3. 如何创建显式缓存 —— curl 示例

**Responses API**（隐式断点 + 对一个稳定文件手动加显式断点）：

```bash
curl https://api.openai.com/v1/responses \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.6",
    "prompt_cache_key": "tenant:acme:knowledge-base-v1",
    "input": [
      {
        "type": "message",
        "role": "user",
        "content": [
          {
            "type": "input_file",
            "file_id": "file_123",
            "prompt_cache_breakpoint": { "mode": "explicit" }
          },
          {
            "type": "input_text",
            "text": "Answer the current question."
          }
        ]
      }
    ]
  }'
```

**Chat Completions API**（完全禁用自动断点，只用手动断点）：

```bash
curl https://api.openai.com/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-5.6",
    "prompt_cache_key": "tenant:acme:support-assistant-v1",
    "prompt_cache_options": { "mode": "explicit" },
    "messages": [
      {
        "role": "system",
        "content": [
          {
            "type": "text",
            "text": "You are a support assistant.",
            "prompt_cache_breakpoint": { "mode": "explicit" }
          }
        ]
      },
      { "role": "user", "content": "What should I do next?" }
    ]
  }'
```

## 4. 响应参数

Chat Completions 返回示例：

```json
"usage": {
  "prompt_tokens": 2006,
  "completion_tokens": 300,
  "total_tokens": 2306,
  "prompt_tokens_details": {
    "cached_tokens": 1920,
    "cache_write_tokens": 0
  }
}
```

Responses API 对应字段是 `usage.input_tokens_details.cached_tokens` / `cache_write_tokens`。

- `cached_tokens`：本次命中缓存、按折扣价计费的 token 数。
- `cache_write_tokens`：本次新写入缓存、按 1.25× 加价计费的 token 数。

两者不会同时非零（一段内容不可能同时是"新写入"又"命中"）。

## 5. 价格（Standard 档，每 1M tokens，美元）

| 模型 | 短上下文 Input | Cached input | Cache writes | Output | 长上下文 Input | Cached input | Cache writes | Output |
|---|---|---|---|---|---|---|---|---|
| gpt-5.6-sol | $5.00 | $0.50 | $6.25 | $30.00 | $10.00 | $1.00 | $12.50 | $45.00 |
| gpt-5.6-terra | $2.50 | $0.25 | $3.125 | $15.00 | $5.00 | $0.50 | $6.25 | $22.50 |
| gpt-5.6-luna | $1.00 | $0.10 | $1.25 | $6.00 | $2.00 | $0.20 | $2.50 | $9.00 |

- Cached input 折扣统一是 input 价的 **10%**（90% off）。
- Cache writes 统一是 input 价的 **1.25×**。
- Batch/Flex 档价格是 Standard 的一半；Priority 档是 Standard 的 2 倍，规律一致。
- 2026-03-05 后发布的模型走 data residency（区域处理）端点会再加 10% uplift。

## 6. 本项目（gravitex-api 网关）支持现状

调研时的结论（详见对应 commit 的代码改动）：

- **已支持**：`prompt_cache_key`、`prompt_cache_retention`（老式字段）在 `GeneralOpenAIRequest` 里有透传字段；`cached_tokens`（读取命中）已被解析并计入 `InputTokenDetails.CachedTokens`，参与计费折扣。隐式缓存（自动缓存）本身可以正常工作。
- **未支持（本次改动前）**：
  1. 新的 `prompt_cache_options`（`mode`/`ttl`）字段未在请求结构体里声明，客户端传了会被静默丢弃，无法转发给上游。
  2. 内容块级别的 `prompt_cache_breakpoint` 字段（打在 `input_text`/`input_file`/`text` 等 block 上）同样未声明，会被丢弃。
  3. 全仓库对 `cache_write_tokens`/`CacheWriteTokens` 零匹配 —— 上游返回的缓存写入 token 数完全没有被解析和计费，会导致成本核算偏差（网关侵蚀这部分差价）。

本次改动补齐了以上三点，具体改动点：

| 改动 | 文件 |
|---|---|
| 请求透传 `prompt_cache_options`（顶层，mode/ttl） | `dto/openai_request.go`（`GeneralOpenAIRequest`） |
| 请求透传 `prompt_cache_breakpoint`（内容块级别） | `dto/openai_request.go`（Chat Completions 用 `MediaContent`，Responses API 用 `MediaInput`） |
| 响应新增 `cache_write_tokens` 字段 | `dto/openai_response.go`（`InputTokenDetails`），Chat Completions 因与上游 JSON 结构一致可自动解析，无需额外代码 |
| Responses API 用量拷贝到内部 `dto.Usage` | `relay/channel/openai/relay_responses.go`（非流式 + 流式）、`chat_via_responses.go`、`relay_image.go` |
| 计费：写入 token 并入现有"缓存创建"计价档 | `service/text_quota.go`（默认比例计费）、`service/tiered_settle.go`（`cc` 表达式变量，供 tiered_expr 计费模型使用） |
| 默认比例：读取 0.1x、写入 1.25x | `setting/ratio_setting/cache_ratio.go`（新增 `gpt-5.6-sol/terra/luna` 三个模型的 `defaultCacheRatio`/`defaultCreateCacheRatio` 条目） |

**设计取舍**：没有新增独立的"缓存写入倍率"字段/表达式变量，而是把 `cache_write_tokens` 直接并入已有的 `CachedCreationTokens`/`cc` 计价档（Claude 的 cache creation 概念）。两者语义一致——都是"本次新写入缓存、按溢价计费的 token"，复用现有 `CacheCreationRatio` 与表达式引擎的 `cc` 变量，避免了对 `pkg/billingexpr` 引擎、前端计价编辑器等大范围改动。

**已知限制 / 后续工作**：
- 本次改动只处理缓存机制本身，不包含 `gpt-5.6-sol`/`terra`/`luna` 的基础模型倍率（`ModelRatio`）录入——渠道要实际转发这三个模型，管理员仍需在后台补充模型倍率/价格配置，否则请求会报 "模型价格未配置" 错误。
- `prompt_cache_options`/`prompt_cache_breakpoint` 目前是纯透传（`json.RawMessage`），网关不校验其内容，格式错误由上游 OpenAI 返回 `400` 给客户端。
