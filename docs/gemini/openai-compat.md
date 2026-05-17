# 用 OpenAI 兼容接口调用 Gemini（对话 + 生图）

> 路径：
> - `POST /v1/chat/completions`：所有 Gemini 文本/多模态对话模型 + Gemini imagine 系列（nano banana 等，可同时返回文本 + 图片）——本文档主体
> - `POST /v1/images/generations` 与 `POST /v1/images/edits`：Imagen 系列 + Gemini imagine 系列——详见独立文档 [`imagine-images-generations.md`](./imagine-images-generations.md)；本文档 §11 仅做摘要
>
> 关键文件：
> - 请求转换（chat/completions）：`relay/channel/gemini/relay-gemini.go` → `CovertOpenAI2Gemini`
> - 请求转换（images/generations）：`relay/channel/gemini/adaptor.go` → `ConvertImageRequest` → `convertImagenRequest` / `convertImagineImageRequest`
> - 响应转换：`relay/channel/gemini/relay-gemini.go` → `responseGeminiChat2OpenAI` / `GeminiChatHandler` / `GeminiChatStreamHandler` / `GeminiImageHandler`(imagen) / `GeminiImagineImageHandler`(nano banana)
> - 用量计算：`relay/channel/gemini/relay-gemini.go` → `buildUsageFromGeminiMetadata`
> - imagine 模型清单：`setting/model_setting/gemini.go` → `SupportedImagineModels`

---

## 1. 模型分类

| 类别 | 代表模型 | 走法 | 特殊行为 |
| --- | --- | --- | --- |
| 普通对话/多模态 | `gemini-2.5-pro`、`gemini-2.5-flash`、`gemini-2.0-flash`、`gemini-1.5-pro` 等 | `:generateContent` 或 `:streamGenerateContent` | 按客户端 `stream` 字段决定是否流式 |
| imagine 生图（可对话） | `gemini-3-pro-image-preview`、`gemini-3.1-flash-image-preview`、`gemini-2.5-flash-image`、`gemini-2.0-flash-exp-image-generation`、`gemini-2.0-flash-exp` | 仅 `:generateContent`（**官方不支持流式**） | 自动注入 `generationConfig.responseModalities=["TEXT","IMAGE"]`，并且**强制把 `stream:true` 降级为非流式** |

> `IsGeminiModelSupportImagine(model)` 命中即视为 imagine 模型，名单由 `setting/model_setting/gemini.go` 中 `SupportedImagineModels` 维护，可在管理后台 `Gemini 设置 → 支持图片生成的模型列表` 修改。

---

## 2. 请求路径与认证

```
POST {base}/v1/chat/completions
Authorization: Bearer sk-<your-token>
Content-Type: application/json
```

---

## 3. 标准 OpenAI 字段 → Gemini 映射

| OpenAI 字段 | Gemini 字段 | 说明 |
| --- | --- | --- |
| `model` | URL path 中的 `models/<model>` | 渠道未做模型映射时即原样上送 |
| `messages` | `contents[]` + `systemInstruction` | `system`/`developer` 角色合并到 `systemInstruction`；`assistant` → `role:"model"`；`tool`/`function` → `functionResponse` |
| `stream` | URL `:streamGenerateContent` vs `:generateContent` | imagine 模型 `stream:true` 会**被强制改成 false** |
| `temperature` | `generationConfig.temperature` | |
| `top_p` | `generationConfig.topP` | |
| `max_tokens` / `max_completion_tokens` | `generationConfig.maxOutputTokens` | |
| `seed` | `generationConfig.seed` | |
| `stop` | `generationConfig.stopSequences` | 最多 5 个，超出会截断 |
| `response_format.type = "json_schema"/"json_object"` | `generationConfig.responseMimeType = "application/json"` + `responseSchema` | `additionalProperties` 等 Gemini 不识别的字段会被自动剔除 |
| `tools` 中的 `function` | `tools[].functionDeclarations` | 见 §4 |
| `tools` 中三个特殊名 (`googleSearch` / `codeExecution` / `urlContext`) | `tools[].googleSearch` / `tools[].codeExecution` / `tools[].urlContext` | 见 §4 |
| `tool_choice` | `toolConfig.functionCallingConfig` | `"auto"→AUTO`、`"none"→NONE`、`"required"→ANY`；对象形式 `{type:"function",function:{name:"X"}}` → `ANY` + `allowedFunctionNames=["X"]` |

