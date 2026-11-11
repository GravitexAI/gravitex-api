# GPT-6 Astra 完整使用文档（v1/chat/completions + v1/responses）

> 整理时间：2026-09-11
> 数据来源：OpenAI 官方文档站（`developers.openai.com`，所有页面均可在 URL 后加 `.md` 获取 Markdown 原文）
> 适用模型：`gpt-6-astra`（GPT-6 系列目前 API 上线的唯一正式模型）
> 发布日期：2026-09-03（见官方 [Changelog](https://developers.openai.com/api/docs/changelog)）

**本文目标**：看完这一篇就能直接接入 GPT-6 Astra，不用再翻其他文档。

---

## 目录

1. [模型概览与规格](#1-模型概览与规格)
2. [端点支持矩阵（最重要）](#2-端点支持矩阵最重要)
3. [定价](#3-定价)
4. [快速开始：两个端点的最小可用调用](#4-快速开始两个端点的最小可用调用)
5. [`/v1/responses` 完整参数参考](#5-v1responses-完整参数参考)
6. [`/v1/chat/completions` 完整参数参考](#6-v1chatcompletions-完整参数参考)
7. [GPT-6 Astra 专属限制（踩坑必读）](#7-gpt-6-astra-专属限制踩坑必读)
8. [reasoning 推理控制详解](#8-reasoning-推理控制详解)
9. [工具调用（Function Calling / 内置工具）](#9-工具调用function-calling--内置工具)
10. [结构化输出（Structured Outputs）](#10-结构化输出structured-outputs)
11. [流式响应](#11-流式响应)
12. [Prompt Caching（命中率与计费）](#12-prompt-caching命中率与计费)
13. [GPT-6 Astra 三个新能力](#13-gpt-6-astra-三个新能力)
14. [多轮会话状态管理](#14-多轮会话状态管理)
15. [错误说明大全（含你遇到的报错）](#15-错误说明大全含你遇到的报错)
16. [速率限制](#16-速率限制)
17. [从 GPT-5.6 / GPT-5.5 迁移清单](#17-从-gpt-56--gpt-55-迁移清单)
18. [提示词最佳实践（官方原文）](#18-提示词最佳实践官方原文)
19. [官方文档链接汇总](#19-官方文档链接汇总)

---

## 1. 模型概览与规格

**官方文档**：https://developers.openai.com/api/docs/models/gpt-6-astra

| 项目 | 值 |
|---|---|
| Model ID | `gpt-6-astra` |
| 默认快照 | `gpt-6-astra`（目前仅此一个快照/别名，无日期后缀版本） |
| 输入模态 | text、image |
| 输出模态 | text（**不支持 audio 输出**） |
| 上下文窗口 | 1,050,000 tokens |
| 最大输入 tokens | 922,000 |
| 最大输出 tokens | 128,000 |
| 知识截止 | 2026-04-30 |
| 推理 token | 支持 |
| `reasoning.effort` 可选值 | `low` / `medium` / `high` / `xhigh` / `max`（**不支持 `none`**） |

**支持的特性（Supported features）**：
`streaming`、`structured_outputs`、`function_calling`、`file_search`、`image_input`、`web_search`、`prompt_caching`

**Responses API 下支持的内置工具（Supported tools）**：
`web_search`、`file_search`、`image_generation`、`code_interpreter`、`hosted_shell`、`apply_patch`、`skills`、`computer_use`、`mcp`、`tool_search`

**官方定位原文**：
> GPT-6 Astra is our most capable model, built for the hardest end-to-end work. Use it for complex reasoning, coding, computer use, research, and document creation.

---

## 2. 端点支持矩阵（最重要）

| Endpoint | Route | GPT-6 Astra 是否支持 |
|---|---|---|
| Chat Completions | `v1/chat/completions` | ✅ **支持**（但**不支持工具调用**，见下） |
| Responses | `v1/responses` | ✅ **支持（推荐）** |
| Batch | `v1/batch` | ✅ 支持 |
| Live | `v1/live/sessions` | ❌ |
| Realtime | `v1/realtime` | ❌ |
| Realtime translation | `v1/realtime/translations` | ❌ |
| Realtime transcription | `v1/realtime/transcription_sessions` | ❌ |
| Assistants | `v1/assistants` | ❌（Assistants API 已于 2026-08-26 下线） |
| Fine-tuning | `v1/fine-tuning` | ❌ |
| Embeddings | `v1/embeddings` | ❌ |
| Image generation | `v1/images/generations` | ❌ |
| Image edit | `v1/images/edits` | ❌ |
| Videos | `v1/videos` | ❌ |
| Speech generation | `v1/audio/speech` | ❌ |
| Transcription | `v1/audio/transcriptions` | ❌ |
| Translation | `v1/audio/translations` | ❌ |
| Moderation | `v1/moderations` | ❌ |
| Completions (legacy) | `v1/completions` | ❌ |

### ⚠️ 两个端点的关键差异

| 能力 | `/v1/chat/completions` | `/v1/responses` |
|---|---|---|
| 基础文本生成 | ✅ | ✅ |
| 图片输入 | ✅ | ✅ |
| 流式 | ✅ | ✅ |
| 结构化输出 | ✅（`response_format`） | ✅（`text.format`） |
| **Function Calling / 工具调用** | ❌ **不支持** | ✅ 支持 |
| 内置工具（web_search / code_interpreter 等） | ❌ | ✅ |
| 推理链跨轮保留 | ❌ | ✅（`previous_response_id` / `reasoning.encrypted_content`） |
| 后台任务（background） | ❌ | ✅ |
| WebSocket 模式 / 中途转向 | ❌ | ✅ |
| `configuration_update` 中途改推理强度 | ❌ | ✅ |
| 会话状态托管 | ❌ 手动管理 | ✅ `conversation` / `previous_response_id` |

> **官方原文（Changelog 2026-09-03）**：
> “Tool calling requires the Responses API. If you use tools with Chat Completions, follow the Responses migration guide.”
>
> **官方原文（reasoning 指南）**：
> “Use the Responses API for function calling. Chat Completions does not support function calling with GPT-6 Astra.”

**结论**：新项目直接上 `/v1/responses`。只有纯文本对话、且不需要工具时才用 `/v1/chat/completions`。

---

## 3. 定价

**官方文档**：https://developers.openai.com/api/docs/pricing

单位：美元 / 1M tokens。GPT-6 Astra 按**短上下文 / 长上下文**分档（分界线 272K 输入 tokens）。

### Standard（标准）

| 档位 | Input | Cached input | Cache writes | Output |
|---|---|---|---|---|
| 短上下文（≤272K） | $10.00 | $1.00 | $12.50 | $50.00 |
| 长上下文（>272K） | $20.00 | $2.00 | $25.00 | $75.00 |

### Batch（批处理，50% 折扣）

| 档位 | Input | Cached input | Cache writes | Output |
|---|---|---|---|---|
| 短上下文 | $5.00 | $0.50 | $6.25 | $25.00 |
| 长上下文 | $10.00 | $1.00 | $12.50 | $37.50 |

### Flex（弹性处理，50% 折扣，同 Batch 价）

| 档位 | Input | Cached input | Cache writes | Output |
|---|---|---|---|---|
| 短上下文 | $5.00 | $0.50 | $6.25 | $25.00 |
| 长上下文 | $10.00 | $1.00 | $12.50 | $37.50 |

### Fast mode（快速模式，2x 价格）

| 档位 | Input | Cached input | Cache writes | Output |
|---|---|---|---|---|
| 短上下文 | $20.00 | $2.00 | $25.00 | $100.00 |
| 长上下文 | $40.00 | $4.00 | $50.00 | $150.00 |

### 计费规则要点（官方原文翻译）

- 输入 tokens **超过 272K** 时，**整个请求**（不是超出部分）按 2x input/cache 费率、1.5x output 费率计费。
- Cache writes 按未缓存 input 费率的 **1.25x** 计费。
- Batch 和 Flex 是 Standard 的 **50%**；Fast mode 是对应档位的 **2x**。
- **推理 tokens（reasoning tokens）不可见，但占用上下文窗口，并按 output tokens 计费。**
- Pro 模式（`reasoning.mode: "pro"`）把所有模型工作聚合计费，按所选模型的标准 token 费率结算，用量和成本高于 standard 模式。

---

## 4. 快速开始：两个端点的最小可用调用

### 4.1 `/v1/responses`（推荐）

**cURL**
```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "input": "用一句话解释什么是向量数据库",
    "reasoning": { "effort": "medium" },
    "max_output_tokens": 2048
  }'
```

**Python**
```python
from openai import OpenAI

client = OpenAI()

response = client.responses.create(
    model="gpt-6-astra",
    input="用一句话解释什么是向量数据库",
    reasoning={"effort": "medium"},
    max_output_tokens=2048,
)
print(response.output_text)
```

**Node.js**
```javascript
import OpenAI from "openai";

const client = new OpenAI();

const response = await client.responses.create({
  model: "gpt-6-astra",
  input: "用一句话解释什么是向量数据库",
  reasoning: { effort: "medium" },
  max_output_tokens: 2048,
});
console.log(response.output_text);
```

**Go**
```go
package main

import (
	"context"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	client := openai.NewClient()
	resp, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
		Model:     "gpt-6-astra",
		Reasoning: shared.ReasoningParam{Effort: shared.ReasoningEffortMedium},
		Input:     responses.ResponseNewParamsInputUnion{OfString: openai.String("用一句话解释什么是向量数据库")},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(resp.OutputText())
}
```

**多模态（文本 + 图片）输入**
```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "input": [
      {
        "role": "user",
        "content": [
          { "type": "input_text", "text": "这张图里有什么？" },
          { "type": "input_image", "image_url": "https://example.com/a.png", "detail": "auto" }
        ]
      }
    ]
  }'
```

### 4.2 `/v1/chat/completions`

**cURL**
```bash
curl https://api.openai.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "messages": [
      { "role": "developer", "content": "你是一个简洁的技术助手。" },
      { "role": "user", "content": "用一句话解释什么是向量数据库" }
    ],
    "reasoning_effort": "medium",
    "max_completion_tokens": 2048
  }'
```

**Python**
```python
completion = client.chat.completions.create(
    model="gpt-6-astra",
    messages=[
        {"role": "developer", "content": "你是一个简洁的技术助手。"},
        {"role": "user", "content": "用一句话解释什么是向量数据库"},
    ],
    reasoning_effort="medium",
    max_completion_tokens=2048,
)
print(completion.choices[0].message.content)
```

> ⚠️ Chat Completions 用 `max_completion_tokens`（**不要用已废弃的 `max_tokens`**，推理模型不兼容），
> Responses 用 `max_output_tokens`。

---

## 5. `/v1/responses` 完整参数参考

**官方参考**：https://developers.openai.com/api/reference/resources/responses/methods/create

### 5.1 顶层 Body 参数总表

| 参数 | 类型 | GPT-6 Astra | 说明 |
|---|---|---|---|
| `model` | string | **必填**（SDK 必填） | `"gpt-6-astra"` |
| `input` | string \| array | 常用必填 | 文本/图片/文件输入，或 Item 数组 |
| `instructions` | string \| null | ✅ | 插入模型上下文的 system/developer 消息。**与 `previous_response_id` 同用时不会从上一轮继承**，便于换 system prompt |
| `max_output_tokens` | number \| null | ✅ | 生成上限（含可见输出 + 推理 tokens）。**最小值 16**，见 §15.2 |
| `max_tool_calls` | number \| null | ✅ | 内置工具调用总次数上限（跨所有内置工具，非单工具）。超出后模型的后续工具调用被忽略 |
| `reasoning` | object \| null | ✅ | 推理配置，详见 §8 |
| `text` | object | ✅ | `{ format, verbosity }`，详见 §10 |
| `tools` | array | ✅（仅 Responses） | 函数工具 + 内置工具，详见 §9 |
| `tool_choice` | string \| object | ✅ | `none`/`auto`/`required`/指定工具/`allowed_tools` |
| `parallel_tool_calls` | boolean \| null | ✅ | 是否允许并行工具调用 |
| `stream` | boolean \| null | ✅ | SSE 流式 |
| `stream_options` | object \| null | ✅ | `{ include_obfuscation }` |
| `background` | boolean \| null | ✅ | 后台异步运行长任务 |
| `store` | boolean \| null | ✅ | 是否存储响应，**省略时默认 true**，存储至少 30 天 |
| `previous_response_id` | string \| null | ✅ | 多轮串联。**不能与 `conversation` 同时用** |
| `conversation` | string \| object \| null | ✅ | 关联 Conversations API 会话 |
| `include` | array \| null | ✅ | 追加返回字段，见下 |
| `prompt` | object \| null | ✅ | `{ id, variables, version }` 引用 Prompt 模板 |
| `prompt_cache_key` | string \| null | ✅ | 缓存分桶键（替代 `user`） |
| `prompt_cache_options` | object | ✅ | `{ mode, ttl, comparison_response_id }`，见 §12 |
| `prompt_cache_retention` | `"in_memory"`\|`"24h"` | ⚠️ 已废弃 | 改用 `prompt_cache_options.ttl` |
| `context_management` | array \| null | ✅ | `[{ type: "compaction", compact_threshold }]` 上下文压缩 |
| `truncation` | `"auto"`\|`"disabled"` | ✅ | `disabled`（默认）超上下文直接 400；`auto` 自动丢弃开头 |
| `service_tier` | enum \| null | ⚠️ 部分 | `auto`/`default`/`flex`/`scale`/`priority`/`fast`/`ultrafast`，见 §7 |
| `metadata` | object \| null | ✅ | 最多 16 个 KV，key≤64 字符，value≤512 字符 |
| `moderation` | object \| null | ✅ | `{ model, policy:{ input:{mode}, output:{mode} } }`，mode = `score`/`block` |
| `safety_identifier` | string \| null | ✅ | 稳定的终端用户标识（建议哈希），≤64 字符 |
| `user` | string | ⚠️ 将被替代 | 用 `safety_identifier` + `prompt_cache_key` 代替 |
| `temperature` | number \| null | ❌ **不支持** | GPT-6 Astra 必须移除 |
| `top_p` | number \| null | ❌ **不支持** | GPT-6 Astra 必须移除 |
| `top_logprobs` | number \| null | ❌ **不支持** | GPT-6 Astra 必须移除 |

### 5.2 `include` 可选值

```
"file_search_call.results"              — 文件检索结果
"web_search_call.results"               — 网页检索结果
"web_search_call.action.sources"        — 网页检索来源
"message.input_image.image_url"         — 输入图片 URL
"computer_call_output.output.image_url" — computer use 输出图片 URL
"code_interpreter_call.outputs"         — 代码解释器执行输出
"reasoning.encrypted_content"           — 加密推理内容（无状态/ZDR 多轮必需）
"message.output_text.logprobs"          — logprobs（GPT-6 Astra 不支持，必须移除）
```

### 5.3 响应对象（Response）主要字段

```jsonc
{
  "id": "resp_...",
  "object": "response",
  "created_at": 1788000000,
  "completed_at": 1788000012,
  "status": "completed",              // completed | incomplete | failed | in_progress | queued | cancelled
  "model": "gpt-6-astra",
  "output": [ /* array of ResponseOutputItem：message / reasoning / function_call / 各类工具调用 */ ],
  "output_text": "...",               // SDK 便捷字段，拼接所有 output_text
  "error": null,                      // ResponseError | null
  "incomplete_details": { "reason": "max_output_tokens" },  // status=incomplete 时
  "instructions": "...",
  "previous_response_id": null,
  "conversation": null,
  "max_output_tokens": 2048,
  "max_tool_calls": null,
  "parallel_tool_calls": true,
  "tools": [],
  "tool_choice": "auto",
  "text": { "format": { "type": "text" }, "verbosity": "medium" },
  "reasoning": {
    "context": "auto",
    "effort": "medium",
    "mode": "standard",
    "summary": "auto"
  },
  "truncation": "disabled",
  "service_tier": "default",
  "store": true,
  "metadata": {},
  "prompt_cache_key": "...",
  "prompt_cache_options": { "mode": "implicit", "ttl": "30m" },
  "prompt_cache_diagnostics": { "type": "cache_miss", "comparison_reusable_tokens": 0 },
  "safety_identifier": "...",
  "temperature": null,
  "top_p": null,
  "top_logprobs": null,
  "usage": {
    "input_tokens": 1200,
    "input_tokens_details": {
      "cached_tokens": 1024,
      "cache_write_tokens": 0
    },
    "output_tokens": 1186,
    "output_tokens_details": {
      "reasoning_tokens": 1024
    },
    "total_tokens": 2386
  }
}
```

**计费关键字段**：
- `usage.input_tokens_details.cached_tokens` → 走缓存输入价（$1.00/M）
- `usage.input_tokens_details.cache_write_tokens` → 走缓存写入价（$12.50/M）
- `usage.output_tokens_details.reasoning_tokens` → 已包含在 `output_tokens` 里，按输出价计费

---

## 6. `/v1/chat/completions` 完整参数参考

**官方参考**：https://developers.openai.com/api/reference/resources/chat

### 6.1 Body 参数总表

| 参数 | 类型 | GPT-6 Astra | 说明 |
|---|---|---|---|
| `messages` | array | **必填** | 消息数组，role: `developer`/`system`/`user`/`assistant`/`tool`/`function` |
| `model` | string | **必填** | `"gpt-6-astra"` |
| `max_completion_tokens` | number \| null | ✅ **推荐** | 生成上限（含可见输出 + 推理 tokens） |
| `max_tokens` | number \| null | ⚠️ **已废弃** | 与推理模型不兼容，改用 `max_completion_tokens` |
| `reasoning_effort` | enum \| null | ✅ | `low`/`medium`/`high`/`xhigh`/`max`（**`none`、`minimal` 对 Astra 无效**） |
| `verbosity` | `low`\|`medium`\|`high` | ✅ | 输出详略，默认 `medium` |
| `response_format` | object | ✅ | `text` / `json_schema` / `json_object`，见 §10 |
| `stream` | boolean \| null | ✅ | SSE 流式 |
| `stream_options` | object \| null | ✅ | `{ include_usage, include_obfuscation }` |
| `store` | boolean \| null | ✅ | 是否存储，用于蒸馏/评估 |
| `n` | number \| null | ✅ | 生成候选数，建议保持 1 控成本 |
| `stop` | string \| array \| null | ⚠️ | 最多 4 个停止序列；新推理模型普遍不支持 |
| `seed` | number \| null | Beta | 尽力确定性采样，配合 `system_fingerprint` 监控 |
| `metadata` | object \| null | ✅ | 最多 16 个 KV |
| `moderation` | object \| null | ✅ | 同 Responses |
| `prompt_cache_key` | string \| null | ✅ | 缓存分桶键 |
| `prompt_cache_options` | object | ✅ | `{ mode, ttl }` |
| `prompt_cache_retention` | enum \| null | ⚠️ 已废弃 | 改用 `prompt_cache_options.ttl` |
| `safety_identifier` | string \| null | ✅ | 终端用户标识 |
| `service_tier` | enum \| null | ⚠️ 部分 | `auto`/`default`/`flex`/`scale`/`priority`/`fast` |
| `prediction` | object \| null | ✅ | Predicted Outputs（重写文件等场景加速） |
| `parallel_tool_calls` | boolean | ➖ | Astra 在 CC 上不支持工具，无意义 |
| `tools` / `tool_choice` | array / enum | ❌ **不支持** | Astra 工具调用必须走 Responses |
| `functions` / `function_call` | — | ❌ 已废弃 | 旧接口，且 Astra 不支持 |
| `web_search_options` | object | ❌ | Astra 的 web_search 走 Responses 内置工具 |
| `modalities` | array | ❌ | Astra 输出仅 text |
| `audio` | object \| null | ❌ | Astra 不支持音频输出 |
| `temperature` | number \| null | ❌ **不支持** | 必须移除 |
| `top_p` | number \| null | ❌ **不支持** | 必须移除 |
| `logprobs` | boolean \| null | ❌ **不支持** | 必须移除 |
| `top_logprobs` | number \| null | ❌ **不支持** | 必须移除 |
| `logit_bias` | map \| null | ⚠️ | 推理模型上基本无效 |
| `frequency_penalty` / `presence_penalty` | number | ⚠️ | 推理模型上不建议使用 |
| `user` | string | ⚠️ 将被替代 | 改用 `safety_identifier` + `prompt_cache_key` |

### 6.2 消息角色说明

| role | 说明 |
|---|---|
| `developer` | **推荐**。o1 及更新模型用 `developer` 替代 `system`，优先级最高 |
| `system` | 旧写法，仍兼容 |
| `user` | 用户消息，支持 text / image_url / input_audio / file 内容块 |
| `assistant` | 助手历史消息 |
| `tool` | 工具执行结果，需带 `tool_call_id` |
| `function` | 已废弃 |

### 6.3 响应对象（ChatCompletion）

```jsonc
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "created": 1788000000,
  "model": "gpt-6-astra",
  "system_fingerprint": "fp_...",
  "service_tier": "default",
  "choices": [
    {
      "index": 0,
      "finish_reason": "stop",     // stop | length | content_filter | tool_calls | function_call
      "message": {
        "role": "assistant",
        "content": "...",
        "refusal": null,
        "annotations": []
      },
      "logprobs": null
    }
  ],
  "usage": {
    "prompt_tokens": 1200,
    "completion_tokens": 1186,
    "total_tokens": 2386,
    "prompt_tokens_details": {
      "text_tokens": 1200,
      "image_tokens": 0,
      "audio_tokens": 0,
      "cached_tokens": 1024,
      "cache_write_tokens": 0
    },
    "completion_tokens_details": {
      "text_tokens": 162,
      "reasoning_tokens": 1024,
      "audio_tokens": 0,
      "accepted_prediction_tokens": 0,
      "rejected_prediction_tokens": 0
    }
  }
}
```

---

## 7. GPT-6 Astra 专属限制（踩坑必读）

来源：官方 [Using GPT-6 Astra](https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra) + [Changelog](https://developers.openai.com/api/docs/changelog)

| # | 限制 | 后果 | 处理方式 |
|---|---|---|---|
| 1 | **不支持 `reasoning.effort: "none"`** | HTTP 400 | 最低用 `low` |
| 2 | **不支持 `temperature`** | HTTP 400 `Unsupported parameter` | 从请求体中删除 |
| 3 | **不支持 `top_p`** | HTTP 400 | 删除 |
| 4 | **不支持 `top_logprobs`** | HTTP 400 | 删除 |
| 5 | **Chat Completions 下不支持 `logprobs`** | HTTP 400 | 删除 |
| 6 | **Responses 下不支持 `include: ["message.output_text.logprobs"]`** | HTTP 400 | 从 `include` 中移除 |
| 7 | **Chat Completions 不支持工具调用** | HTTP 400 | 改用 `/v1/responses` |
| 8 | **EU 数据驻留下不支持 Fast mode** | HTTP 400 | 改用 Standard 处理 |
| 9 | **EU 数据驻留下 `service_tier` 不支持 `fast` / `priority`** | HTTP 400 | 用 `default` / `auto` |
| 10 | Fast mode 对 Astra **不提供延迟 SLA** | — | 不要按 SLA 做容量规划 |
| 11 | `configuration_update` 仅支持 **standard 单 Agent 模式** | 不支持的 effort 值会 400 | Pro 模式/多 Agent 下不要用 |
| 12 | 开启了 **Misalignment monitoring** | 异步监控可能触发安全告警或中断会话 | 见 [misalignment-monitoring](https://developers.openai.com/api/docs/guides/safety-checks/misalignment-monitoring) |

### 网关/中转侧透传建议

如果你在做 API 网关（例如本项目 gravitex-api），针对 `gpt-6-astra` 需要注意：

- 不要给请求**默认注入** `temperature` / `top_p`，否则所有 Astra 请求都会 400。
- 若上游客户端习惯传 `max_tokens`，在 Chat Completions 下应映射为 `max_completion_tokens`；在 Responses 下映射为 `max_output_tokens`。
- `max_output_tokens` / `max_completion_tokens` **不要默认填 1 或很小的数**，最小 16（见 §15.2）。
- Chat Completions 请求里带 `tools` 且 model 是 `gpt-6-astra` 时，建议网关层给出明确提示，或自动改走 Responses。

---

## 8. reasoning 推理控制详解

**官方文档**：https://developers.openai.com/api/docs/guides/reasoning

### 8.1 `reasoning` 对象字段（Responses）

| 字段 | 类型 | 说明 |
|---|---|---|
| `effort` | `none`/`minimal`/`low`/`medium`/`high`/`xhigh`/`max` | 推理强度。**Astra 不支持 `none`** |
| `mode` | `standard` \| `pro` | 执行模式，默认 `standard` |
| `context` | `auto` / `current_turn` / `all_turns` | 后续轮次回灌哪些推理项。省略或 `auto` 由模型决定；GPT-5.6 系默认 `all_turns`，更早模型默认 `current_turn` |
| `summary` | `auto` / `concise` / `detailed` | 推理摘要粒度 |
| `generate_summary` | — | **已废弃**，用 `summary` |

Chat Completions 只有扁平的 `reasoning_effort`，没有 `mode` / `context` / `summary`。

### 8.2 effort 取值该怎么选（官方建议表）

| Effort | 适用场景 |
|---|---|
| `none` | 延迟敏感、完全不需要推理的任务（语音、快速检索、分类）。**Astra 不可用** |
| `low` | 需要工具调用、规划、搜索、多步决策，但要兼顾速度和成本。典型：数据分析、起草、执行型编码、客服/聊天助手 |
| `medium` | 质量和可靠性重要、涉及规划与复杂推理判断。**多数工作负载的默认选择**，延迟/性能/成本的平衡点。典型：Agent 编码、研究、表格幻灯片处理、长周期任务委派 |
| `high` | 硬核推理、复杂调试、深度规划、高价值任务。典型：Agent 编码、长周期研究、知识工作。建议同时评估 `medium` 和 `high` |
| `xhigh` | 深度研究、异步工作流、需要长时间运行的 Agent 任务。**只在评估显示收益明显值回延迟和成本时才用**。典型：安全与代码审查、企业生产力、更深的研究任务 |
| `max` | 最复杂任务的最大推理。当前在用 `xhigh` 的可以评估 `max` 是否更强 |

> 延迟敏感场景想更快出首 token：可以提示模型先生成一小段前言（preamble），再进入深度推理。

### 8.3 推理 tokens 与成本控制

- 推理 tokens **不可见**，但**占上下文窗口**，且**按 output token 计费**。
- 用 `max_output_tokens`（Responses）/ `max_completion_tokens`（CC）封顶总生成量。
- **官方建议**：刚开始实验时，至少为推理 + 输出预留 **25,000 tokens**。
- 若生成量触顶，返回 `status: "incomplete"` + `incomplete_details.reason: "max_output_tokens"`。
  **此时可能一个可见 token 都没有，但 input + reasoning tokens 照样计费。**

**处理 incomplete 的标准写法**：
```javascript
const response = await openai.responses.create({
  model: "gpt-6-astra",
  reasoning: { effort: "medium" },
  input: [{ role: "user", content: prompt }],
  max_output_tokens: 300,
});

if (
  response.status === "incomplete" &&
  response.incomplete_details.reason === "max_output_tokens"
) {
  console.log("Ran out of tokens");
}
```

### 8.4 Pro 模式

```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "reasoning": { "mode": "pro", "effort": "medium" },
    "input": "Review this database migration plan and identify potential failure modes."
  }'
```

`mode` 与 `effort` **相互独立**：mode 选执行模式，effort 控制该模式内的推理量。

---

## 9. 工具调用（Function Calling / 内置工具）

**官方文档**：
- Function calling：https://developers.openai.com/api/docs/guides/function-calling
- Using tools：https://developers.openai.com/api/docs/guides/tools

> ⚠️ **GPT-6 Astra 的工具调用只能走 `/v1/responses`。**

### 9.1 Responses `tools` 支持的类型

| 类型 | 说明 | 官方文档 |
|---|---|---|
| `function` | 自定义函数工具 | [function-calling](https://developers.openai.com/api/docs/guides/function-calling) |
| `custom` | 自定义格式工具（支持 lark/regex 语法约束） | [function-calling#custom-tools](https://developers.openai.com/api/docs/guides/function-calling#custom-tools) |
| `web_search` | 网页搜索 | [tools-web-search](https://developers.openai.com/api/docs/guides/tools-web-search) |
| `file_search` | 向量库文件检索 | [tools-file-search](https://developers.openai.com/api/docs/guides/tools-file-search) |
| `code_interpreter` | 运行 Python | [tools-code-interpreter](https://developers.openai.com/api/docs/guides/tools-code-interpreter) |
| `image_generation` | 生成/编辑图片 | [tools-image-generation](https://developers.openai.com/api/docs/guides/tools-image-generation) |
| `computer_use` / `computer_use_preview` | 操作浏览器和桌面 | [tools-computer-use](https://developers.openai.com/api/docs/guides/tools-computer-use) |
| `shell` / `local_shell` | 托管容器或本地 shell | [tools-shell](https://developers.openai.com/api/docs/guides/tools-shell) |
| `apply_patch` | 让模型产出结构化 diff | [tools-apply-patch](https://developers.openai.com/api/docs/guides/tools-apply-patch) |
| `skills` | 可复用技能包 | [tools-skills](https://developers.openai.com/api/docs/guides/tools-skills) |
| `mcp` | 远程 MCP 服务器 / 连接器 | [tools-connectors-mcp](https://developers.openai.com/api/docs/guides/tools-connectors-mcp) |
| `tool_search` | 大工具集延迟加载 | [tools-tool-search](https://developers.openai.com/api/docs/guides/tools-tool-search) |
| `programmatic_tool_calling` | 程序化工具调用 | [tools-programmatic-tool-calling](https://developers.openai.com/api/docs/guides/tools-programmatic-tool-calling) |
| `namespace` | 工具分组命名空间 | [tools-tool-search](https://developers.openai.com/api/docs/guides/tools-tool-search) |

### 9.2 函数工具调用示例

```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6-astra",
    "input": "北京今天天气怎么样？",
    "tools": [
      {
        "type": "function",
        "name": "get_weather",
        "description": "查询指定城市的当前天气",
        "parameters": {
          "type": "object",
          "properties": {
            "city": { "type": "string", "description": "城市名称" }
          },
          "required": ["city"],
          "additionalProperties": false
        },
        "strict": true
      }
    ],
    "tool_choice": "auto"
  }'
```

**返回中的工具调用项**：
```jsonc
{
  "output": [
    { "type": "reasoning", "id": "rs_...", "summary": [] },
    {
      "type": "function_call",
      "id": "fc_...",
      "call_id": "call_abc123",
      "name": "get_weather",
      "arguments": "{\"city\":\"北京\"}",
      "status": "completed"
    }
  ]
}
```

**回传工具结果**（必须带同一个 `call_id`）：
```jsonc
{
  "model": "gpt-6-astra",
  "previous_response_id": "resp_...",
  "input": [
    {
      "type": "function_call_output",
      "call_id": "call_abc123",
      "output": "{\"temp_c\":22,\"condition\":\"晴\"}"
    }
  ],
  "tools": [ /* 同上，工具定义要一起带上 */ ]
}
```

### 9.3 `tool_choice` 取值

| 值 | 说明 |
|---|---|
| `"none"` | 不调用任何工具，直接生成消息（无 tools 时的默认） |
| `"auto"` | 模型自行决定（有 tools 时的默认） |
| `"required"` | 必须调用至少一个工具 |
| `{"type":"function","name":"xxx"}` | 强制调用指定函数 |
| `{"type":"allowed_tools","mode":"auto"\|"required","tools":[...]}` | 限定候选工具集 |
| `{"type":"web_search"}` 等 | 强制使用某个内置工具 |

---

## 10. 结构化输出（Structured Outputs）

**官方文档**：https://developers.openai.com/api/docs/guides/structured-outputs

> ⚠️ **两个端点写法不同，迁移时最容易踩的坑之一。**

### Responses：用 `text.format`

```json
{
  "model": "gpt-6-astra",
  "input": "提取这段话里的人名和公司：张三入职了字节跳动。",
  "text": {
    "format": {
      "type": "json_schema",
      "name": "entity_extraction",
      "description": "抽取实体",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "person": { "type": "string" },
          "company": { "type": "string" }
        },
        "required": ["person", "company"],
        "additionalProperties": false
      }
    },
    "verbosity": "low"
  }
}
```

### Chat Completions：用 `response_format`

```json
{
  "model": "gpt-6-astra",
  "messages": [{ "role": "user", "content": "提取这段话里的人名和公司：张三入职了字节跳动。" }],
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "name": "entity_extraction",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "person": { "type": "string" },
          "company": { "type": "string" }
        },
        "required": ["person", "company"],
        "additionalProperties": false
      }
    }
  }
}
```

**三种 format 类型**：

| type | 说明 |
|---|---|
| `text` | 默认，纯文本 |
| `json_schema` | **推荐**，严格遵循 JSON Schema |
| `json_object` | 旧版 JSON mode，只保证合法 JSON。**需在提示词中明确要求输出 JSON**，否则模型不会产出 JSON |

`strict: true` 时只支持 JSON Schema 的子集（必须 `additionalProperties: false`、所有字段进 `required` 等）。

### `verbosity`

`low` / `medium`（默认）/ `high`，控制回答详略。
Responses 放在 `text.verbosity`，Chat Completions 是顶层 `verbosity`。

---

## 11. 流式响应

**官方文档**：https://developers.openai.com/api/docs/guides/streaming-responses

设置 `"stream": true` 即可，SSE 格式。

### Responses 流式事件类型（完整列表）

```
response.created
response.in_progress
response.queued
response.completed
response.failed
response.incomplete

response.output_item.added
response.output_item.done
response.content_part.added
response.content_part.done

response.output_text.delta
response.output_text.done
response.output_text.annotation.added
response.refusal.delta
response.refusal.done

response.reasoning_text.delta
response.reasoning_text.done
response.reasoning_summary_part.added
response.reasoning_summary_part.done
response.reasoning_summary_text.delta
response.reasoning_summary_text.done

response.function_call_arguments.delta
response.function_call_arguments.done
response.custom_tool_call_input.delta
response.custom_tool_call_input.done

response.web_search_call.in_progress
response.web_search_call.searching
response.web_search_call.completed

response.file_search_call.in_progress
response.file_search_call.searching
response.file_search_call.completed

response.code_interpreter_call.in_progress
response.code_interpreter_call.interpreting
response.code_interpreter_call.completed
response.code_interpreter_call_code.delta
response.code_interpreter_call_code.done

response.image_generation_call.in_progress
response.image_generation_call.generating
response.image_generation_call.partial_image
response.image_generation_call.completed

response.mcp_call.in_progress
response.mcp_call.completed
response.mcp_call.failed
response.mcp_call_arguments.delta
response.mcp_call_arguments.done
response.mcp_list_tools.in_progress
response.mcp_list_tools.completed
response.mcp_list_tools.failed

response.shell_call_command.added
response.shell_call_command.delta
response.shell_call_command.done
response.shell_call_output_content.delta
response.shell_call_output_content.done

response.audio.delta / .done                     (Astra 不产生)
response.audio.transcript.delta / .done          (Astra 不产生)
```

**官方参考**：https://developers.openai.com/api/reference/resources/responses/streaming-events

### Chat Completions 流式

分块结构为 `chat.completion.chunk`，通过 `choices[0].delta` 增量返回，以 `data: [DONE]` 结束。

`stream_options` 两个选项：

| 字段 | 说明 |
|---|---|
| `include_usage` | 在 `[DONE]` 前多发一个只含 `usage` 的 chunk（`choices` 为空数组）。**流被中断时可能收不到，计费统计要做兜底** |
| `include_obfuscation` | 默认开启，在 delta 事件上加随机 `obfuscation` 字段做旁路攻击缓解。若信任链路，可设 `false` 省带宽 |

**Chat Completions 参考**：https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events

---

## 12. Prompt Caching（命中率与计费）

**官方文档**：https://developers.openai.com/api/docs/guides/prompt-caching

### `prompt_cache_options`

| 字段 | 说明 |
|---|---|
| `mode` | `implicit`（默认）/ `explicit`。`implicit`：自动创建 1 个隐式断点 + 写入最近 3 个显式断点；`explicit`：不创建隐式断点，写入最近 4 个显式断点（若无显式断点则完全不走缓存） |
| `ttl` | 最小缓存生命周期，默认且目前**仅支持 `"30m"`**。后端可能保留更久 |
| `comparison_response_id` | 诊断缓存未命中时，指定对比的 response id（Responses 专有） |

### 显式断点

在内容块上加 `prompt_cache_breakpoint`：
```json
{ "type": "input_text", "text": "……长系统提示……", "prompt_cache_breakpoint": { "mode": "explicit" } }
```
断点继承请求级 `prompt_cache_options.ttl`，边界**不做 token block 对齐**。

### 规则要点

- 支持 `gpt-5.6` 及之后的模型（含 Astra）。
- 每个请求最多写 **4 个断点**。
- 缓存匹配时最多考虑会话中**最近 80 个断点**，内容块回溯无上限。
- `prompt_cache_key` 用来分桶，提高命中率（替代旧的 `user` 字段）。
- `prompt_cache_retention` **已废弃**，从 GPT-5.5 及更早迁移时替换为 `prompt_cache_options.ttl: "30m"`。

### 计费

- 缓存命中输入：$1.00/M（短上下文）
- 缓存写入：$12.50/M（= 未缓存输入价 × 1.25）
- 诊断字段：`prompt_cache_diagnostics`（含 `cache_missed_tokens`、`reason`、`comparison_reusable_tokens`）
- 诊断文档：https://developers.openai.com/api/docs/guides/prompt-caching/diagnostics

---

## 13. GPT-6 Astra 三个新能力

### 13.1 Async tool calling（异步工具调用）

**官方文档**：https://developers.openai.com/api/docs/guides/async-tool-calling

在 function 或 custom 工具上设 `"async": true`，模型可在你的应用执行工具期间**继续推理、调用其他工具、或回答请求中相互独立的部分**。结果就绪后用原来的 `call_id` 回传。工具执行和挂起任务管理仍由你的应用负责。

```json
{
  "type": "function",
  "name": "run_long_query",
  "async": true,
  "parameters": { "type": "object", "properties": {}, "additionalProperties": false }
}
```

### 13.2 Mid-turn steering（中途转向）

**官方文档**：https://developers.openai.com/api/docs/guides/steering

在 Astra 工作过程中通过 **WebSocket** 发送额外的用户指令（修正或需求变更）。Responses API 会保留已完成的工作，并把更新纳入续写（continuation）。

配套文档：[WebSocket Mode](https://developers.openai.com/api/docs/guides/websocket-mode)

### 13.3 `configuration_update`：会话中途改推理强度且保留缓存

**官方文档**：https://developers.openai.com/api/docs/guides/reasoning#change-reasoning-mid-conversation

在 `input` 数组中、下一条 user 消息**之前**插入：
```json
{
  "type": "configuration_update",
  "reasoning": { "effort": "high" }
}
```

**关键点**：
- **仅 `gpt-6-astra` 支持**，且**仅限 standard 单 Agent 模式**。只能改推理强度。
- **请求级 `reasoning.effort` 保持不变**，这样 prompt 前缀不变，prompt cache 才能继续命中。
- 新的 effort 一直生效，直到下一个 `configuration_update` 覆盖它。
- effort 值不被支持时返回 400。

**完整示例（Python）**：
```python
from openai import OpenAI

client = OpenAI()
model = "gpt-6-astra"

response = client.responses.create(
    model=model,
    reasoning={"effort": "low"},
    input="Draft a database migration plan.",
    store=True,
)

response = client.responses.create(
    model=model,
    previous_response_id=response.id,
    reasoning={"effort": "low"},          # 保持不变！
    input=[
        {"type": "configuration_update", "reasoning": {"effort": "high"}},
        {"role": "user", "content": "Analyze the failure modes and propose rollback steps."},
    ],
    store=True,
)
print(response.output_text)
```

---

## 14. 多轮会话状态管理

**官方文档**：https://developers.openai.com/api/docs/guides/conversation-state

三种方式，按需选择：

| 方式 | 适用 | 说明 |
|---|---|---|
| `previous_response_id` | 最简单 | 串联上一轮响应。**注意：不消除前文的 input token 计费** |
| `conversation` | 持久会话 | 配合 Conversations API，输入项自动前置、输出自动追加 |
| 手动回放 Items | 无状态 / ZDR | `store: false`，自己维护 `output` 数组并回灌 |

### 无状态 / ZDR 场景保留推理链

```json
{
  "model": "gpt-6-astra",
  "store": false,
  "include": ["reasoning.encrypted_content"],
  "input": [ /* 把上一轮的 reasoning item（含 encrypted_content）原样带回 */ ]
}
```

### 长会话压缩

```json
{
  "context_management": [
    { "type": "compaction", "compact_threshold": 400000 }
  ]
}
```
**官方文档**：https://developers.openai.com/api/docs/guides/compaction

### 常见坑（官方"Common migration errors"原文）

- 读 `choices[0].message.content` 而不是 `response.output_text` / `response.output`
- 把 `output` 里每一项都当成 message（reasoning、tool call、function call 是**独立的 Item 类型**）
- 手动传上下文时丢掉 reasoning / function_call / function_call_output Item
- 回传函数结果时没带对应的 `call_id`
- Responses 里误用 `response_format`（应该是 `text.format`）
- 直接复用 Chat Completions 的流式 chunk 处理器
- 以为用了 `previous_response_id` 前文就不计费了 —— **前文 input token 照样计费**

---

## 15. 错误说明大全（含你遇到的报错）

**官方文档**：https://developers.openai.com/api/docs/guides/error-codes

### 15.1 官方 API 错误码总表

| 状态码 | error.code / type | 原因 | 处理 |
|---|---|---|---|
| 400 | `invalid_request_error`，`error.param = service_tier` | 请求的或解析出的 service tier 不被项目允许 | 设为项目允许的 tier，或改项目设置。项目限制作用于 `default`/`flex`/`priority`；`fast` 按 `priority` 评估；Scale Tier 不受此策略约束 |
| 400 | `invalid_request_error` | 参数非法/缺失/格式不对/不支持的组合 | **不要原样重试**。读 message 修请求 |
| 401 | Invalid Authentication | 认证凭据无效 | 检查 API key 和组织/项目是否匹配 |
| 401 | Incorrect API key provided | key 错误、有空格、已删除或被撤销 | 换新 key，清缓存 |
| 401 | 未加入组织 | 账号不属于任何组织 | 让管理员邀请加入 |
| 401 | IP not authorized | 请求 IP 不在允许列表 | 从允许的 IP 发起，或改 IP allowlist |
| 403 | Country/region not supported | 地区不支持 | 见[支持国家列表](https://developers.openai.com/api/docs/supported-countries) |
| 404 | NotFound | response / conversation / vector store / file ID 错误或过期 | 核对 ID 和项目 |
| 409 | Conflict | 资源被并发修改 | 重新读取状态再应用变更，用乐观锁/幂等键 |
| 422 | UnprocessableEntity | 格式正确但无法处理 | 先排查确定性原因（大小、编码、文件状态、字段组合） |
| 429 | `credit_balance_exhausted` | 预付额度耗尽 | **重试无效**，去充值 |
| 429 | Rate limit reached | 请求/token 速率超限 | 遵循 `Retry-After`；无该头时用带 jitter 的指数退避 |
| 429 | `rate_limit_error` / `slow_down` | 流量**爬坡太快**（即使没超 RPM/TPM） | 遵循 `Retry-After`，降速后**逐步**回升。经验法则：达到 1M input TPM 后，每 15 分钟涨幅不超过 50% |
| 429 | `organization_spend_limit_exceeded` | 组织月度消费上限 | 改组织 limit 设置 |
| 429 | `project_spend_limit_exceeded` | 项目月度消费上限 | 改项目 limit 设置 |
| 429 | `organization_usage_limit_exceeded` | 组织触达 OpenAI 分配的用量上限 | 申请提额或联系支持 |
| 500 | Internal Server Error | 服务端问题 | 短暂等待后有界重试，记录 request id |
| 503 | `service_unavailable_error` / `server_is_overloaded` | 模型临时过载 | 遵循 `Retry-After` 后重试；查[状态页](https://status.openai.com/) |

> **重要**：计费类错误要看 `error.code` 细分，`error.type` 可能统一是 `insufficient_quota`。
> 计费/消费/额度类错误**重试不会恢复访问**，必须先改额度或充值。

### 15.2 ⭐ 你遇到的报错：`max_output_tokens` 最小值

```
status_code=400, Invalid 'max_output_tokens': integer below minimum value.
Expected a value >= 16, but got 1 instead. (request id: a392a10d29e77c8f)
```

| 项目 | 说明 |
|---|---|
| **原因** | `max_output_tokens` 传了 `1`，而 API 校验要求 **≥ 16** |
| **触发端点** | `/v1/responses`（Chat Completions 对应字段是 `max_completion_tokens`，同类下限校验） |
| **错误类型** | `invalid_request_error`，`error.param = "max_output_tokens"` |
| **是否可重试** | ❌ 不可。参数校验类 400 原样重试永远失败 |

**修复方式**：
```diff
  {
    "model": "gpt-6-astra",
    "input": "...",
-   "max_output_tokens": 1
+   "max_output_tokens": 4096
  }
```

**为什么最小是 16**：推理模型生成的每个 token 里包含不可见的推理 token 和格式化 token，把上限压到个位数没有任何有效输出空间，API 层直接拒绝。

**⚠️ 对 GPT-6 Astra 的额外提醒**：
即使传 16 也**几乎必定**返回 `status: "incomplete"` + `incomplete_details.reason: "max_output_tokens"`，且**零可见输出但照常计费 input + reasoning tokens**。
**官方建议：初次接入至少为推理 + 输出预留 25,000 tokens。**

**网关侧建议**：如果 `max_output_tokens` 是网关自动填充的（比如探活请求用 1 来省钱），请改成一个合法的小值（如 16）并接受可能拿不到可见输出；更稳妥的探活方式是调用 `GET /v1/models` 而不是真实推理。

### 15.3 GPT-6 Astra 参数类 400 报错速查

| 报错（message 形态） | 触发参数 | 修复 |
|---|---|---|
| `Unsupported parameter: 'temperature' is not supported with this model.` | `temperature` | 删除该字段 |
| `Unsupported parameter: 'top_p' is not supported with this model.` | `top_p` | 删除 |
| `Unsupported parameter: 'top_logprobs' is not supported with this model.` | `top_logprobs` | 删除 |
| `Unsupported parameter: 'logprobs' is not supported with this model.` | `logprobs`（Chat Completions） | 删除 |
| `Unsupported value: 'reasoning_effort' does not support 'none' with this model.` | `reasoning_effort` / `reasoning.effort` | 改为 `low` 及以上 |
| 函数工具在 Chat Completions 上被拒 | `tools` / `functions` | 改用 `/v1/responses` |
| `Invalid 'max_output_tokens': integer below minimum value. Expected a value >= 16` | `max_output_tokens` | 调到 ≥16（建议 ≥25000） |
| `Invalid service_tier argument: The requested service tier is not allowed for this project.` | `service_tier` | 改项目设置或换 tier |
| `Item 'rs_...' of type 'reasoning' was provided without its required following item.` | 手动回放 Item 时丢了配套项 | reasoning item 必须与其后续 item 成对回传 |
| 超上下文窗口 | 输入过长 | 设 `truncation: "auto"`，或裁剪输入 |

> 部分具体 message 文案来自社区/第三方复现（LlamaIndex #20459、LiteLLM #40123、OpenRouter 迁移指南等），
> OpenAI 可能微调措辞。**代码里请按 `error.param` + `status` 判断，不要硬匹配 message 字符串。**

### 15.4 WebSocket 模式专属错误

| code | 说明 | 处理 |
|---|---|---|
| `previous_response_not_found` | `previous_response_id` 无法从可用状态解析 | 带完整 input context 重试，并把 `previous_response_id` 设为 `null` |
| `websocket_connection_limit_reached` | 连接触达 60 分钟上限 | 新开一个 WebSocket 连接继续 |

### 15.5 Python SDK 异常类映射

| 异常类 | 触发 | 处理 |
|---|---|---|
| `APIConnectionError` | 网络/代理/TLS/DNS/防火墙 | 检查网络、代理、证书、防火墙规则 |
| `APITimeoutError` | 请求超时 | 短暂等待后重试 |
| `AuthenticationError` | key/token 无效、过期、被撤销 | 换 key |
| `BadRequestError`（原 `InvalidRequestError`） | 请求畸形或缺参 | 按 message 修参数，不要盲重试 |
| `ConflictError` | 资源被并发更新 | 重新读取再更新 |
| `InternalServerError` | 服务端错误（**503 也走这个类**） | 有界重试 |
| `NotFoundError` | 资源不存在 | 核对 ID |
| `PermissionDeniedError` | 无权访问 | 核对 key / org id / 资源 id |
| `RateLimitError` | **429**（限速或爬坡过快） | 遵循 `Retry-After` |
| `UnprocessableEntityError` | 格式对但处理不了 | 排查确定性原因后重试 |

> **⚠️ 迁移注意（官方原文）**：Python SDK 对 429 抛 `RateLimitError`，对 503 抛 `InternalServerError`。
> 如果你原来只捕获其中一个类来做限流/过载处理，现在**两个都要捕获**并检查 `error.code`。
> 例如 Video 过载现在返回 503（以前是 429）。

### 15.6 重试决策矩阵

| 类别 | 能否原样重试 | 正确动作 |
|---|---|---|
| 400 / 401 / 403 / 404 | ❌ 否 | 修请求、身份、权限或 ID |
| 409 | ⚠️ 需先对账 | 重新读取版本后安全应用 |
| 422 | ⚠️ 有时可以 | 先排除确定性原因 |
| 429（rate / slow_down） | ✅ 有界重试 | 遵循 `Retry-After`，退避 + jitter |
| 429（计费 / 额度类） | ❌ 否 | 充值或改额度上限 |
| 500 / 503 | ✅ 有界重试 | 退避、查状态页、保留 request id |
| 连接 / 超时 | ⚠️ 视情况 | 读操作可重试；写操作先确认是否已产生副作用 |

### 15.7 标准错误处理代码（官方示例）

**Python**
```python
import openai
from openai import OpenAI

client = OpenAI()

try:
    response = client.responses.create(model="gpt-6-astra", input="Hello world")
except openai.APIConnectionError as e:
    print(f"Failed to connect to OpenAI API: {e}")
except openai.RateLimitError as e:
    print(f"OpenAI API request exceeded rate limit: {e}")
except openai.APIError as e:
    print(f"OpenAI API returned an API Error: {e}")
else:
    print(response.output_text)
```

**Go**
```go
client := openai.NewClient()
response, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
	Model: "gpt-6-astra",
	Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("Hello world")},
})
if err != nil {
	var apiError *openai.Error
	if errors.As(err, &apiError) {
		fmt.Println("OpenAI API returned an API error:", apiError)
		return
	}
	fmt.Println("Failed to connect to OpenAI API:", err)
	return
}
fmt.Println(response.OutputText())
```

**Node.js**
```javascript
try {
  const response = await client.responses.create({
    model: "gpt-6-astra",
    input: "Hello world",
  });
  console.log(response.output_text);
} catch (error) {
  if (error instanceof OpenAI.APIConnectionError) {
    console.error("Failed to connect to the OpenAI API:", error.message);
  } else if (error instanceof OpenAI.RateLimitError) {
    console.error("OpenAI API request exceeded its rate limit:", error.message);
  } else if (error instanceof OpenAI.APIError) {
    console.error("OpenAI API returned an error:", error.status, error.message);
  } else {
    throw error;
  }
}
```

### 15.8 排障时要记录的信息（官方要求）

联系支持时提供：
- 使用的模型
- 收到的错误 message 和 code
- **request id**（在响应头 / 错误信息里）
- 发送的请求数据和请求头
- 请求的时间戳和时区

**日志建议**：记录 request id、时间戳时区、模型、端点、status、code、重试次数、脱敏后的 payload 特征。**绝不要记录 API key 和敏感提示词内容。**

---

## 16. 速率限制

**官方文档**：https://developers.openai.com/api/docs/guides/rate-limits

### GPT-6 Astra Standard 限额

| Tier | RPM | TPM | Batch queue limit |
|---|---:|---:|---:|
| Tier 1 | 500 | 500,000 | 1,500,000 |
| Tier 2 | 5,000 | 1,000,000 | 3,000,000 |
| Tier 3 | 5,000 | 2,000,000 | 100,000,000 |
| Tier 4 | 10,000 | 4,000,000 | 200,000,000 |
| Tier 5 | 15,000 | 40,000,000 | 15,000,000,000 |

### 爬坡限速（重要）

即使在 RPM/TPM 之内，**流量增长太快**也会触发 `429 slow_down`。
官方经验法则：流量达到 **1M input TPM** 后，每 **15 分钟** 增幅不超过 **50%**。具体触发点随模型和流量情况变化。

企业客户若常态触达爬坡限速，可考虑 [Scale Tier](https://openai.com/api-scale-tier/)；GPT-5.6 及之后的模型可用 [Reserved Tier](https://openai.com/api-reserved-tier/)。

---

## 17. 从 GPT-5.6 / GPT-5.5 迁移清单

**官方迁移文档**：https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra#migration-quickstart

### 参数改动

- [ ] `model` 设为 `gpt-6-astra`
- [ ] **推理强度**：原来用 `none` 或 `minimal` 的，**从 `low` 开始**测。其余保持原有等效强度。Responses 用 `reasoning.effort`，CC 用 `reasoning_effort`
- [ ] **工具调用**：改用 Responses API。Astra 支持 Chat Completions，但**工具调用必须走 Responses**
- [ ] **删除不支持的参数**：`temperature`、`top_p`、`top_logprobs`。Chat Completions 额外删 `logprobs`；Responses 额外从 `include` 里删 `message.output_text.logprobs`
- [ ] **Fast mode**：EU 数据驻留场景改用 Standard 处理。Astra 在 EU 数据驻留下不支持 `service_tier: "fast"` / `"priority"`。Astra 的 Fast mode **无延迟 SLA**
- [ ] **动态改推理强度**：用 `configuration_update` Item，**请求级 `reasoning.effort` 保持不变**以保留缓存前缀。先确认[兼容性限制](https://developers.openai.com/api/docs/guides/reasoning#change-reasoning-mid-conversation)
- [ ] **Prompt caching**：从 GPT-5.5 及更早迁移时，`prompt_cache_retention` → `prompt_cache_options.ttl: "30m"`

### 从 Chat Completions 迁到 Responses 的完整清单

- [ ] 端点 `POST /v1/chat/completions` → `POST /v1/responses`
- [ ] `messages` → `input`（简单消息数组两边通用，可直接复用）
- [ ] 输出 `choices[0].message.content` → `response.output_text` 或遍历 `response.output`
- [ ] 结构化输出 `response_format` → `text.format`
- [ ] 函数定义和结果的形态变了，`call_id` 必须正确回传
- [ ] 流式消费改为处理 typed Responses events
- [ ] 选定状态策略：`previous_response_id` / 手动回放 Item / Conversations API
- [ ] 无状态或 ZDR 场景：`store: false` + `include: ["reasoning.encrypted_content"]`
- [ ] 用官方托管工具替代自造编排逻辑
- [ ] 灰度前对比行为、延迟、token 用量和错误率

### 用 Codex 自动迁移

```
$openai-docs migrate this project to GPT-6 Astra
```
技能包：https://github.com/openai/codex/tree/main/codex-rs/skills/src/assets/samples/openai-docs

---

## 18. 提示词最佳实践（官方原文）

**官方文档**：https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra#prompting-best-practices

### Astra 的五个行为特点

| 特点 | 表现 |
|---|---|
| **主动性与跟进** | 更像协作者，**更容易在额外输入可能改变结果时主动提问**，可能在你希望它自行假设并推进时停下来 |
| **指令遵循** | 比之前模型更强，但也**对 skill 文件、`AGENTS.md` 等上下文中的指令更敏感**。官方**强烈建议**审计模型可访问的所有 skill 和指令文件 |
| **人格与写作风格** | 倾向详细、重格式的回答（列表、表格、Markdown），可能跨会话重复固定短语 |
| **子 Agent 委派** | 委派频率可能低于你的预期，需要明确指定何时、多大程度使用子 Agent 并行 |
| **测试与验证** | 编码任务中在认为完成前会做较充分的测试；小任务上可能测过头 |

### 官方给的提示词片段（可直接复制）

**① 让它更自主、别过早停下**
```text
You should infer the user's intent and task scope from the instructions and prior conversation context. Your job is to bias towards action and carry the user's intended task to completion.

When the user expresses intent to perform new work or fix an existing issue, persist until the user's intended goal is complete. Progress autonomously towards the user's goal (e.g. creating isolated worktrees / checkouts if needed, resolving merge conflicts, read-only actions, creating draft PRs etc.) unless they are clearly destructive or irreversible.
```

**② 把"能不能帮我…"当成执行指令**
```text
When the user's prompt indicates a request for action, such as "can you...", "I want to...", "help me..." and similar expressions, treat these as instructions to do the work and take action. Do not stop at acknowledging capability (e.g. "Yes…"), proposing a plan, or offering to continue. Do not settle for a partial or "helpful enough" solution that does not fully satisfy the user's task to save time, effort or tokens. If a task requires sustained work, complete all the necessary work until the intended outcome is fulfilled.
```

**③ 先做出可评审的结果，再问审批**
```text
Before asking the user clarifying questions, you should complete the work that is already authorized from context and necessary to make the proposed action concrete and reviewable. The user should be approving a concrete, reviewable result. For example, before deploying a change, writing to an external application, merging a PR or publishing a site, do all the required work first so that user approval is the final step. You don't need user permission for reversible tasks, read-only actions, reviews or fixes, or anything for which authorization is provided earlier in the session or strongly implied from the task instruction.

Do not introduce unsolicited warnings, disclaimers, approval flows, or safety/compliance checklists due to hypothetical risk.
```

**④ 明确用户指令优先于 skill**
```text
The user's instructions take precedence over guidelines provided in a skill. If explicit user instructions conflict with a skill's instructions, prioritize the user's instructions.
```

**⑤ 让它说清楚是哪条 skill 让它停下来的**
```text
If a skill causes you to ask for permission or confirmation, pause, leave requested work unfinished, or diverge from the user's intent, name and link to the exact SKILL.md file you read, quote the relevant instruction, and briefly explain how it applies. Distinguish explicit skill requirements from your interpretation of guidelines.
```

**⑥ 要散文不要满屏列表**
```text
Default to using clear, concise paragraphs, each developing one main idea. Use lists only when the information is genuinely parallel, sequential, or easier to compare, and avoid nested lists unless the hierarchy cannot be expressed clearly in prose. Use plain, simple language: familiar words, concrete examples, and precise verbs. Prefer active voice and direct statements.

Make sure to state the main point clearly and early, then develop it with the explanation and detail the reader needs. Let each sentence build on what came before. Develop the points that matter and provide enough support to be useful.
```

**⑦ 技术表达的分寸**
```text
Use plain language over jargon, and reference technical details only to the degree that it helps illustrate an idea or your work to the user. Communicate complex concepts in a clear and cohesive manner, and calibrate your writing to the level of background knowledge assumed from the user's prompt and context.
```

**⑧ 去 AI 味（去套话）**
```text
Avoid using slop words or phrases like "Bottom Line:" in conclusions, "delve," "foster," "leverage," "it's worth noting," "importantly," "Question? Answer." or "This isn't about X. It's about Y.", "genuinely" or hyphenated compound descriptions and adjectives. Do not use concluding summary statements such as "In short:..", "The simplest mental model is:...".

State the intended action directly. Avoid adding what you won't do, what will remain unchanged, or how you'll separate or categorize results. Do not use contrastive framing such as "X, not Y" or "X—not Y" that introduces an unprompted alternative that the user didn't ask about. Avoid invented compound labels like "exact-head checks" and "editorial-row layouts", vague qualifiers, and canned transitions; use plain verbs and prepositions to state the actual relationship directly.
```

**⑨ 调节子 Agent 委派**
```text
If at any point you can parallelize work by delegating tasks to another agent (no matter if you are the root or subagent), you should do so using collaboration tools if it could save time or improve quality.
```

**⑩ 让 Agent 间消息可读**
```text
Messages that you send to other agents and your final answer may be read by a human, so ensure they are legible. Always put proper spaces between words and/or numbers.
```

**⑪ 控制测试范围**
```text
Do not write tests for reversible, low-impact changes that mirror the implementation. If you do choose to verify your work with tests, make sure that the tests are meaningful and necessary to verify implementation.

Run tests appropriate to the change and complete required checks. Once those pass, broaden or repeat testing only when new changes, failures, or unresolved concerns justify it; otherwise, continue toward completing the task.
```

---

## 19. 官方文档链接汇总

### 模型与定价
| 主题 | 链接 |
|---|---|
| GPT-6 Astra 模型页 | https://developers.openai.com/api/docs/models/gpt-6-astra |
| **Using GPT-6 Astra（迁移+提示词）** | https://developers.openai.com/api/docs/guides/latest-model/gpt-6-astra |
| 模型列表 | https://developers.openai.com/api/docs/models |
| 模型对比 | https://developers.openai.com/api/docs/models/compare |
| 定价 | https://developers.openai.com/api/docs/pricing |
| Changelog | https://developers.openai.com/api/docs/changelog |
| 废弃公告 | https://developers.openai.com/api/docs/deprecations |
| 发布博客 | https://openai.com/index/gpt-6-astra/ |

### API 参考
| 主题 | 链接 |
|---|---|
| **Responses — Create** | https://developers.openai.com/api/reference/resources/responses/methods/create |
| Responses 流式事件 | https://developers.openai.com/api/reference/resources/responses/streaming-events |
| Responses WebSocket 事件 | https://developers.openai.com/api/reference/resources/responses/websocket-events |
| Responses — Retrieve / Cancel / Delete / Compact | https://developers.openai.com/api/reference/resources/responses |
| **Chat Completions 参考** | https://developers.openai.com/api/reference/resources/chat |
| Chat Completions 流式事件 | https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events |
| API 参考总览 | https://developers.openai.com/api/reference/overview |

### 核心指南
| 主题 | 链接 |
|---|---|
| 快速开始 | https://developers.openai.com/api/docs/quickstart |
| **迁移到 Responses API** | https://developers.openai.com/api/docs/guides/migrate-to-responses |
| **推理模型（reasoning）** | https://developers.openai.com/api/docs/guides/reasoning |
| 推理最佳实践 | https://developers.openai.com/api/docs/guides/reasoning-best-practices |
| 文本生成 | https://developers.openai.com/api/docs/guides/text |
| 结构化输出 | https://developers.openai.com/api/docs/guides/structured-outputs |
| 流式响应 | https://developers.openai.com/api/docs/guides/streaming-responses |
| 会话状态 | https://developers.openai.com/api/docs/guides/conversation-state |
| 上下文压缩（Compaction） | https://developers.openai.com/api/docs/guides/compaction |
| Token 计数 | https://developers.openai.com/api/docs/guides/token-counting |
| 后台模式 | https://developers.openai.com/api/docs/guides/background |
| WebSocket 模式 | https://developers.openai.com/api/docs/guides/websocket-mode |
| **中途转向（Steering）** | https://developers.openai.com/api/docs/guides/steering |
| 多 Agent | https://developers.openai.com/api/docs/guides/responses-multi-agent |
| Webhooks | https://developers.openai.com/api/docs/guides/webhooks |
| 文件输入 | https://developers.openai.com/api/docs/guides/file-inputs |

### 工具
| 主题 | 链接 |
|---|---|
| Using tools 总览 | https://developers.openai.com/api/docs/guides/tools |
| Function calling | https://developers.openai.com/api/docs/guides/function-calling |
| **异步工具调用** | https://developers.openai.com/api/docs/guides/async-tool-calling |
| 程序化工具调用 | https://developers.openai.com/api/docs/guides/tools-programmatic-tool-calling |
| Tool search | https://developers.openai.com/api/docs/guides/tools-tool-search |
| Web search | https://developers.openai.com/api/docs/guides/tools-web-search |
| File search | https://developers.openai.com/api/docs/guides/tools-file-search |
| Code Interpreter | https://developers.openai.com/api/docs/guides/tools-code-interpreter |
| Computer use | https://developers.openai.com/api/docs/guides/tools-computer-use |
| Computer use 集成实践 | https://developers.openai.com/api/docs/guides/tools-computer-use-integration |
| Shell | https://developers.openai.com/api/docs/guides/tools-shell |
| Local shell | https://developers.openai.com/api/docs/guides/tools-local-shell |
| Apply Patch | https://developers.openai.com/api/docs/guides/tools-apply-patch |
| Skills | https://developers.openai.com/api/docs/guides/tools-skills |
| MCP 与连接器 | https://developers.openai.com/api/docs/guides/tools-connectors-mcp |
| 图片生成工具 | https://developers.openai.com/api/docs/guides/tools-image-generation |

### 运维与成本
| 主题 | 链接 |
|---|---|
| **错误码** | https://developers.openai.com/api/docs/guides/error-codes |
| **速率限制** | https://developers.openai.com/api/docs/guides/rate-limits |
| Prompt caching | https://developers.openai.com/api/docs/guides/prompt-caching |
| Prompt cache 诊断 | https://developers.openai.com/api/docs/guides/prompt-caching/diagnostics |
| 成本优化 | https://developers.openai.com/api/docs/guides/cost-optimization |
| Flex 处理 | https://developers.openai.com/api/docs/guides/flex-processing |
| Fast mode | https://developers.openai.com/api/docs/guides/fast-mode |
| 消费上限 | https://developers.openai.com/api/docs/guides/spend-limits |
| 支持的国家/地区 | https://developers.openai.com/api/docs/supported-countries |
| 状态页 | https://status.openai.com/ |

### 安全
| 主题 | 链接 |
|---|---|
| **失准监控（Astra 新增）** | https://developers.openai.com/api/docs/guides/safety-checks/misalignment-monitoring |
| 安全最佳实践 | https://developers.openai.com/api/docs/guides/safety-best-practices |
| 数据使用政策 | https://developers.openai.com/api/docs/guides/your-data |

### 第三方平台
| 主题 | 链接 |
|---|---|
| Azure / Microsoft Foundry 推理模型 | https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/reasoning |
| Amazon Bedrock 差异 | https://developers.openai.com/api/docs/guides/amazon-bedrock |
| OpenRouter GPT-6 Astra 迁移指南 | https://openrouter.ai/docs/cookbook/evaluate-and-optimize/model-migrations/gpt-6-astra |

### 文档站小技巧

任何文档页 URL 加 `.md` 后缀即可拿到 Markdown 原文，方便喂给 AI：
```bash
curl -sL https://developers.openai.com/api/docs/guides/reasoning.md
```

索引文件：
- 全站索引：https://developers.openai.com/llms.txt
- API 指南索引：https://developers.openai.com/api/docs/llms.txt
- API 参考索引：https://developers.openai.com/api/reference/llms.txt
- **全量单文件导出**：https://developers.openai.com/api/llms-full.txt

---

## 附：一分钟自检清单

接入 `gpt-6-astra` 前，逐条确认：

- [ ] 用的是 `/v1/responses`（需要工具时**必须**）
- [ ] 请求体里**没有** `temperature`、`top_p`、`top_logprobs`、`logprobs`
- [ ] `reasoning.effort` 不是 `none`（最低 `low`）
- [ ] `max_output_tokens` ≥ 16，实际建议 ≥ 25000
- [ ] Chat Completions 用 `max_completion_tokens`，不用 `max_tokens`
- [ ] 结构化输出：Responses 用 `text.format`，CC 用 `response_format`
- [ ] 多轮回传时 reasoning / function_call Item 完整、`call_id` 正确
- [ ] 错误处理：按 `status` + `error.code` + `error.param` 分类，不硬匹配 message
- [ ] 429 区分限速类（可退避重试）与计费类（不可重试）
- [ ] 输入超 272K 时成本翻倍（input 2x、output 1.5x），已做预算评估
- [ ] 日志记录 request id，且不记录 API key
