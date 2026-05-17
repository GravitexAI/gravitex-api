# Gemini 模型 OpenAI 兼容接口参数透传指南

## 一、`/v1/chat/completions` 透传机制

### 1.1 `googleSearch` / `codeExecution` / `urlContext` — 通过 `tools` 数组

这三个 Gemini 原生工具，通过在 OpenAI 请求的 `tools` 数组中声明特殊的 `function.name` 来透传：

```json
{
  "model": "gemini-2.5-flash",
  "messages": [
    { "role": "user", "content": "今天有什么新闻？" }
  ],
  "tools": [
    { "type": "function", "function": { "name": "googleSearch" } },
    { "type": "function", "function": { "name": "codeExecution" } },
    { "type": "function", "function": { "name": "urlContext" } }
  ]
}
```

这三个特殊名称会被自动转换为 Gemini 原生工具类型：

- `"googleSearch"` → `GeminiChatTool{ GoogleSearch: {} }`
- `"codeExecution"` → `GeminiChatTool{ CodeExecution: {} }`
- `"urlContext"` → `GeminiChatTool{ URLContext: {} }`

对应代码: `relay/channel/gemini/relay-gemini.go:369-432`

Gemini 工具结构体定义: `dto/gemini.go:321-327`

```go
type GeminiChatTool struct {
    GoogleSearch          any `json:"googleSearch,omitempty"`
    GoogleSearchRetrieval any `json:"googleSearchRetrieval,omitempty"`
    CodeExecution         any `json:"codeExecution,omitempty"`
    FunctionDeclarations  any `json:"functionDeclarations,omitempty"`
    URLContext            any `json:"urlContext,omitempty"`
}
```

### 1.2 `thinking_config` / `image_config` — 通过 `extra_body`

`GeneralOpenAIRequest` 有专用的 `extra_body` 字段 (`dto/openai_request.go:82`)：

```go
ExtraBody json.RawMessage `json:"extra_body,omitempty"`
```

JSON 结构为 `extra_body.google.xxx`：

```json
{
  "model": "gemini-2.5-flash",
  "messages": [
    { "role": "user", "content": "Hello" }
  ],
  "extra_body": {
    "google": {
      "thinking_config": {
        "thinking_budget": 1024,
        "include_thoughts": true,
        "thinking_level": "HIGH"
      },
      "image_config": {
        "aspect_ratio": "16:9",
        "image_size": "2K"
      }
    }
  }
}
```

处理代码: `relay/channel/gemini/relay-gemini.go:241-353`

#### thinking_config 子字段说明


| 字段                 | 类型     | 说明                | Gemini API 字段     |
| ------------------ | ------ | ----------------- | ----------------- |
| `thinking_budget`  | int    | thinking token 预算 | `thinkingBudget`  |
| `include_thoughts` | bool   | 是否返回思考过程          | `includeThoughts` |
| `thinking_level`   | string | thinking 级别       | `thinkingLevel`   |


#### image_config 子字段说明


| 字段             | 类型     | 说明             | Gemini API 字段 |
| -------------- | ------ | -------------- | ------------- |
| `aspect_ratio` | string | 宽高比，如 `"16:9"` | `aspectRatio` |
| `image_size`   | string | 图片尺寸，如 `"2K"`  | `imageSize`   |


> **注意**: 字段名必须用 snake_case，使用 camelCase 会返回错误提示。

### 1.3 imagine 模型自动设置

对于 Gemini imagine 模型（如 `gemini-2.5-flash-image-preview`），代码会自动设置 `ResponseModalities: ["TEXT", "IMAGE"]`，使模型能够输出图片。

代码: `relay/channel/gemini/relay-gemini.go:227-232`

### 1.4 参数透传总览


| Gemini 参数        | 透传方式                                | 示例                                        |
| ---------------- | ----------------------------------- | ----------------------------------------- |
| `googleSearch`   | `tools` 数组                          | `{"function": {"name": "googleSearch"}}`  |
| `codeExecution`  | `tools` 数组                          | `{"function": {"name": "codeExecution"}}` |
| `urlContext`     | `tools` 数组                          | `{"function": {"name": "urlContext"}}`    |
| `thinkingConfig` | `extra_body.google.thinking_config` | `{"thinking_budget": 1024}`               |
| `imageConfig`    | `extra_body.google.image_config`    | `{"aspect_ratio": "16:9"}`                |


---

## 二、`/v1/images/generations` 调用 Gemini

### 2.1 支持范围

**仅支持 imagen 系列模型**（model 名以 `"imagen"` 开头），非 imagen 模型直接报错：

```
not supported model for image generation, only imagen models are supported
```

代码: `relay/channel/gemini/adaptor.go:60-61`

### 2.2 参数映射