`messages.content` 支持多模态数组（OpenAI v2）：
- `type:"text"` → `parts[].text`
- `type:"image_url"` / `type:"input_audio"` / `type:"file"` → 由系统下载/解码后塞进 `parts[].inlineData`，并按 MIME 校验白名单：
  - 图片：`image/png`、`image/jpeg`、`image/jpg`、`image/webp`、`image/heic`、`image/heif`
  - 音频：`audio/mpeg`、`audio/mp3`、`audio/wav`
  - 视频：`video/mp4`、`video/mov`、`video/mpeg`、`video/mpg`、`video/avi`、`video/wmv`、`video/mpegps`、`video/flv`
  - 文档：`application/pdf`、`text/plain`
- `content` 字符串里夹的 `![alt](data:image/...;base64,...)` Markdown 图片会被识别并拆成独立 `inlineData` part（与 `type:"image_url"` 等价）。

---

## 4. tools 透传

```jsonc
"tools": [
  { "type": "function", "function": { "name": "googleSearch" } },     // 启用谷歌搜索
  { "type": "function", "function": { "name": "codeExecution" } },    // 启用代码执行
  { "type": "function", "function": { "name": "urlContext" } },       // 启用 URL 上下文
  { "type": "function", "function": {                                  // 普通 function calling
      "name": "get_weather",
      "description": "查天气",
      "parameters": { "type": "object", "properties": { "city": { "type": "string" } }, "required": ["city"] }
    }
  }
]
```

三个特殊名称会被识别成 Gemini 原生工具开关，其它 `function` 走标准 functionDeclarations。

---

## 5. extra_body — 完全透传 Gemini 原生参数

`GeneralOpenAIRequest.ExtraBody` 是 `json.RawMessage`，全部位于 `extra_body.google.*` 命名空间下。

```jsonc
{
  "model": "gemini-2.5-flash-image",
  "messages": [{ "role": "user", "content": "画一只柴犬" }],
  "extra_body": {
    "google": {
      "generationConfig": { /* ... */ },
      "safetySettings":   [ /* ... */ ],
      "tools":            [ /* ... */ ],
      "systemInstruction": { /* ... */ },
      "thinking_config":  { /* ... */ },
      "image_config":     { /* ... */ }
    }
  }
}
```

### 5.1 两条透传路径

| 路径 | 字段 | 行为 |
| --- | --- | --- |
| ① **snake_case 白名单（兼容旧版）** | `extra_body.google.thinking_config`、`extra_body.google.image_config` | 由 `CovertOpenAI2Gemini` 显式解析后写入对应结构体字段。**只接受 snake_case key**，类型不匹配会被静默跳过（不报错），交由 ② 兜底。 |
| ② **完全透传（无 schema 校验）** | `extra_body.google.*` 下**除 ① 外**的任意字段 | 把整个 `extra_body.google` 子树（剔除 `thinking_config`、`image_config`）**深度合并**到最终发给 Gemini 的请求 JSON。字段名按 **Gemini 官方原生 camelCase** 书写（`generationConfig`、`safetySettings`、`tools`、`systemInstruction`、`toolConfig`、`cachedContent`、`responseModalities`、`responseSchema`、`responseJsonSchema` 等等）。 |

### 5.2 thinking_config（snake_case 白名单）

| 字段 | 类型 | Gemini 字段 | 说明 |
| --- | --- | --- | --- |
| `thinking_budget` | int | `thinkingBudget` | thinking token 预算。> 0 时自动把 `include_thoughts` 设为 true；= 0 或负数禁用思考 |
| `include_thoughts` | bool | `includeThoughts` | 是否返回思考过程（`reasoning_content`） |
| `thinking_level` | string | `thinkingLevel` | 思考级别（如 `"HIGH"`） |

