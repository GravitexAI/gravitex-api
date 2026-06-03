# 用 OpenAI 兼容接口调用 Gemini imagine 模型生图（**内部开发文档**）

> 本文档面向 Gravitex AI 网关后端开发者，包含源码路径、内部函数名、错误码语义等实现细节。
>
> **想给客户/外部开发者看的版本**：见 [`Gemini imagine 生图 API.md`](./Gemini%20imagine%20生图%20API.md)。

> 路径：
> - `POST /v1/images/generations`
> - `POST /v1/images/edits`
>
> 适用模型：Gemini imagine 系列（nano banana 等可同时返回文本+图片的多模态生图模型），具体清单见 §1。
>
> 关键文件：
> - 入口/计费：`relay/image_handler.go` → `ImageHelper`
> - 请求转换：`relay/channel/gemini/adaptor.go` → `ConvertImageRequest` → `convertImagineImageRequest`
> - 响应转换：`relay/channel/gemini/relay-gemini.go` → `GeminiImagineImageHandler`
> - imagine 模型清单：`setting/model_setting/gemini.go` → `SupportedImagineModels`（管理后台 `Gemini 设置 → 支持图片生成的模型列表` 可改）
> - 同模型走 `/v1/chat/completions` 入口的规则：见 [`openai-compat.md`](Gemini Openai格式对话 API.md)

---

## 1. 模型/路径分流

`/v1/images/generations` 入口下，按模型自动选不同上游协议：

| 模型 | URL 路径 | 上游 action | 协议 | 说明 |
| --- | --- | --- | --- | --- |
| `imagen-*`（如 `imagen-3.0-generate-002`） | `/v1/images/generations` | `:predict` | `GeminiImageRequest` | 走原 Imagen 协议，返回 `predictions[]` |
| imagine 系列（`gemini-3.1-flash-image-preview`、`gemini-2.5-flash-image`、`gemini-3-pro-image-preview`、`gemini-2.0-flash-exp-image-generation`、`gemini-2.0-flash-exp` 等） | `/v1/images/generations` 或 `/v1/images/edits` | `:generateContent` | `GeminiChatRequest` | 走 chat 协议，自动注入 `responseModalities=["TEXT","IMAGE"]` 与平台默认 `safetySettings`；两个入口语义等价 |
| 其它（`gemini-2.5-pro` 等纯对话模型） | `/v1/images/generations` | — | — | **不支持**，会在 `ConvertImageRequest` 里直接报错 |

> imagine 模型清单由 `IsGeminiModelSupportImagine(model)` 命中决定，可在管理后台 `Gemini 设置 → 支持图片生成的模型列表` 增删，无需改代码。

> 本文只讲 imagine 系列。`imagen-*` 走传统 Imagen 协议（与本文档无关）。

---

## 2. 请求路径与认证

```
POST {base}/v1/images/generations
POST {base}/v1/images/edits
Authorization: Bearer sk-<your-token>
Content-Type: application/json
```

> 不支持 `multipart/form-data`（OpenAI 官方 `images/edits` 用的是 multipart 上传文件），请用 JSON + base64 / URL 形式传图。

---

## 3. OpenAI 标准字段 → Gemini 映射

| OpenAI 字段 | Gemini 字段 | 说明 |
| --- | --- | --- |
| `model` | URL path 中的 `models/<model>` | 必须在 `SupportedImagineModels` 里 |
| `prompt` | `contents[0].parts[0].text` | 必填，空字符串会直接报错 |
| `image` | `contents[0].parts[1..]` 的 `inlineData` | 可选，详见 §4 |
| `size` | `generationConfig.imageConfig.aspectRatio` | 见 §5.1 映射 |
| `quality` | `generationConfig.imageConfig.imageSize` | 见 §5.2 映射 |
| `n` | — | imagine 单次调用上游只产出 1 张图，`n` 不会被放大；按上游实际产出张数计费（见 §7） |
| `response_format` / `style` / `background` / `output_format` / `output_compression` / `partial_images` | — | 当前**不透传**给 Gemini；如需更细粒度的 `imageConfig`，请改走 `/v1/chat/completions` + `extra_body.google.generationConfig.imageConfig`（见 [`openai-compat.md`](Gemini Openai格式对话 API.md) §5） |

> 平台默认 `safetySettings` 来自 `setting/model_setting/gemini.go` → `SafetySettings`，会自动写入。`/v1/images/generations` 入口下**不接受** `extra_body`，要覆盖 safety 请走 chat/completions。

---

## 4. `image` 字段（图生图 / 图编辑）

