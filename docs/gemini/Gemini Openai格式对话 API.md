# 用 OpenAI 兼容接口调用 Gemini

> 路径：`POST https://api.gravitex.ai/v1/chat/completions`

---

## 1. 模型分类


| 类别              | 代表模型                                                                                                                       | 走法                                            | 特殊行为                                                                                       |
| --------------- | -------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- | ------------------------------------------------------------------------------------------ |
| 对话/多模态        | `gemini-3.5-flash`、`gemini-3.1-pro-preview`、`gemini-3-flash-preview`、`gemini-3.1-flash-lite-preview`                       | `:generateContent` 或 `:streamGenerateContent` | 按客户端 `stream` 字段决定是否流式                                                                     |


---

## 2. 请求路径与认证

```
POST https://api.gravitex.ai/v1/chat/completions
Authorization: Bearer sk-<your-token>
Content-Type: application/json
```

---

## 3. 标准 OpenAI 字段 → Gemini 映射


| OpenAI 字段                                                        | Gemini 字段                                                                   | 说明                                                                                                                               |
| ---------------------------------------------------------------- | --------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `model`                                                          | URL path 中的 `models/<model>`                                                | 模型名原样传递给 Gemini                                                                                                                  |
| `messages`                                                       | `contents[]` + `systemInstruction`                                          | `system`/`developer` 角色合并到 `systemInstruction`；`assistant` → `role:"model"`；`tool`/`function` → `functionResponse`               |
| `stream`                                                         | URL `:streamGenerateContent` vs `:generateContent`                          |                                                                                                                                  |
| `temperature`                                                    | `generationConfig.temperature`                                              |                                                                                                                                  |
| `top_p`                                                          | `generationConfig.topP`                                                     |                                                                                                                                  |
| `max_tokens` / `max_completion_tokens`                           | `generationConfig.maxOutputTokens`                                          |                                                                                                                                  |
| `seed`                                                           | `generationConfig.seed`                                                     |                                                                                                                                  |
| `stop`                                                           | `generationConfig.stopSequences`                                            | 最多 5 个，超出会截断                                                                                                                     |
| `response_format.type = "json_schema"/"json_object"`             | `generationConfig.responseMimeType = "application/json"` + `responseSchema` | `additionalProperties` 等 Gemini 不识别的字段会被自动剔除                                                                                     |
| `tools` 中的 `function`                                            | `tools[].functionDeclarations`                                              | 见 §4                                                                                                                             |
| `tools` 中三个特殊名 (`googleSearch` / `codeExecution` / `urlContext`) | `tools[].googleSearch` / `tools[].codeExecution` / `tools[].urlContext`     | 见 §4                                                                                                                             |
| `tool_choice`                                                    | `toolConfig.functionCallingConfig`                                          | `"auto"→AUTO`、`"none"→NONE`、`"required"→ANY`；对象形式 `{type:"function",function:{name:"X"}}` → `ANY` + `allowedFunctionNames=["X"]` |
| `frequency_penalty`                                              | `generationConfig.frequencyPenalty`                                         |                                                                                                                                  |
| `presence_penalty`                                               | `generationConfig.presencePenalty`                                          |                                                                                                                                  |
| `top_k`                                                          | `generationConfig.topK`                                                     |                                                                                                                                  |
| `n`                                                              | `generationConfig.candidateCount`                                           | 仅 `n > 1` 时生效，控制候选回答数量                                                                                                           |
| `logprobs`                                                       | `generationConfig.responseLogprobs`                                         | 是否返回 logprobs                                                                                                                    |
| `top_logprobs`                                                   | `generationConfig.logprobs`                                                 | top logprobs 数量                                                                                                                  |
| `modalities`                                                     | `generationConfig.responseModalities`                                       | JSON 数组（如 `["text","audio"]`）                                                                   |
| `audio`                                                          | `generationConfig.speechConfig`                                             | TTS 语音配置，直接透传给 Gemini speechConfig                                                                                               |


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

`extra_body.google.*` 命名空间下的所有字段都会透传给 Gemini 原生 API。

```jsonc
{
  "model": "gemini-3.5-flash",
  "messages": [{ "role": "user", "content": "画一只柴犬" }],
  "extra_body": {
    "google": {
      "generationConfig": { /* ... */ },
      "safetySettings":   [ /* ... */ ],
      "tools":            [ /* ... */ ],
      "systemInstruction": { /* ... */ },
      "thinking_config":  { /* ... */ }
    }
  }
}
```