> 只要传了 `extra_body.google`，自动 `ThinkingAdaptor` 会关闭，全部 thinking 行为由调用方掌控。

### 5.3 image_config（snake_case 白名单）

| 字段 | 类型 | Gemini 字段 | 说明 |
| --- | --- | --- | --- |
| `aspect_ratio` | string | `aspectRatio` | 宽高比，如 `"1:1"`、`"16:9"`、`"9:16"` |
| `image_size` | string | `imageSize` | 图片尺寸，如 `"1K"`、`"2K"` |

### 5.4 完全透传合并规则

* 把 `extra_body.google`（剔除上面 2 个 snake_case key）作为 patch。
* 把已经构造好的 `GeminiChatRequest` 结构体 Marshal 成 `map[string]any` 作为 base。
* **deep merge**：
  * 相同 key 两边都是 map → 递归合并；
  * 其它类型（标量、数组、null）→ patch 直接覆盖 base；
  * patch 中不存在的 key 保留 base 原值。
* 合并后用 `map[string]any` 作为上游请求体发送，效果等同于"原生 Gemini 调用"。

> 含义：你可以用 `extra_body.google.generationConfig.maxOutputTokens` 覆盖通过 OpenAI 字段 `max_tokens` 设置的值，也可以用 `extra_body.google.safetySettings` 完全替换平台默认安全设置，新增 Gemini 字段（如未来上线的字段）无需改代码即可直接使用。

### 5.5 透传示例（imagine 模型完整原生请求）

```json
{
  "model": "gemini-3.1-flash-image-preview",
  "messages": [
    { "role": "user", "content": [
        { "type": "text", "text": "来一张甄姬拔菜图" },
        { "type": "image_url", "image_url": { "url": "data:image/jpeg;base64,/9j/4AAQ..." } }
      ]
    }
  ],
  "extra_body": {
    "google": {
      "generationConfig": {
        "temperature": 1,
        "topP": 0.95,
        "maxOutputTokens": 32768,
        "responseModalities": ["TEXT", "IMAGE"],
        "imageConfig": { "aspectRatio": "1:1", "imageSize": "" }
      },
      "safetySettings": [
        { "category": "HARM_CATEGORY_HATE_SPEECH",       "threshold": "OFF" },
        { "category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF" },
        { "category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF" },
        { "category": "HARM_CATEGORY_HARASSMENT",        "threshold": "OFF" }
      ]
    }
  }
}
```

---

## 6. imagine 模型的隐式行为

| 行为 | 触发条件 | 说明 |
| --- | --- | --- |
| **自动注入 `responseModalities`** | model ∈ `SupportedImagineModels` | 把 `generationConfig.responseModalities = ["TEXT","IMAGE"]` 写入请求（仍可通过 `extra_body.google.generationConfig.responseModalities` 覆盖） |
| **强制非流式** | model ∈ `SupportedImagineModels` 且客户端传 `stream:true` | 自动把 `stream`、`stream_options` 清空，上游走 `:generateContent`，下游走 `GeminiChatHandler`，对客户端透明。Gemini 官方对 imagine 模型不支持 SSE 流式 |
| **图片输出格式** | candidate.Content.Parts 中含 `inlineData.mimeType` 以 `image/` 开头 | `message.content` 返回 OpenAI v2 多模态数组（见 §7.1） |

---

## 7. 响应格式

### 7.1 非流式 `chat.completion`

含图片时（**OpenAI v2 多模态数组**）：

```json
{
  "id": "sXIFar39H4K0694P2MWmWQ",
  "object": "chat.completion",
  "created": 1747299537,
  "model": "gemini-3.1-flash-image-preview",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": [
          { "type": "text",      "text": "好的，为您生成了一张..." },
          { "type": "image_url", "image_url": {
              "url": "data:image/png;base64,iVBORw0KGgo...",
              "mime_type": "image/png"
            }
          }
        ]
      },
      "finish_reason": "stop"
    }
  ],
  "usage": { /* 见 §8 */ }
}
```