OpenAI 的 `image` 字段用于传待编辑/参考图。本入口接受以下两种 JSON 形态：

```jsonc
// (1) 单张：字符串
{ "image": "https://example.com/cat.jpg" }
{ "image": "data:image/png;base64,iVBORw0KGgo..." }
{ "image": "iVBORw0KGgo..." }   // 纯 base64（无 data URI 前缀），需要保证 mime 是图片

// (2) 多张：字符串数组
{ "image": [
    "https://example.com/a.jpg",
    "data:image/png;base64,iVBORw0KGgo..."
] }
```

* URL 形态会由平台 fetch + 缓存，并校验 MIME 是否在 Gemini 支持的图片白名单里。
* base64 / data URI 形态由平台 decode + 校验 MIME。
* 非以上两种形态（数字、bool、对象、数组里混了非字符串等）**会直接报错**，避免静默吞错。
* MIME 白名单：`image/png`、`image/jpeg`、`image/jpg`、`image/webp`、`image/heic`、`image/heif`（与 chat/completions 入口一致）。

> `/v1/images/edits` 入口下的 `image` 与 `/v1/images/generations` 行为完全一致——对 imagine 模型而言，generations 与 edits 协议层没有区别。

---

## 5. size / quality 映射

### 5.1 size → aspectRatio

| OpenAI `size` | Gemini `aspectRatio` |
| --- | --- |
| `256x256`、`512x512`、`1024x1024` | `1:1` |
| `1536x1024` | `3:2` |
| `1024x1536` | `2:3` |
| `1024x1792` | `9:16` |
| `1792x1024` | `16:9` |
| 其它含 `:` 的写法（如 `9:16`、`4:3`） | 原样透传 |
| 未传或无法识别的尺寸 | 不传 `aspectRatio`，由 Gemini 默认（通常 `1:1`） |

### 5.2 quality → imageSize

imagine 模型的输出分辨率离散，只有 `1K` / `2K` 两档：

| OpenAI `quality` | Gemini `imageSize` |
| --- | --- |
| `hd`、`high`、`2K` | `2K` |
| `standard`、`medium`、`low`、`auto`、`1K`、未传 | `1K` |

---

## 6. 响应格式

成功响应：

```json
{
  "created": 1747299537,
  "data": [
    {
      "url": "",
      "b64_json": "iVBORw0KGgo...",
      "revised_prompt": "好的，我已为您生成了一只穿唐装写春联的柴犬..."
    }
  ],
  "metadata": { "text": "好的，我已为您生成了一只穿唐装写春联的柴犬..." }
}
```

* `data[].b64_json`：上游 `candidates[].content.parts` 中所有 `inlineData.mimeType` 以 `image/` 开头的 part。
* `data[0].revised_prompt`：上游同时返回的所有非思考文本 part（描述、修订后的 prompt 等）合并的字符串，**仅注入到第一张图片**。
* `metadata.text`：与 `revised_prompt` 同内容，方便不识别 `revised_prompt` 的客户端拿到模型描述。
* 不返回 `usage` 字段（OpenAI Image API 协议本身没有），但服务端日志和计费仍按上游真实 token 进行（见 §7）。

错误响应：

| 上游情况 | HTTP 状态 | 错误码 |
| --- | --- | --- |
| `promptFeedback.blockReason` 命中（被 safety 拦截） | `400` | `prompt_blocked` |
| 上游 200 但 `candidates[].content.parts` 没有任何 `image/*` inlineData | `502` | `empty_response` |
| 上游 5xx / 4xx | 透传上游状态码 | 由 `relay/service.RelayErrorHandler` 统一处理 |

---

## 7. 用量与计费

* 复用 `buildUsageFromGeminiMetadata` 解析上游 `usageMetadata`，与 `/v1/chat/completions` 入口下 `GeminiChatHandler` 完全同口径（包括 `prompt_tokens` / `completion_tokens` / `prompt_tokens_details` / `completion_tokens_details` 的 modality 拆分）。

* 服务端会打印一条 INFO 日志便于对账：

  ```
  [INFO] gemini imagine images generations upstream usageMetadata (responseId=..., model=...): {"promptTokenCount":..., "candidatesTokenCount":..., "candidatesTokensDetails":[...]}
  ```

* `usage.GeneratedImages` 设置为本次响应**实际产出**的图片张数，由 `relay/image_handler.go` 在 `OtherRatios["n"]` 中按真实张数计费——避免请求 `n=4` 但上游只出 1 张时被多扣 4 张图片费用。

* 上游被 safety 拦截或返回零图时 → 不产生计费（直接返回错误，不写 usage）。

