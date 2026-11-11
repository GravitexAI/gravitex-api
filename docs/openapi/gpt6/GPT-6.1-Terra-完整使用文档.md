# GPT-6.1 Terra 完整使用文档（v1/chat/completions + v1/responses + Azure Foundry）

> 整理时间：2026-10-27
> 数据来源：OpenAI 官方文档站（`developers.openai.com`，页面 URL 加 `.md` 可取 Markdown 原文）+ Microsoft Learn / Azure Foundry 官方博客
> 适用模型：`gpt-6.1-terra`
> 发布日期：2026-10-26（见官方 [Changelog](https://developers.openai.com/api/docs/changelog)，Azure Foundry 同日 GA）

**本文目标**：看完这一篇就能直接接入 GPT-6.1 Terra，并搞清楚 **Prompt Caching 30m/12h 在 OpenAI 与 Azure 上的差异**（这是本模型接入时最容易踩的坑）。

通用部分（Responses/Chat Completions 全量参数、错误码大全、流式事件全表）与 GPT-6 Astra 一致，见 `GPT-6-Astra-完整使用文档.md`，本文只展开 Terra 的差异与专属内容。

---

## 目录

1. [模型概览与规格](#1-模型概览与规格)
2. [端点支持矩阵](#2-端点支持矩阵)
3. [定价（含 30m/12h 缓存写入价）](#3-定价含-30m12h-缓存写入价)
4. [快速开始](#4-快速开始)
5. [与 GPT-6 Astra / GPT-6.1 Sol 的关键差异](#5-与-gpt-6-astra--gpt-61-sol-的关键差异)
6. [专属限制（踩坑必读）](#6-专属限制踩坑必读)
7. [reasoning 推理控制](#7-reasoning-推理控制)
8. [Prompt Caching 详解（OpenAI：30m + 12h）](#8-prompt-caching-详解openai30m--12h)
9. [Azure Foundry 接入（只支持 30m！）](#9-azure-foundry-接入只支持-30m)
10. [OpenAI vs Azure 缓存能力对照表](#10-openai-vs-azure-缓存能力对照表)
11. [网关（gravitex-api）侧接入建议](#11-网关gravitex-api侧接入建议)
12. [从 GPT-5.6 Terra 迁移清单](#12-从-gpt-56-terra-迁移清单)
13. [官方文档链接汇总](#13-官方文档链接汇总)
14. [一分钟自检清单](#14-一分钟自检清单)

---

## 1. 模型概览与规格

**官方文档**：https://developers.openai.com/api/docs/models/gpt-6.1-terra

| 项目 | 值 |
|---|---|
| Model ID | `gpt-6.1-terra` |
| 默认快照 | `gpt-6.1-terra`（目前仅此一个快照/别名） |
| 输入模态 | text、image |
| 输出模态 | text（**不支持 audio/video 输出**） |
| 上下文窗口 | 1,050,000 tokens |
| 最大输入 tokens | 922,000 |
| 最大输出 tokens | 128,000 |
| 知识截止 | 2026-06-01（比 Astra 的 2026-04-30 新两个月） |
| 推理 token | 支持 |
| `reasoning.effort` 可选值 | **`none`** / `low` / `medium`（默认）/ `high` / `xhigh` / `max` |

**支持的特性**：`streaming`、`structured_outputs`、`function_calling`、`file_search`、`image_input`、`web_search`、`prompt_caching`

**不支持的特性**：`fine_tuning`、**`predicted_outputs`**（Predicted Outputs，Astra 是支持的，Terra 不支持）

**Responses API 下支持的内置工具**：
`web_search`、`file_search`、`image_generation`、`code_interpreter`、`hosted_shell`、`apply_patch`、`skills`、`computer_use`、`mcp`、`tool_search`

**官方定位原文**：
> A fast, efficient model for visual reasoning, precise computer use, and clear, natural writing.

**官方给的典型用途**：产品图与目录条目匹配、技术图纸解读、应用界面操作（computer use）、文本起草与修改。

**性能宣传要点**（官方博客 / 社区公告）：
- 相比 GPT-5.6 Terra **标准 API 价直降 50%**（input $2→$1，output $12→$5）
- 在专业任务（Agents' Last Exam 等）上**以 1/10 的单 token 价格追平 GPT-6 Astra**
- 平均完成任务速度**比 Astra 和 GPT-6.1 Sol 快 3 倍以上**
- 可用范围：API、ChatGPT Work 和 Codex（Plus / Pro / Enterprise / Edu）

---

## 2. 端点支持矩阵

| Endpoint | Route | 支持 |
|---|---|---|
| Chat Completions | `v1/chat/completions` | ✅ **支持（但工具调用必须走 Responses）** |
| Responses | `v1/responses` | ✅ **支持（推荐）** |
| Batch | `v1/batch` | ✅ 支持 |
| Live | `v1/live/sessions` | ❌ |
| Realtime | `v1/realtime` | ❌ |
| Realtime translation / transcription | `v1/realtime/*` | ❌ |
| Assistants | `v1/assistants` | ❌（已于 2026-08-26 下线） |
| Fine-tuning | `v1/fine-tuning` | ❌ |
| Embeddings / Images / Videos / Audio / Moderation / Completions | — | ❌ |

**端点能力差异**与 Astra 相同（详见 Astra 文档 §2）：工具调用、内置工具、后台任务、WebSocket 中途转向、会话托管等都是 Responses 专属；Chat Completions 只适合纯文本/图片输入的对话。

**官方迁移指引原文**：
> When migrating from GPT-5.6 Terra, compare both models on representative images and workflows using the same reasoning effort. If your application calls tools through Chat Completions, migrate those requests to the Responses API.

---

## 3. 定价（含 30m/12h 缓存写入价）

**官方文档**：https://developers.openai.com/api/docs/pricing

单位：美元 / 1M tokens。短/长上下文分界线为 **272K 输入 tokens**。

### Standard（标准）

| 档位 | Input | Cached input | 30m write | 12h write | Output |
|---|---|---|---|---|---|
| 短上下文（≤272K） | $1.00 | **$0.05** | $1.25 | $2.50 | $5.00 |
| 长上下文（>272K） | $2.00 | $0.10 | $2.50 | $5.00 | $7.50 |

### Batch / Flex（50% 折扣）

| 档位 | Input | Cached input | 30m write | 12h write | Output |
|---|---|---|---|---|---|
| 短上下文 | $0.50 | $0.025 | $0.625 | $1.25 | $2.50 |
| 长上下文 | $1.00 | $0.05 | $1.25 | $2.50 | $3.75 |

### Fast mode（2x 价格）

| 档位 | Input | Cached input | 30m write | 12h write | Output |
|---|---|---|---|---|---|
| 短上下文 | $2.00 | $0.10 | $2.50 | $5.00 | $10.00 |
| 长上下文 | $4.00 | $0.20 | $5.00 | $10.00 | $15.00 |

### 计费规则要点（官方原文翻译）

- **Cached input 是未缓存 input 价的 5%**（0.05×）。⚠️ 这是 Terra 和 Sol 独享的折扣，GPT-6 家族其他模型（Astra/Sol 之外的 5.6+ 模型）cached input 是 0.1×。
- **Cache writes 不是额外附加费**：input token 只会按「未缓存价 / 缓存命中价 / 缓存写入价」三者之一计费。
  - 30 分钟档写入 = 未缓存 input 价 × **1.25**
  - 12 小时档写入 = 未缓存 input 价 × **2.5**（两者都是总价倍率，不是加价）
- 输入 tokens 超过 272K 时，**整个请求**（不是超出部分）按 2x input/cache 费率、1.5x output 费率计费。
- Batch 和 Flex 是 Standard 的 50%；Fast mode 是 2x；区域处理（data residency）端点 +10%（2026-03-05 后发布的模型）。
- **推理 tokens 不可见，但占用上下文窗口，并按 output tokens 计费。**

### 与同家族模型价格对比（Standard，短上下文）

| 模型 | Input | Cached input | 30m write | 12h write | Output |
|---|---|---|---|---|---|
| gpt-6-astra | $10.00 | $1.00 | $12.50 | $25.00 | $50.00 |
| gpt-6.1-sol | $2.00 | $0.10 | $2.50 | $5.00 | $10.00 |
| **gpt-6.1-terra** | **$1.00** | **$0.05** | **$1.25** | **$2.50** | **$5.00** |
| gpt-6-luna | $0.10 | $0.01 | $0.125 | $0.25 | $0.50 |
| gpt-5.6-terra（前任） | $2.00 | $0.20 | $2.50 | $5.00 | $12.00 |

> Terra 对 5.6-Terra：input -50%、output -58%、cached input -75%。缓存命中率高的场景收益比表面降价更大。

---

## 4. 快速开始

### 4.1 `/v1/responses`（推荐）

```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6.1-terra",
    "input": "用一句话解释什么是向量数据库",
    "reasoning": { "effort": "medium" },
    "max_output_tokens": 2048
  }'
```

**多模态（文本 + 图片）输入**：
```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6.1-terra",
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

**带 12 小时缓存的请求**（详见 §8）：
```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6.1-terra",
    "prompt_cache_options": { "mode": "explicit", "ttl": "12h" },
    "input": [
      {
        "role": "developer",
        "content": [
          {
            "type": "input_text",
            "text": "至少 1024 tokens 的稳定指令与参考资料……",
            "prompt_cache_breakpoint": { "mode": "explicit", "ttl": "12h" }
          }
        ]
      },
      { "role": "user", "content": "当前问题（不写缓存，不产生写入费）……" }
    ]
  }'
```

### 4.2 `/v1/chat/completions`

```bash
curl https://api.openai.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-6.1-terra",
    "messages": [
      { "role": "developer", "content": "你是一个简洁的技术助手。" },
      { "role": "user", "content": "用一句话解释什么是向量数据库" }
    ],
    "reasoning_effort": "medium",
    "max_completion_tokens": 2048
  }'
```

> ⚠️ Chat Completions 用 `max_completion_tokens`（不要用已废弃的 `max_tokens`），Responses 用 `max_output_tokens`，最小值 16、建议预留 ≥25000（推理 token 不可见但计入上限）。
> `reasoning_effort: "none"` 在 Terra 上是**合法值**（Astra 会 400），适合延迟敏感的轻任务。

Python / Node.js / Go SDK 示例与 Astra 文档 §4 完全一致，把 `model` 换成 `gpt-6.1-terra` 即可。

---

## 5. 与 GPT-6 Astra / GPT-6.1 Sol 的关键差异

| 维度 | GPT-6 Astra | GPT-6.1 Sol | **GPT-6.1 Terra** |
|---|---|---|---|
| 定位 | 最强旗舰 | 均衡（近旗舰） | **快 + 省，视觉/屏幕任务主力** |
| `reasoning.effort: none` | ❌ 不支持 | ❌ 不支持 | ✅ **支持** |
| effort 默认值 | medium | medium | medium |
| 知识截止 | 2026-04-30 | — | 2026-06-01 |
| Predicted Outputs（`prediction`） | ✅ | — | ❌ **不支持** |
| Multi-agent（Responses beta） | — | ✅ | ❌ 未开放 |
| Fast mode 欧盟数据驻留 | ❌ 不支持 | ✅ | ✅ **支持** |
| Ultrafast mode | ✅（US/全球） | ✅（US/EU/全球） | ❌ |
| Misalignment monitoring | ✅ | — | ❌（有独立系统卡附录） |
| Cached input 折扣 | 0.1× | **0.05×** | **0.05×** |
| 温度类参数（`temperature`/`top_p`/`top_logprobs`/`logprobs`） | ❌ 必须删除 | ❌ | ⚠️ **effort ≠ `none` 时必须删除**（见 §6） |
| 上下文 / 最大输入 / 最大输出 | 1.05M / 922K / 128K | — | 1.05M / 922K / 128K |
| 工具调用 | 仅 Responses | 仅 Responses | 仅 Responses |

**选择建议**（官方口径）：深度研究、复杂编码选 Sol 或 Astra；高吞吐的视觉分析 + 界面操作 + 日常文本工作选 Terra；Terra 负责 Astra 规划出来的大任务里「量大、屏幕级」的执行部分，价格只有 Astra 的 1/10。

---

## 6. 专属限制（踩坑必读）

来源：官方 [Using GPT-6](https://developers.openai.com/api/docs/guides/latest-model) + 模型卡 + Changelog。

| # | 限制 | 后果 | 处理方式 |
|---|---|---|---|
| 1 | **工具调用必须走 `/v1/responses`** | CC 带 `tools` 会 400 | 迁到 Responses（与 Astra 相同） |
| 2 | **effort ≠ `none` 时**不支持 `temperature` / `top_p` / `top_logprobs`（CC 还有 `logprobs`） | HTTP 400 | 从请求体删除；`effort: "none"` 时才可带 |
| 3 | **不支持 Predicted Outputs**（`prediction` 参数） | — | 重写文件加速场景换 Sol/Astra |
| 4 | **不支持 Multi-agent** | — | 子代理编排用 Sol |
| 5 | `max_output_tokens` 最小 16 | 400 | 建议 ≥25000；探活请用 `GET /v1/models` |
| 6 | 输入 >272K 整请求 2x/1.5x 计费 | 账单翻倍 | 长上下文场景先做预算评估 |
| 7 | 流式 `include_usage` 中断时收不到 usage chunk | 计费缺口 | 网关侧做用量兜底（同 Astra） |
| 8 | 缓存前缀 <1024 tokens 不可缓存 | 白付写入费/无命中 | 见 §8「最小可缓存长度陷阱」 |

### Azure 侧补充（若走 Foundry）

- `prompt_cache_options.ttl` **只有 `30m` 一个合法值**，传 `12h` 不被支持（见 §9）。
- PTU-M 部署不支持缓存断点、不返回 `cache_write_tokens`。
- CC 端断点支持 `text` / `image_url` / `input_audio` / `file` 块；Responses 端支持 `input_text` / `input_image` / `input_file` 块。

---

## 7. reasoning 推理控制

**官方文档**：https://developers.openai.com/api/docs/guides/reasoning

### 7.1 effort 取值

Responses 用 `reasoning.effort`，CC 用 `reasoning_effort`。Terra 独有优势是支持 `none`：

| Effort | 适用场景 |
|---|---|
| `none` | 延迟敏感、完全不需要推理的任务（分类、路由、快速检索）。**Terra 支持，Astra/Sol 不支持** |
| `low` | 需要工具调用、规划，但要兼顾速度和成本 |
| `medium`（默认） | 多数工作负载的平衡点 |
| `high` | 硬核推理、复杂调试 |
| `xhigh` | 深度研究、长时 Agent 任务，评估后值得才用 |
| `max` | 最复杂任务 |

官方建议：**从 `medium` 起步**，质量提升值得额外的延迟和 token 时再上调。

### 7.2 中途改推理强度（保留缓存）

在 `input` 数组里、下一条 user 消息之前插入 `configuration_update`，**请求级 `reasoning.effort` 保持不变**，这样 prompt 前缀不变、缓存继续命中（GPT-6 家族能力，注意核对兼容性限制）：

```json
{ "type": "configuration_update", "reasoning": { "effort": "high" } }
```

### 7.3 推理 token 与成本

- 推理 token 不可见、占上下文、按 output 价计费；初次实验至少预留 25000。
- 触顶返回 `status: "incomplete"` + `incomplete_details.reason: "max_output_tokens"`，**零可见输出也照常计费 input + reasoning**。

---

## 8. Prompt Caching 详解（OpenAI：30m + 12h）

**官方文档**：https://developers.openai.com/api/docs/guides/prompt-caching
12h 缓存于 2026-10-20 上线（Changelog）：*“You can now choose 12-hour prompt caching in the API... 12-hour cache writes cost 2.5x the input token price.”*

### 8.1 TTL 与价格

`prompt_cache_options.ttl` 选择缓存**最小**存活期（Responses API）：

| 值 | 最小存活期 | 写入费率 | 适用 |
|---|---|---|---|
| `"30m"`（默认） | 30 分钟 | 1.25× 未缓存 input 价 | 高频复用的会话与 Agent 步骤 |
| `"12h"` | 12 小时 | 2.5× 未缓存 input 价 | 跨较长时间间隔复用的稳定指令/参考资料 |

- 读价两种 TTL 相同：Terra 上都是 **0.05× input 价**。
- 这是**总写入倍率**，不是在 input 价上加价；OpenAI 可能保留更久，`"12h"` 不是删除期限。
- 不支持的 ttl 值返回 `400 invalid_request_error`。
- 每个断点可用 `prompt_cache_breakpoint.ttl` 单独覆盖请求级 ttl，**一个请求里可以混用两种生命周期**。

### 8.2 断点与模式

| 模式 | 行为 |
|---|---|
| `implicit`（默认） | 自动在「最近一条合规消息」末尾放一个断点（占 4 个写入槽中的 1 个，剩 3 个给显式断点） |
| `explicit` | 只用你显式标记的断点；一个都不标 = 完全不走缓存、不产生写入费 |

- 在内容块上加 `"prompt_cache_breakpoint": { "mode": "explicit" }` 标记断点；断点覆盖它之前的整个前缀。
- **每个请求最多写 4 个断点**；缓存匹配回看会话最近 50 个显式断点（implicit 还会看最近 20 条合规消息结尾等边界）。
- **最小可缓存前缀 1,024 tokens**（隐藏系统内容不计入）。
- `prompt_cache_key` 在 5.6+ 上不再影响路由，仅用于给不同客户/用户做**缓存账目隔离**（可选）。

### 8.3 混用 30m/12h 的计费规则（官方表格）

同一个请求内，每个 token 按**覆盖它的最长生命周期写入**计一次费：

| 请求前缓存状态 | 选择的断点 | 计费读取 | 30m 写入 | 12h 写入 |
|---|---|---|---|---|
| 无缓存 | 2,000 tok 处 `12h`，5,000 tok 处 `30m` | 0 | 3,000 | 2,000 |
| 无缓存 | 2,000 tok 处 `30m`，5,000 tok 处 `12h` | 0 | 0 | 5,000（后面的 12h 把前面也带上） |
| 30m 覆盖到 2,000 tok | 5,000 tok 处 `12h` | 0 | 0 | 5,000 |
| 12h 覆盖到 2,000 tok | 5,000 tok 处 `30m` | 2,000 | 3,000 | 0 |

- **30m 升 12h 要重新付一次 12h 写入费**，不抵扣之前的 30m 写入。
- 30m 读取会把覆盖刷新为「读后至少 30 分钟」；命中的是 12h 条目时保留原有更晚到期时间，不重启 12h 时钟、不重复收费。

### 8.4 用量字段与成本计算

```jsonc
{
  "usage": {
    "input_tokens": 12000,
    "input_tokens_details": {
      "cached_tokens": 4000,
      "cache_write_tokens": 7000,
      "cache_write_tokens_details": {
        "ttl_30m_tokens": 3000,
        "ttl_12h_tokens": 4000
      }
    }
  }
}
```

- `cached_tokens` 是计费读取；`cache_write_tokens` 拆成 `ttl_30m_tokens` + `ttl_12h_tokens`（两字段不重叠、和等于总量，为 0 也返回）。
- 普通 input = `input_tokens - cached_tokens - cache_write_tokens` = 1,000。
- **成本公式**（Terra 短上下文价）：`普通×$1 + cached×$0.05 + ttl_30m×$1.25 + ttl_12h×$2.50`，除以 1M。
  上例 = `(1000 + 4000×0.05 + 3000×1.25 + 4000×2.5) / 1M = $0.01515`（按 $1 input 价）。

### 8.5 其他实用能力

- **预热（prewarm）**：`prompt_cache_options.prewarm: true` 只写缓存不生成输出，适合启动时预热共享指令/工具定义，随后发真实请求（同前缀）。写入按所选 TTL 计费。
- **最小可缓存长度陷阱**：共享前缀不足 1,024 tokens 时，与其短着白付 uncached 价，不如扩充到 1,024 换取缓存。按 0.05 读率、1.25 写率，10 次请求时原前缀 ≥221 tokens 就值得扩到 1,024。
- **诊断**：`prompt_cache_diagnostics`（GA）+ `comparison_response_id` 可对比上一次 response 找 cache miss 原因。
- **保前缀改 effort**：用 `configuration_update`（§7.2），别动请求级 `reasoning.effort`。
- 影响前缀的设置：`model`、`tools`（定义/顺序/schema）、`parallel_tool_calls`、`text.format`、`text.verbosity`、`reasoning.effort`、`context_management`（compaction 会破坏复用）。

---

## 9. Azure Foundry 接入（只支持 30m！）

**官方公告**：[GPT-6.1 Terra now generally available in Microsoft Foundry](https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/gpt-6-1-terra-now-generally-available-in-microsoft-foundry-build-agents-that-see/4567769)（2026-10-26）
**官方文档**：[Prompt caching with Azure OpenAI in Microsoft Foundry](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/prompt-caching)

### 9.1 部署形态

- **Global Standard**：所有支持区域，day one 可用。
- **Data Zone Standard**：US / EU / APAC。
- 通过 **Responses API** 接入，**无新 SDK 要求、无 schema 变化**；`model` 填 Azure 部署名（同 OpenAI 请求结构）。

### 9.2 Azure 定价（USD / 1M tokens）

| 部署类型 | 档位 | Input | Cached Input | **Cached Writes** | Output |
|---|---|---|---|---|---|
| Global Standard | 短上下文 | $1.00 | $0.05 | $1.25 | $5.00 |
| Global Standard | 长上下文 | $2.00 | $0.10 | $2.50 | $7.50 |
| Data Zone Standard (US) | 短 / 长 | $1.10 / $2.20 | $0.055 / $0.11 | $1.375 / $2.75 | $5.50 / $8.25 |
| Data Zone Standard (EU) | 短 / 长 | $1.20 / $2.40 | $0.06 / $0.12 | $1.50 / $3.00 | $6.00 / $9.00 |
| Data Zone Standard (APAC) | 短 / 长 | $1.20 / $2.40 | $0.06 / $0.12 | $1.50 / $3.00 | $6.00 / $9.00 |

> 注意：Azure 表里的 "Cached Writes" 是**单一档**（短 $1.25 / 长 $2.50），长的 $2.50 只是长上下文 2x 倍率，**不是 12h 价**。

### 9.3 ⚠️ Azure 缓存能力与 OpenAI 的差异（官方原文）

> **Set `prompt_cache_options.ttl` to `30m` to configure the minimum cache lifetime for all breakpoints in the request. The `30m` value is the default and the only supported value.**

| 能力 | OpenAI API | Azure OpenAI（Foundry） |
|---|---|---|
| 隐式/显式缓存模式（`prompt_cache_options.mode`） | ✅ | ✅ |
| 缓存写入计费（`cache_write_tokens`） | ✅ | ✅（Standard pay-as-you-go） |
| **TTL 30m** | ✅ 默认 | ✅ 默认 |
| **TTL 12h（2.5× 写入）** | ✅（2026-10-20 起，Responses） | ❌ **不支持，`30m` 是唯一合法值** |
| `cache_write_tokens_details`（30m/12h 拆分） | ✅ | ❌ 未提供（只有总量 `cache_write_tokens`） |
| 断点位置（Responses） | `input_text` 等 | ✅ `input_text` / `input_image` / `input_file` |
| 断点位置（Chat Completions） | — | ✅ `text` / `image_url` / `input_audio` / `file` |
| `prompt_cache_key` | 仅账目隔离（5.6+） | ✅ 仍用于提升缓存匹配（≤15 RPM/前缀） |
| PTU 部署 | — | 缓存读**最高 100% 折扣**；PTU-M **不支持断点、不返回 `cache_write_tokens`** |
| 关闭缓存 | `mode: "explicit"` 且不标断点 | 同左（仅 Standard 部署） |

**其他 Azure 要点**：

- 缓存最小 1,024 tokens，前 1,024 tokens 必须完全一致；首个差异字符 = 整体 miss（`cached_tokens: 0`）。
- 5.6+ 模型缓存读取按精确边界报告，不再 128-token 取整。
- `prompt_cache_retention`（`in_memory`/`24h`）只适用于 5.6 之前的老模型，在 5.6+ 上已废弃，传了会怎样官方未承诺——**网关侧应剥离**。
- 缓存不跨订阅共享；Data Zone 内数据留在 zone 边界内。

---

## 10. OpenAI vs Azure 缓存能力对照表

一句话总结：**OpenAI 官方支持缓存创建 30m（1.25×）和 12h（2.5×）两档；Azure 只支持 30m 一档，没有 12h。**

| 场景 | OpenAI | Azure |
|---|---|---|
| 会话内高频复用 | `ttl: "30m"`，写 1.25× | 默认行为，写 $1.25 |
| 跨小时复用稳定指令 | `ttl: "12h"`，写 2.5×，读仍 0.05× | **做不到**：只有 30m，到期后需重写（再付一次 1.25×） |
| 混用两种生命周期 | 请求级 30m + 断点级 12h 覆盖 | ❌ |
| 网关透传 `prompt_cache_options.ttl: "12h"` | 正常计费 | **不被支持（唯一合法值 30m）** |
| 用量对账 | `ttl_30m_tokens` / `ttl_12h_tokens` 分开 | 只有 `cache_write_tokens` 总量 |

**对账/计费侧影响**：同一份「30m 写 + 12h 写」的用量从 OpenAI 渠道走和从 Azure 渠道走，账面结构不同——OpenAI 渠道要按两个 TTL 档分别乘 1.25×/2.5×，Azure 渠道只有一个写入价。网关侧若统一按 `cache_write_tokens × 1.25` 计费，**OpenAI 渠道的 12h 写入会少算一半**。

---

## 11. 网关（gravitex-api）侧接入建议

代码现状（main-alpha，2026-10-27）：`gpt-6.x` 系列模型（astra/sol/terra/luna）在计费配置里**均未登记**，目前只有 `gpt-5.6-*` 家族。接入 `gpt-6.1-terra` 需要动以下几处：

### 11.1 计费配置

| 文件 | 现状（gpt-5.6-terra） | 建议（gpt-6.1-terra） |
|---|---|---|
| `setting/ratio_setting/model_ratio.go` | `1.25`（= $2 input，基准 $2/1M） | `0.5`（= $1 input） |
| completion ratio（同文件） | 6.0（$12/$2） | `5.0`（$5/$1） |
| `setting/ratio_setting/cache_ratio.go`（cached） | `0.1` | **`0.05`**（Terra/Sol 独享 5% 读价） |
| `setting/ratio_setting/cache_ratio.go`（write） | `1.25` | `1.25`（30m）；**12h 需 2.5**（见下） |

### 11.2 12h 缓存写入计费（新增点）

- 上游 `usage.input_tokens_details.cache_write_tokens_details` 会返回 `ttl_30m_tokens` / `ttl_12h_tokens` 两档；**统一按 1.25× 计费会把 12h 写入少算一半**。
- 需要在 usage 解析层把两档拆开，分别乘 1.25× / 2.5×。可参考 `f91d0cb45`（wpr 记录动态缓存计费 TTL）的既有思路——本仓库已有按 TTL 区分缓存计费的先例。
- 短/长上下文分档（>272K：input/cache 2x、output 1.5x）走既有的分档表达式计费引擎（`pkg/billingexpr`，改动前必读 `expr.md`）。

### 11.3 模型清单与目录

- `relay/channel/codex/constants.go` 的 `ModelList`：按需追加（是否在 codex 渠道暴露取决于运营口径）。
- `service/codex_catalog.go`：参照既有条目补 `"gpt-6.1-terra": {ContextWindow: 272000, MaxContextWindow: 872000}`（catalog 的窗口口径与 OpenAI 标称 1.05M 不是一回事，见该文件注释）。

### 11.4 Azure 渠道的 12h 防呆

- Azure 渠道收到/注入 `prompt_cache_options.ttl: "12h"` 会不被支持（唯一合法值 `30m`）。网关侧应：
  - 不给 Azure 渠道**默认注入** 12h；
  - 透传客户端请求时，若渠道是 Azure 且 ttl 为 `12h`，**剥离或降级为 `30m`**（并记日志），避免整批 400。
- Azure 的 `prompt_cache_retention` 已废弃，透传时一并剥离（老模型专用参数）。
- Azure 用量响应没有 `cache_write_tokens_details`，对账逻辑要容忍字段缺失（按 1.25× 单档兜底即可，与 Azure 计费口径一致）。

### 11.5 参数校验沿用 Astra 的坑

- 不要给请求默认注入 `temperature` / `top_p`（effort ≠ none 时 400）。
- `max_tokens` → CC 映射 `max_completion_tokens` / Responses 映射 `max_output_tokens`，最小 16。
- CC 请求带 `tools` 且 model 是 `gpt-6.1-terra` → 明确报错提示走 Responses。
- Terra 支持 `reasoning_effort: "none"`——**不要沿用 Astra 的「none 非法」拦截逻辑**（若渠道配置复用了 Astra 的校验规则要放开）。

---

## 12. 从 GPT-5.6 Terra 迁移清单

- [ ] `model` 设为 `gpt-6.1-terra`（无日期快照，行为随官方滚动更新）
- [ ] **相同 reasoning effort 下**在代表性图片和工作流上对比新旧模型（官方建议）
- [ ] 工具调用在 CC 上的请求 → 迁到 Responses
- [ ] effort 为默认/非 none 时确认请求里没有 `temperature`/`top_p`/`top_logprobs`/`logprobs`
- [ ] cached input 从 0.1×（$0.20）降到 0.05×（$0.05），缓存命中率高的业务重新核算成本
- [ ] 如需 12h 缓存：确认走 OpenAI 渠道（Azure 不支持），并在计费侧拆 `ttl_12h_tokens`
- [ ] 若从 GPT-5.5 及更早迁：`prompt_cache_retention` → `prompt_cache_options.ttl: "30m"`
- [ ] 输入 >272K 的场景成本翻倍（input/cache 2x、output 1.5x），重新预算
- [ ] 灰度前对比行为、延迟、token 用量与错误率

---

## 13. 官方文档链接汇总

| 主题 | 链接 |
|---|---|
| **GPT-6.1 Terra 模型页** | https://developers.openai.com/api/docs/models/gpt-6.1-terra |
| GPT-6 家族指南（含 Terra 章节） | https://developers.openai.com/api/docs/guides/latest-model |
| 发布博客 | https://openai.com/index/introducing-gpt-6-1-terra/ |
| 系统卡附录 | https://deploymentsafety.openai.com/gpt-6-1-terra |
| **Prompt caching 指南** | https://developers.openai.com/api/docs/guides/prompt-caching |
| 缓存诊断 | https://developers.openai.com/api/docs/guides/prompt-caching/diagnostics |
| **定价** | https://developers.openai.com/api/docs/pricing |
| Changelog | https://developers.openai.com/api/docs/changelog |
| **Azure Foundry GA 公告** | https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/gpt-6-1-terra-now-generally-available-in-microsoft-foundry-build-agents-that-see/4567769 |
| **Azure prompt caching 文档** | https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/prompt-caching |
| Azure 定价 | https://azure.microsoft.com/en-us/pricing/details/azure-openai/ |
| reasoning 指南 | https://developers.openai.com/api/docs/guides/reasoning |
| 社区发布公告 | https://community.openai.com/t/gpt-6-1-terra-delivering-fast-and-capable-computer-use-at-a-lower-price/1409900 |

---

## 14. 一分钟自检清单

接入 `gpt-6.1-terra` 前，逐条确认：

- [ ] 需要工具调用的请求走的是 `/v1/responses`
- [ ] effort ≠ `none` 时请求体里没有 `temperature`、`top_p`、`top_logprobs`、`logprobs`
- [ ] `max_output_tokens` / `max_completion_tokens` ≥ 16（实际建议 ≥ 25000）
- [ ] 需要跨小时缓存复用的前缀给了 `ttl: "12h"`（仅 OpenAI 渠道），且预算按 2.5× 写入价评估
- [ ] Azure 渠道的请求没有携带 `ttl: "12h"`
- [ ] 计费侧把 `ttl_30m_tokens` 和 `ttl_12h_tokens` 分开计价（OpenAI 渠道）
- [ ] 缓存前缀 ≥ 1,024 tokens，且稳定内容在前、变化内容在后
- [ ] 输入 >272K 的场景已按 2x/1.5x 重算成本
- [ ] 用 `prompt_cache_key` 做客户级缓存账目隔离（可选）
- [ ] 日志记录 request id，不记录 API key