### 5.1 两条透传路径


| 路径                         | 字段                                                                   | 行为                                                                                                                                                                                                                                                                                          |
| -------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ① **snake_case 白名单（兼容旧版）** | `extra_body.google.thinking_config`                                  | 显式解析后写入对应字段。**只接受 snake_case key**，类型不匹配会被静默跳过（不报错），交由 ② 兜底。                                                                                                                                                                                                                                |
| ② **完全透传（无 schema 校验）**    | `extra_body.google.`* 下**除 ① 外**的任意字段                                | 把整个 `extra_body.google` 子树（剔除 `thinking_config`）**深度合并**到最终发给 Gemini 的请求 JSON。字段名按 **Gemini 官方原生 camelCase** 书写（`generationConfig`、`safetySettings`、`tools`、`systemInstruction`、`toolConfig`、`cachedContent`、`responseModalities`、`responseSchema`、`responseJsonSchema` 等等）。 |


### 5.2 thinking_config（snake_case 白名单）


| 字段                 | 类型     | Gemini 字段         | 说明                                                                |
| ------------------ | ------ | ----------------- | ----------------------------------------------------------------- |
| `thinking_budget`  | int    | `thinkingBudget`  | thinking token 预算。> 0 时自动把 `include_thoughts` 设为 true；= 0 或负数禁用思考 |
| `include_thoughts` | bool   | `includeThoughts` | 是否返回思考过程（`reasoning_content`）                                     |
| `thinking_level`   | string | `thinkingLevel`   | 思考级别（如 `"HIGH"`）                                                  |


> 只要传了 `extra_body.google`，系统自动的思维链适配会关闭，全部 thinking 行为由调用方掌控。

### 5.3 完全透传合并规则

- 把 `extra_body.google`（剔除上面 1 个 snake_case key）作为 patch。
- 把已经根据 OpenAI 字段构造好的 Gemini 请求作为 base。
- **deep merge**：
  - 相同 key 两边都是 map → 递归合并；
  - 其它类型（标量、数组、null）→ patch 直接覆盖 base；
  - patch 中不存在的 key 保留 base 原值。
- 合并后直接作为上游请求体发送，效果等同于"原生 Gemini 调用"。

> 含义：你可以用 `extra_body.google.generationConfig.maxOutputTokens` 覆盖通过 OpenAI 字段 `max_tokens` 设置的值，也可以用 `extra_body.google.safetySettings` 完全替换平台默认安全设置，新增 Gemini 字段（如未来上线的字段）无需改代码即可直接使用。

### 5.4 透传示例

```json
{
  "model": "gemini-3.5-flash",
  "messages": [{ "role": "user", "content": "帮我写一篇关于 AI 的文章" }],
  "extra_body": {
    "google": {
      "generationConfig": {
        "temperature": 1,
        "topP": 0.95,
        "maxOutputTokens": 32768
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

## 6. 响应格式

### 6.1 非流式 `chat.completion`

```json
{
  "id": "sXIFar39H4K0694P2MWmWQ",
  "object": "chat.completion",
  "created": 1747299537,
  "model": "gemini-3.5-flash",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "你好，有什么可以帮你？",
        "reasoning_content": "用户在打招呼..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": { /* 见 §7 */ }
}
```

- `id` = 上游 `responseId`（与日志 `request_id` 一致，便于排查问题），上游缺 `responseId` 时回退到 `chatcmpl-*` 格式。
- `reasoning_content`：思考过程的文本内容（仅 `include_thoughts:true` 时才会返回）。
- `executable_code` / `code_execution_result`：自动转成 markdown 代码块嵌入在 text 里。
- 非图片媒体（音频等）：以 markdown `[media](data:...)` 形式嵌入在 text 里。
- `finish_reason` 映射：`STOP→stop`、`MAX_TOKENS→length`、`SAFETY/RECITATION/PROHIBITED_CONTENT/...→content_filter`、有 `functionCall` → `tool_calls`。

### 6.2 流式 `chat.completion.chunk`

- 流式响应中 `delta.content` 是字符串（不是数组）。
- 图片以 `![image](data:image/png;base64,...)` markdown 形式嵌入 `delta.content`。
- `id` 在流式响应中保持一致。

---

## 7. usage 用量

`response.usage` 完整字段：

```jsonc
{
  "prompt_tokens": 1127,
  "completion_tokens": 2050,     // 包含 reasoning tokens
  "total_tokens": 2273,

  "prompt_tokens_details": {
    "cached_tokens": 0,
    "text_tokens":   7,
    "audio_tokens":  0,
    "image_tokens":  1120
  },

  "completion_tokens_details": {
    "text_tokens":      26,
    "audio_tokens":     0,
    "image_tokens":     1120,
    "reasoning_tokens": 904      // 思考 token，单独展示
  }
}
```

### 7.1 思考 token 怎么算

- `reasoning_tokens` 单独显示，方便查看思考消耗。
- `completion_tokens` **包含** `reasoning_tokens`（OpenAI 标准语义，扣费按 `completion_tokens`）。

### 7.2 输出 token 归类

系统会根据实际输出内容自动归类 token 类型：


| 实际输出内容 | 归类                |
| ------ | ----------------- |
| 输出了图片  | 归入 `image_tokens` |
| 只输出了文本 | 归入 `text_tokens`  |


> 归类基于实际输出内容，而非模型名——即使模型名含 "image"，纯文本回答仍按文本计费。

### 7.3 modality 大小写处理

系统自动兼容 `"image"` / `"IMAGE"` 等不同大小写写法。

---

## 8. 日志与对账

平台会为每个请求记录上游的 token 用量信息，可在日志中查看：

- `responseId` 与 `response.id` 完全一致，也与日志的 `request_id` 一致，方便排查问题。

---

## 9. 完整调用示例

### 9.1 纯文本对话（开 thinking）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [{"role": "user", "content": "证明费马大定理"}],
    "extra_body": {
      "google": {
        "thinking_config": { "thinking_level": "HIGH", "include_thoughts": true }
      }
    }
  }'
```