不含图片时（**继续返回字符串**，向后兼容）：

```json
{
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "你好，有什么可以帮你？",
        "reasoning_content": "用户在打招呼..."
      },
      "finish_reason": "stop"
    }
  ]
}
```

* `id` = 上游 `responseId`（与日志 `request_id` 一致，便于根据响应 id 反查日志），上游缺 `responseId` 时回退到本地 `chatcmpl-<system_request_id>`。
* `reasoning_content`：上游 `parts[].thought=true` 时的文本内容（即思考过程，仅 `include_thoughts:true` 才会返回）。
* `executable_code` / `code_execution_result`：转成 markdown 代码块塞在 text 里。
* 非图片 `inlineData`（音频等）：仍以 markdown `[media](data:...)` 文本形式塞在 text part 里（避免 DTO 新字段）。
* `finish_reason` 映射：`STOP→stop`、`MAX_TOKENS→length`、`SAFETY/RECITATION/PROHIBITED_CONTENT/...→content_filter`、有 `functionCall` → `tool_calls`。

### 7.2 流式 `chat.completion.chunk`

* 流式响应中 `delta.content` 是字符串（不是数组）。
* 图片以 `![image](data:image/png;base64,...)` markdown 形式嵌入 `delta.content`（流式 DTO 限制，未做 v2 改造）。
* `id` 采用**二步策略**：首个 chunk 用本地 `chatcmpl-*`；拿到上游 `responseId` 后改写并应用到后续所有 chunk + 末尾 usage 帧，保证全程一致。
* imagine 模型即使客户端要求 `stream:true` 也不会触发流式（被入口降级为非流式，见 §6）。

---

## 8. usage 用量

`response.usage` 完整字段：

```jsonc
{
  "prompt_tokens": 1127,         // = PromptTokenCount + ToolUsePromptTokenCount
  "completion_tokens": 2050,     // = CandidatesTokenCount + ThoughtsTokenCount（OpenAI 语义：reasoning 计入 completion）
  "total_tokens": 2273,          // 上游 totalTokenCount

  "prompt_tokens_details": {
    "cached_tokens": 0,          // = cachedContentTokenCount
    "text_tokens":   7,          // 按 promptTokensDetails[].modality=TEXT 累加
    "audio_tokens":  0,          // 按 promptTokensDetails[].modality=AUDIO 累加
    "image_tokens":  1120        // 按 promptTokensDetails[].modality=IMAGE 累加（IMAGE 分支新增）
  },

  "completion_tokens_details": {
    "text_tokens":      26,      // 按 candidatesTokensDetails[].modality=TEXT
    "audio_tokens":     0,
    "image_tokens":     1120,    // 按 candidatesTokensDetails[].modality=IMAGE
    "reasoning_tokens": 904      // = thoughtsTokenCount（思考输出，单独展示）
  }
}
```

### 8.1 思考 token 怎么算

* `reasoning_tokens` 单独显示 = `thoughtsTokenCount`。
* 但 `completion_tokens` **包含** `thoughtsTokenCount`（OpenAI 标准语义，扣费按 `completion_tokens`）。
* 即：`completion_tokens = candidatesTokenCount + thoughtsTokenCount`，与原生 Gemini 计费一致。

### 8.2 输出 token modality 归类（兜底规则）

上游若返回了 `candidatesTokensDetails`，按其中 modality 拆分即可。

上游**没返回** `candidatesTokensDetails` 时，按以下规则兜底：

| 实际 candidate.Content.Parts | image / audio / text 归类 |
| --- | --- |
| 出现过 `inlineData.mimeType` 以 `image/` 开头的 part | `image_tokens = candidatesTokenCount` |
| 没出现图片 part（即使模型名含 "image"） | `text_tokens = candidatesTokenCount` |

