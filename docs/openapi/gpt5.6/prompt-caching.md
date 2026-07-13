# GPT-5.6 Prompt Caching 机制说明

> 调研时间：2026-07-10，来源：OpenAI 官方文档 `developers.openai.com/api/docs/guides/prompt-caching`、`/pricing`、`/guides/latest-model`。
>
> **本文档第 1-6 节描述的显式缓存机制（`prompt_cache_options`/`prompt_cache_breakpoint`/`cache_write_tokens`）专指 OpenAI 官方原生 API（`api.openai.com`）。Azure OpenAI 目前不支持这套机制，也还没有上架 GPT-5.6，详见第 7 节。本项目 commit `8c84ed53` 的透传字段与计费改动，实现的正是第 1-6 节这套 OpenAI 原生机制。**

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

## 3. OpenAI 官方到底怎么"创建"缓存

### 3.1 关键认知：没有单独的"创建缓存"接口

这是最容易被误解的一点：OpenAI 不像 Gemini（`models.cachedContents.create` 单独建缓存、拿到一个 `cache_name` 再引用）那样有一个专门的"创建缓存"API。**"创建缓存"和"正常对话请求"是同一个 API 调用**——你打的 `prompt_cache_breakpoint`，只是告诉 OpenAI "这里往前的内容值得存一份"，创建动作是这次请求的**副作用**，不是一个独立步骤：

1. 你正常调用 `/v1/chat/completions` 或 `/v1/responses`，在某个内容块上加 `prompt_cache_breakpoint: {"mode": "explicit"}`。
2. 服务端检查这个断点之前的内容有没有缓存命中：
   - **没命中（第一次调用，或缓存已过期）** → 正常跑完整推理，同时把断点之前的内容写入缓存 → 这次请求的 `cache_write_tokens > 0`，`cached_tokens = 0`，多付 1.25× 的钱。**这次调用本身就是"创建缓存"的动作**。
   - **命中（后续调用，prompt_cache_key 一致、前缀完全一致、还在 TTL 内）** → 跳过重新计算，直接复用 → `cached_tokens > 0`，`cache_write_tokens = 0`，按 10% 折扣计费。
3. 同一个断点一旦写入成功，30 分钟 TTL 内的后续调用只会"读"不会"重写"——即使中间又调用了 10 次，也不会再产生 `cache_write_tokens`（除非过期后重新触发一次写入）。

所以"创建缓存"= **带着 `prompt_cache_breakpoint` 打第一次请求**，没有第二个动作。

### 3.2 完整生命周期示例：从创建到命中

用同一个 `prompt_cache_key` + 完全相同的前缀连续打两次 Chat Completions，观察 usage 字段的变化：

**第 1 次调用（创建 / cache miss）**——注意此时缓存里还没有这段内容：

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
            "text": "You are a support assistant. <...1024+ tokens 的稳定说明/知识库...>",
            "prompt_cache_breakpoint": { "mode": "explicit" }
          }
        ]
      },
      { "role": "user", "content": "第一个用户的问题" }
    ]
  }'
```

对应响应（节选）——`cache_write_tokens` 非零，说明这次请求把断点之前的内容**新写入**了缓存：

```json
"usage": {
  "prompt_tokens": 1300,
  "completion_tokens": 120,
  "prompt_tokens_details": {
    "cached_tokens": 0,
    "cache_write_tokens": 1200
  }
}
```

**第 2 次调用（命中 / cache read）**——`prompt_cache_key` 不变，断点之前的内容一字不差，只换了 user 消息：

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
            "text": "You are a support assistant. <...和上一次逐字节完全相同的说明/知识库...>",
            "prompt_cache_breakpoint": { "mode": "explicit" }
          }
        ]
      },
      { "role": "user", "content": "第二个用户的问题（内容可以不同）" }
    ]
  }'
```

对应响应（节选）——这次 `cached_tokens` 非零、`cache_write_tokens` 归零，说明命中了第一次写入的缓存：

```json
"usage": {
  "prompt_tokens": 1310,
  "completion_tokens": 95,
  "prompt_tokens_details": {
    "cached_tokens": 1200,
    "cache_write_tokens": 0
  }
}
```

两次调用的差异只在断点之后的 `user` 消息内容；断点之前的 1200 tokens 逐字节相同，所以第 2 次命中。30 分钟内继续用同一个 `prompt_cache_key` 打相同前缀，会一直读到这份缓存；超过 30 分钟没人用（或被更长时间挤出），下一次调用就会退回"cache miss → 重新写入"，回到第 1 次的状态。

### 3.3 标记断点的两种写法（Responses API / Chat Completions API）

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


## 7. Azure OpenAI 是否支持这套显式缓存？—— 不支持，且目前连模型都没有

结论先说：**同事的判断是对的，有官方依据**。而且比"创建缓存不行"更彻底——Azure 上目前根本没有 GPT-5.6 这个模型，显式缓存断点机制对 Azure 渠道来说无从谈起。