### 9.2 多模态输入（文本 + 图片 URL）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "这张图里有什么？" },
          { "type": "image_url", "image_url": { "url": "https://example.com/cat.jpg" } }
        ]
      }
    ]
  }'
```

### 9.3 启用 Google 搜索 + URL 上下文

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [{"role": "user", "content": "今天科技领域有什么大新闻？"}],
    "tools": [
      { "type": "function", "function": { "name": "googleSearch" } },
      { "type": "function", "function": { "name": "urlContext" } }
    ]
  }'
```

### 9.4 流式对话

```bash
curl -N -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [{"role": "user", "content": "写一首五言绝句"}],
    "stream": true,
    "stream_options": { "include_usage": true }
  }'
```

---

## 10. OpenAI 独有参数（Gemini 无对应，传了会被忽略）

以下 OpenAI 标准参数在 Gemini API 中没有对应字段，传入后会被静默丢弃，不会报错：


| OpenAI 字段                                     | 说明                                                                |
| --------------------------------------------- | ----------------------------------------------------------------- |
| `logit_bias`                                  | Gemini 不支持 token 级偏置                                              |
| `prediction`                                  | Predicted output（Gemini 无对应）                                      |
| `user`                                        | OpenAI 用户标识（Gemini 无对应）                                           |
| `parallel_tool_calls`                         | Gemini 不支持控制并行工具调用                                                |
| `verbosity`                                   | GPT-5 特有参数                                                        |
| `service_tier`                                | OpenAI 服务等级                                                       |
| `safety_identifier`                           | OpenAI 安全标识                                                       |
| `store`                                       | OpenAI 数据存储开关                                                     |
| `prompt_cache_key` / `prompt_cache_retention` | OpenAI 缓存控制                                                       |
| `web_search_options`                          | OpenAI 网络搜索（Gemini 用 `tools` 中的 `googleSearch` / `urlContext` 代替） |
| `functions` / `function_call`                 | OpenAI 旧版 function calling（已废弃，请用 `tools` + `tool_choice`）        |


---

## 11. Q&A — 思维链（Thinking / Reasoning）控制

### Q1：用 OpenAI 格式调用时，能控制 Gemini 的思维链长度吗？

**可以。** 有三种方式：

#### 方式一：`reasoning_effort`（OpenAI 标准字段，最简单）

直接传 OpenAI 的 `reasoning_effort` 参数，系统自动转成 Gemini 的 thinking 配置：

```json
{
  "model": "gemini-3.5-flash",
  "messages": [{"role": "user", "content": "证明费马大定理"}],
  "reasoning_effort": "high"
}
```

映射规则（自动处理）：