> **重要**：此前的旧逻辑是"模型名含 image 就全算图片"，会把 banana 等多模态模型的纯文本回答误按图片计费；现已改为基于 candidate 实际产出归类，纯文本响应按文本计费。

### 8.3 modality 大小写处理

所有 modality 匹配使用 `strings.EqualFold` + `strings.TrimSpace`，兼容上游可能返回的 `"image"` / `"IMAGE"` / `" Image "` 等写法。

---

## 9. 日志与对账

每个请求在响应阶段会打印一条 INFO 级日志，原文输出上游 `usageMetadata`，便于对账：

```
[INFO] gemini upstream usageMetadata (responseId=sXIFar39H4K0694P2MWmWQ, model=gemini-3.1-flash-image-preview):
{"promptTokenCount":1127,"candidatesTokenCount":1146,"totalTokenCount":2273,...,"thoughtsTokenCount":904}
```

* 非流式响应：在 `GeminiChatHandler` 解析完成后立即打印一次。
* 流式响应：在 `geminiStreamHandler` 收到 `TotalTokenCount != 0` 的 chunk（通常是末尾 usage 帧）时打印一次。
* `responseId` 与 `response.id` 完全一致，也与日志的 `request_id` 一致 → 三方都可以互相反查。

---

## 10. 完整调用示例

### 10.1 纯文本对话（开 thinking）

```bash
curl -X POST https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-pro",
    "messages": [{"role": "user", "content": "证明费马大定理"}],
    "extra_body": {
      "google": {
        "thinking_config": { "thinking_budget": 8192, "include_thoughts": true }
      }
    }
  }'
```

### 10.2 多模态输入（文本 + 图片 URL）

```bash
curl -X POST https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "这张图里有什么？" },
          { "type": "image_url", "image_url": { "url": "https://example.com/cat.jpg" } }
        ]
      }
    ]
  }'
```

### 10.3 启用 Google 搜索 + URL 上下文

```bash
curl -X POST https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "今天科技领域有什么大新闻？"}],
    "tools": [
      { "type": "function", "function": { "name": "googleSearch" } },
      { "type": "function", "function": { "name": "urlContext" } }
    ]
  }'
```

### 10.4 imagine 模型生图（含原生参数透传）

```bash
curl -X POST https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "把这张照片改成赛博朋克风格" },
          { "type": "image_url", "image_url": { "url": "data:image/jpeg;base64,/9j/4AAQ..." } }
        ]
      }
    ],
    "extra_body": {
      "google": {
        "generationConfig": {
          "temperature": 1,
          "responseModalities": ["TEXT", "IMAGE"],
          "imageConfig": { "aspectRatio": "16:9", "imageSize": "2K" }
        },
        "safetySettings": [
          { "category": "HARM_CATEGORY_HATE_SPEECH",       "threshold": "OFF" },
          { "category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF" },
          { "category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF" },
          { "category": "HARM_CATEGORY_HARASSMENT",        "threshold": "OFF" }
        ]
      }
    }
  }'
```

> imagine 模型即使写了 `"stream": true` 也会被强制改为非流式。

### 10.5 imagine 模型走 `/v1/images/generations`（详细规则见 [`imagine-images-generations.md`](./imagine-images-generations.md)）

```bash
curl -X POST https://api.example.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "一只穿着唐装在写春联的柴犬，水墨风格",
    "n": 1,
    "size": "1024x1024",
    "quality": "hd"
  }'
```

> 响应结构对齐 OpenAI Image API（`data[].b64_json` + 可选 `data[0].revised_prompt`），见 §11.3。

### 10.6 流式对话

```bash
curl -N -X POST https://api.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "写一首五言绝句"}],
    "stream": true,
    "stream_options": { "include_usage": true }
  }'
```

---

## 11. imagine 模型走 `/v1/images/generations` 和 `/v1/images/edits`

详见独立文档：[**imagine-images-generations.md**](./imagine-images-generations.md)

简要要点：