### 7.1 证据 1：Azure 官方文档只字未提"显式缓存"

Azure 官方 Prompt Caching 文档：

- https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/prompt-caching （`ms.date: 2026-05-13`）

全文抓取后逐字检索 `explicit` / `breakpoint` / `cache_write` / `prompt_cache_options` —— **零匹配**。文档里只讲了两种策略：

```
"prompt_cache_retention": "in_memory"
```

或：

```
"prompt_cache_retention": "24h"
```

以及路由用的 `prompt_cache_key`，返回侧只定义了 `prompt_tokens_details.cached_tokens`，没有 `cache_write_tokens`。这与本目录下 `Azure_gpt.md` 里同事的原始判断完全一致。

### 7.2 证据 2：Azure 模型清单里根本没有 GPT-5.6

Azure 官方模型目录页：

- https://aka.ms/oai/modelupdates （"Foundry Models sold by Azure"）

列出的最新系列是：

```
GPT-5.5 series   NEW  gpt-5.5
GPT-5.4 series   gpt-5.4-mini, gpt-5.4-nano, gpt-5.4, gpt-5.4-pro
GPT-5.3 series   gpt-5.3-chat, gpt-5.3-codex
...
```

**没有 gpt-5.6 / sol / terra / luna 任何条目。** GPT-5.6 是 2026-06-26 才开始"限量预览"（OpenAI 官方博客 [Previewing GPT-5.6 Sol](https://openai.com/index/previewing-gpt-5-6-sol/)），2026-07-09 才 GA（[GPT-5.6: Frontier intelligence that scales with your ambition](https://openai.com/index/gpt-5-6/)），目前还是政府协调的小范围放量。Azure 侧连模型本身都没上架，自然不存在"创建缓存"这个问题——不是"不行"，是"还没有"。

### 7.3 证据 3：历史规律——Azure 对 OpenAI 新缓存参数一贯滞后数月

三个 Microsoft Q&A 官方论坛帖子显示同一模式：OpenAI 每出新的缓存能力，Azure 都要晚很久才跟上（甚至一直没跟上）：

1. **2025-11-15**，[Is the new openai prompt_cache_retention setting working for azure?](https://learn.microsoft.com/en-us/answers/questions/5623446/is-the-new-openai-prompt-cache-retention-setting-w) —— 用户在 Azure 上传 GPT-5.1 新增的 `prompt_cache_retention` 参数，返回 **400 Bad Request**；微软员工回复：功能仍在滚动上线，还没覆盖所有部署。
2. **2026-03-04**，[Does Azure OpenAI support "Extended Prompt Cache Retention"?](https://learn.microsoft.com/en-us/answers/questions/5807188/does-azure-openai-support-extended-prompt-cache-re) —— 同样的参数用在 `gpt-5.2` 上仍报错 `prompt_cache_retention is not supported on this model`；官方回复明确：**Azure OpenAI 目前只支持标准 in-memory 缓存，不支持 Extended Retention，且没有支持时间表**。
3. **2026-03-31**，[Realtime API caching behavior available through OpenAI but not through Azure OpenAI](https://learn.microsoft.com/en-us/answers/questions/5845663/realtime-api-caching-behavior-available-through-op) —— 用户反馈同一套实时对话逻辑在 OpenAI 直连能命中缓存，Azure 上 `cached_tokens` 始终为 0。

### 7.4 那 Azure 现在该怎么调用缓存？

既然确认不支持显式断点，Azure 上能用的只有**自动/隐式缓存**——跟 GPT-5.6 之前的老机制一样，不需要任何"创建"动作：

```bash
curl https://{your-resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version=2025-04-01-preview \
  -H "api-key: $AZURE_OPENAI_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "system", "content": "..."},
      {"role": "user", "content": "..."}
    ],
    "prompt_cache_key": "tenant:acme:support-assistant"
  }'
```

- 前 1024 tokens 完全一致即可自动命中，之后每多 128 个相同 token 再多命中一档；
- 加 `prompt_cache_key` 能提升路由到同一台机器的概率，是目前 Azure 上唯一能"半主动"影响缓存命中率的手段；
- 不要发 `prompt_cache_options` / `prompt_cache_breakpoint`，Azure 目前不认，发了要么被忽略要么报错；
- 响应里只有 `cached_tokens`，没有 `cache_write_tokens`——不能从第一次请求 `cached_tokens=0` 就反推所有输入 token 都算"缓存写入"，因为 Azure 官方没有提供这个计费口径，本项目也不应该替 Azure 编造这个字段。

### 7.5 小结

```
Azure Chat 缓存行为符合预期：自动缓存 + cached_tokens 读取折扣
Azure 不支持显式缓存创建（prompt_cache_options / prompt_cache_breakpoint）
Azure 官方 usage 不包含 cache_write_tokens

```