| `reasoning_effort` | Gemini 3 系列                |
| ------------------ | -------------------------- |
| `"low"`            | `thinkingLevel = "LOW"`    |
| `"medium"`         | `thinkingLevel = "MEDIUM"` |
| `"high"`           | `thinkingLevel = "HIGH"`   |


> 注意：`reasoning_effort` 仅在模型名含 `-thinking` 后缀时生效（如 `gemini-3.5-flash-thinking`）。普通模型名（如 `gemini-3.5-flash`）不会触发自动思维链适配。

#### 方式二：模型名后缀（自动注入 thinking 配置）


| 模型名后缀            | 行为                                                                  |
| ---------------- | ------------------------------------------------------------------- |
| `-thinking`      | 开启 thinking + `includeThoughts: true`，budget 按 `max_tokens` 百分比自动计算 |
| `-thinking-<数字>` | 开启 thinking，budget = 指定数字（会被 clamp 到合法范围）                           |
| `-nothinking`    | 关闭 thinking（`thinkingBudget = 0`），仅 Gemini 2.5 系列生效                 |


示例：`gemini-3.5-flash-thinking-16384` → 开启 thinking，budget = 16384。

#### 方式三：`extra_body.google.thinking_config`（最灵活，完全控制）

```json
{
  "model": "gemini-3.5-flash",
  "messages": [{"role": "user", "content": "..."}],
  "extra_body": {
    "google": {
      "thinking_config": {
        "thinking_level": "HIGH",
        "include_thoughts": true
      }
    }
  }
}
```

Gemini 3 系列用 `thinking_level` 代替 `thinking_budget`：

```json
{
  "extra_body": {
    "google": {
      "thinking_config": {
        "thinking_level": "HIGH",
        "include_thoughts": true
      }
    }
  }
}
```

> **优先级**：`extra_body.google.thinking_config` > `reasoning_effort` > 模型名后缀。传了 `extra_body.google` 后系统自动的思维链适配会关闭，所有 thinking 行为完全由调用方控制。

### Q2：思维链的输出怎么拿？

设了 `include_thoughts: true` 后，思考过程会放在响应的 `reasoning_content` 字段：

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "最终回答...",
      "reasoning_content": "思考过程..."
    }
  }]
}
```

流式响应中，思考内容通过 `delta.reasoning_content` 增量返回。

### Q3：Gemini 2.5 和 Gemini 3 的 thinking 有什么区别？


| 特性                         | Gemini 2.5                   | Gemini 3                                    |
| -------------------------- | ---------------------------- | ------------------------------------------- |
| 控制方式                       | `thinkingBudget`（整数，token 数） | `thinkingLevel`（枚举：MINIMAL/LOW/MEDIUM/HIGH） |
| 能否关闭 thinking              | 可以（`thinkingBudget = 0`）     | **不能关闭**（官方限制）                              |
| `reasoning_effort: "none"` | 支持（关闭 thinking）              | **不支持**                                     |


---

## 12. 已知限制


| 限制                                           | 说明                                                                                                       |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| 流式响应中的图片用 markdown 嵌图                        | 流式时图片以 `![image](data:...)` 形式嵌入文本；非流式时返回 v2 多模态数组                                                       |
| 非图片媒体输出仍是 markdown 文本                        | 音频等非图片媒体以 markdown 形式嵌入                                                                                  |
| `extra_body.google.`* 完全透传，**不做字段校验**        | 字段写错（如 typo、值类型错误）会原样发给上游，由 Gemini 返回错误，调用方负责字段正确性                                                       |
| 默认安全设置由平台统一配置                                | 想关掉/调整请通过 `extra_body.google.safetySettings` 覆盖                                                          |


---

## 13. 调试 / 排错快速索引


| 问题                                                                    | 怎么查                                                                                                                                         |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| usage 看起来不对                                                           | 查看日志中的 token 用量记录，与上游数据核对                                                                                                                   |
| `response.id` 和日志对不上                                                  | 正常不应出现，若出现请联系技术支持                                                                                                                           |
| 图片为啥是 markdown 不是数组                                                   | 流式响应中图片以 markdown 形式返回；非流式才返回多模态数组                                                                                             |
| `extra_body.google.xxx` 没生效                                           | 1) `extra_body` 是 JSON 对象，不能是字符串；2) `xxx` 是否在 `google` 命名空间下；3) `thinking_config` 需要用 snake_case，其他字段用 Gemini 原生 camelCase |
| 不想用平台默认 safety                                                        | 在 `extra_body.google.safetySettings` 里完整覆盖                                                                                                  |