---

## 8. 限制

| 限制 | 说明 |
| --- | --- |
| `n` 被忽略 | imagine 上游单次只出 1 张图；如需多张请客户端多次调用 |
| 不支持流式 | OpenAI `/v1/images/generations` 本身就是非流式入口；imagine 模型在 chat/completions 入口的流式也会被强制降级 |
| `response_format` / `background` / `output_format` 等不透传 | 当前不映射给 Gemini；要使用 Gemini 原生 imageConfig 全部能力请走 `/v1/chat/completions` + `extra_body.google.generationConfig.imageConfig` |
| 不支持 `multipart/form-data` | `/v1/images/edits` 仅支持 JSON + `image` 字段 |
| 默认 safety 由平台决定 | 想关掉/调整请走 `/v1/chat/completions` + `extra_body.google.safetySettings` 覆盖 |
| MIME 白名单严格 | `image` 字段必须是 png / jpeg / jpg / webp / heic / heif 之一 |

---

## 9. 完整调用示例

### 9.1 纯文生图

```bash
curl -X POST https://api.example.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "一只穿唐装在写春联的柴犬，水墨风格",
    "size": "1024x1024",
    "quality": "hd"
  }'
```

### 9.2 图生图（参考图 + 改写）

```bash
curl -X POST https://api.example.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "把这张照片改成赛博朋克风格，霓虹色调",
    "size": "16:9",
    "image": "data:image/jpeg;base64,/9j/4AAQ..."
  }'
```

### 9.3 多图融合（多张参考图）

```bash
curl -X POST https://api.example.com/v1/images/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash-image",
    "prompt": "把这两个角色画到同一张图里",
    "image": [
      "https://example.com/char-a.png",
      "https://example.com/char-b.png"
    ]
  }'
```

### 9.4 同样的请求走 `/v1/images/edits`（与 9.2 等价）

```bash
curl -X POST https://api.example.com/v1/images/edits \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "把这张照片改成赛博朋克风格",
    "image": "data:image/jpeg;base64,/9j/4AAQ..."
  }'
```

> 对 imagine 模型来说，`/v1/images/edits` 与 `/v1/images/generations` 协议层完全等价，平台只是按 OpenAI 入口的语义区分；选哪个看客户端 SDK 习惯即可。

---

## 10. 调试 / 排错

| 问题 | 怎么查 |
| --- | --- |
| 报 `not supported model for image generation, only imagen / imagine (nano banana) models are supported` | 模型名既不以 `imagen` 开头，也不在 `SupportedImagineModels` 里——把它加进管理后台 imagine 模型清单，或换 `/v1/chat/completions` 入口 |
| 报 `prompt is required for image generation` | `prompt` 字段为空或仅有空白；ImageRequest 早在 binding 层就要求必填，正常不会到这里 |
| 报 `mime type is not supported by Gemini: ...` | `image` 字段拿到的图 MIME 不在白名单里；常见原因是 base64 没带 data URI 前缀且服务端无法从内容嗅探出 MIME |
| 报 `invalid image field: unsupported shape, expected string or []string` | `image` 传成了对象 / 数字 / bool 等，请改成单 string 或 []string |
| usage 看起来不对 | 翻日志找 `gemini imagine images generations upstream usageMetadata`，原文核对；`GeneratedImages` 看响应里 `data` 长度 |
| 上游返回 200 但客户端拿到 502 `empty_response` | 说明上游 `candidates[].content.parts` 没有任何 `image/*` inlineData——通常是模型没真生图，只回了文本；翻日志看 usageMetadata 与 `responseId`，必要时切到 `/v1/chat/completions` 入口看上游完整 candidate |
| 计费按 `n` 扣了多次 | 不应再出现：`GeneratedImages` 来自上游真实产出；如怀疑请到日志比对 `image_handler.go` 的 `生成数量 N` 字段 |

---

## 11. 代码锚点

修改点都打了 `CHZ-PATCH(<feature>)` 注释，搜索这些字符串即可定位：

| 锚点 | 功能 |
| --- | --- |
| `CHZ-PATCH(gemini-imagine-images-generations)` | imagine 模型支持 `/v1/images/generations` 与 `/v1/images/edits` 入口（gemini + vertex 通道） |
| `CHZ-PATCH(gemini-imagine-no-stream)` | imagine 模型在 chat/completions 入口下的强制非流式（与本入口无关，但同模型族） |
| `CHZ-PATCH(gemini-usage-fix)` | usage 模态归类 + 对账日志（chat 入口；imagine images 入口复用同一 `buildUsageFromGeminiMetadata`） |