| OpenAI 参数 | 说明                                 | Imagen API 参数                          |
| --------- | ---------------------------------- | -------------------------------------- |
| `prompt`  | 提示词                                | `instances[0].prompt`                  |
| `n`       | 生成数量（指针类型 `*uint`）                 | `parameters.sampleCount`               |
| `size`    | 尺寸，如 `"1024x1792"`                 | `parameters.aspectRatio`（转换为 `"9:16"`） |
| `quality` | `"hd"`/`"2K"` → `"2K"`；其他 → `"1K"` | `parameters.imageSize`                 |


代码: `relay/channel/gemini/adaptor.go:66-121`

### 2.3 请求结构示例

请求：

```json
{
  "model": "imagen-3.0-generate-001",
  "prompt": "一只猫",
  "n": 2,
  "size": "1024x1792",
  "quality": "hd"
}
```

被转换为：

```json
{
  "instances": [
    { "prompt": "一只猫" }
  ],
  "parameters": {
    "sampleCount": 2,
    "aspectRatio": "9:16",
    "personGeneration": "allow_adult",
    "imageSize": "2K"
  }
}
```

### 2.4 请求 URL

```
{base}/v1beta/models/{model}:predict
```

代码: `relay/channel/gemini/adaptor.go:149-151`

### 2.5 响应转换

Gemini Imagen 返回结构 → OpenAI ImageResponse：

```json
// Gemini 原始响应
{
  "predictions": [
    {
      "mimeType": "image/png",
      "bytesBase64Encoded": "iVBORw0KGgo..."
    }
  ]
}

// 转换为 OpenAI 格式
{
  "data": [
    { "b64_json": "iVBORw0KGgo..." }
  ],
  "usage": {
    "prompt_tokens": 258,
    "total_tokens": 258
  }
}
```

> usage 固定为 258 tokens/张图片。

代码: `relay/channel/gemini/relay-gemini.go:1606-1658`

### 2.6 局限性

`**/v1/images/generations` 不支持 `extra_body` 透传**，`ImageRequest` 结构体没有 `ExtraBody` 字段：

```go
// dto/openai_image.go:14
type ImageRequest struct {
    Model          string `json:"model"`
    Prompt         string `json:"prompt" binding:"required"`
    N              *uint  `json:"n,omitempty"`
    Size           string `json:"size,omitempty"`
    Quality        string `json:"quality,omitempty"`
    // ...
    // 没有 ExtraBody 字段！
}
```

### 2.7 `/v1/chat/completions` vs `/v1/images/generations` 对比


|                            | `/v1/chat/completions`        | `/v1/images/generations` |
| -------------------------- | ----------------------------- | ------------------------ |
| `extra_body` 透传            | 支持                            | **不支持**                  |
| `tools` 透传 (googleSearch等) | 支持                            | 不适用                      |
| 支持模型                       | 所有 Gemini 模型                  | 仅 imagen 系列              |
| Gemini imagine 模型图片输出      | 走这条路                          | 不支持                      |
| 响应中的图片                     | markdown `![image](data:...)` | `b64_json`               |


---

## 三、其他透传机制（补充）

### 3.1 `PassThroughBody` — 全透传模式

配置项: `channel_settings.pass_through_body_enabled` (`dto/channel_settings.go:7`)

**启用后，整个原始请求体直接转发到上游，不做任何格式转换。** 这意味着发送的是原始 JSON body，需要调用方自行构造 Gemini 原生格式。

```go
// relay/gemini_handler.go:141-146
if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
    storage, err := common.GetBodyStorage(c)
    requestBody = common.ReaderOnly(storage)
}
```

### 3.2 `ParamOverride` — 渠道级 JSON 注入

渠道级别的 `param_override` 配置，可对请求 JSON 做任意路径的增删改操作。

代码: `relay/common/override.go`

在 Gemini handler 中的应用: `relay/gemini_handler.go:160-165`

---

## 四、完整请求示例

### 场景一：Gemini 聊天 + Google 搜索 + Thinking

```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-xxx" \
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [
      {"role": "user", "content": "今天有什么新闻？"}
    ],
    "tools": [
      {"type": "function", "function": {"name": "googleSearch"}}
    ],
    "extra_body": {
      "google": {
        "thinking_config": {
          "thinking_level": "HIGH"
        }
      }
    }
  }'
```

### 场景二：Gemini imagine 模型生成图片

```bash
curl -X POST http://localhost:3000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-xxx" \
  -d '{
    "model": "gemini-2.5-flash-image-preview",
    "messages": [
      {"role": "user", "content": "画一只在草地上奔跑的柴犬"}
    ],
    "extra_body": {
      "google": {
        "image_config": {
          "aspect_ratio": "16:9",
          "image_size": "2K"
        }
      }
    }
  }'
```

### 场景三：Imagen 模型生成图片

```bash
curl -X POST http://localhost:3000/v1/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-xxx" \
  -d '{
    "model": "imagen-3.0-generate-001",
    "prompt": "一只在草地上奔跑的柴犬",
    "n": 2,
    "size": "1024x1792",
    "quality": "hd"
  }'
```