* nano banana 等 imagine 模型可直接通过 OpenAI 标准 `/v1/images/generations` 与 `/v1/images/edits` 入口调用（底层走 `:generateContent`），返回 OpenAI Image API 格式（`data[].b64_json` + `data[0].revised_prompt` + `metadata.text`）。
* `image` 字段（`string` / `[]string`，URL / 纯 base64 / data URI）作为图生图 / 图编辑的输入参考图。
* `n` 被忽略，按上游实际生成的图片张数计费（见独立文档 §7）。
* 该入口下**不接受** `extra_body`；要用 Gemini 原生 `imageConfig` / `safetySettings` / `thinking_config` 等全部能力，请改走 `/v1/chat/completions` 入口（见 §5）。

---

## 12. 已知限制

| 限制 | 说明 |
| --- | --- |
| imagine 模型不支持流式 | 客户端 `stream:true` 自动降级为非流式 |
| 流式响应中的图片用 markdown 嵌图 | `delta.content` 是 `*string`，未引入 v2 数组；客户端可解析 `![image](data:...)` 拿到 base64 |
| 非图片 `inlineData`（音频等）输出仍是 markdown 文本 | 当前未提供专门的 audio/video 多模态 part 类型 |
| `extra_body.google.*` 完全透传，**不做 schema 校验** | 字段写错（如 typo、值类型错误）会原样发给上游，由 Gemini 返回 4xx，调用方负责字段正确性 |
| 默认安全设置由平台 `setting/model_setting/gemini.go` → `SafetySettings` 决定 | 想关掉/调整请通过 `extra_body.google.safetySettings` 覆盖 |
| imagine 走 `/v1/images/generations` 时 `n` 被忽略 | 上游单次只出 1 张图，按实际生成张数计费（详见 [`imagine-images-generations.md`](./imagine-images-generations.md) §7） |

---

## 13. 调试 / 排错快速索引

| 问题 | 怎么查 |
| --- | --- |
| usage 看起来不对 | 翻日志找 `gemini upstream usageMetadata` 或 `gemini imagine images generations upstream usageMetadata`，原文核对 |
| `response.id` 和日志 `request_id` 对不上 | 不应再出现，若出现说明上游没回 `responseId`，本地用 `chatcmpl-*` 回退 |
| 图片为啥是 markdown 不是数组 | 检查是不是走的流式；非流式才会用 v2 数组（chat/completions 入口）；走 `/v1/images/generations` 永远是 `b64_json` |
| `extra_body.google.xxx` 没生效 | 1) `extra_body` 是 JSON，不能是字符串；2) `xxx` 是否落在 `google` 命名空间下；3) 是否在 §5.2/§5.3 的 snake_case 白名单里——白名单字段需要用 snake_case，其他字段用 Gemini 原生 camelCase |
| imagine 模型为什么没流式 | 见 §6，强制降级 |
| 不想用平台默认 safety | 在 `extra_body.google.safetySettings` 里完整覆盖 |
| `/v1/images/generations` 报 `not supported model for image generation` | 模型名既不以 `imagen` 开头，也不在 `SupportedImagineModels` 里——把它加进管理后台 imagine 模型清单，或换 chat/completions 入口 |

---

## 14. 代码锚点

修改点都打了 `CHZ-PATCH(<feature>)` 注释，搜索这些字符串即可定位：

| 锚点 | 功能 |
| --- | --- |
| `CHZ-PATCH(gemini-resp-id)` | `response.id` 用上游 `responseId` |
| `CHZ-PATCH(gemini-imagine-no-stream)` | imagine 模型强制非流式 |
| `CHZ-PATCH(gemini-usage-fix)` | usage 去重 + 模态归类 + 对账日志 |
| `CHZ-PATCH(gemini-image-content-v2)` | 非流式图片输出改 OpenAI v2 多模态数组 |
| `CHZ-PATCH(gemini-extra-body-passthrough)` | `extra_body.google.*` 完全透传 |
| `CHZ-PATCH(gemini-imagine-images-generations)` | imagine 模型支持 `/v1/images/generations` 入口（gemini + vertex 通道） |
