# Gemini imagine 生图 API 用户文档

Gemini imagine 系列（包含 Google 官方"nano banana"等可同时返回**文本 + 图片**的多模态生图模型）通过 Gravitex AI 网关，可用 OpenAI 兼容的 `/v1/images/generations` 与 `/v1/images/edits` 接口直接调用——无需 Google 账号、无需切换 SDK，只要把 `base_url` 指到 Gravitex AI 即可立即使用。

**Base URL**：`https://api.gravitex.ai`

---

## 目录

- [认证](#认证)
- [模型列表](#模型列表)
- [接口总览](#接口总览)
- [`/v1/images/generations` — 文生图 / 图生图](#v1imagesgenerations--文生图--图生图)
  - [请求参数](#请求参数)
  - [`image` 字段格式](#image-字段格式)
  - [`size` / `quality` 取值映射](#size--quality-取值映射)
  - [响应格式](#响应格式)
- [`/v1/images/edits` — 图片编辑](#v1imagesedits--图片编辑)
- [使用场景与示例](#使用场景与示例)
  - [1. 纯文生图](#1-纯文生图)
  - [2. 图生图 / 风格迁移](#2-图生图--风格迁移)
  - [3. 多图融合](#3-多图融合)
  - [4. `/v1/images/edits` 等价调用](#4-v1imagesedits-等价调用)
  - [5. 用 Python (openai SDK) 调用](#5-用-python-openai-sdk-调用)
  - [6. 用 Node.js (openai SDK) 调用](#6-用-nodejs-openai-sdk-调用)
- [错误处理](#错误处理)
- [计费规则](#计费规则)
- [限制说明](#限制说明)
- [最佳实践](#最佳实践)
- [常见问题](#常见问题)
- [附录：与 `/v1/chat/completions` 入口的对比](#附录与-v1chatcompletions-入口的对比)

---

## 认证

所有接口使用 **Bearer Token** 认证。在 [Gravitex AI 平台](https://api.gravitex.ai) 创建令牌后，在请求头中添加：

```
Authorization: Bearer sk-{your_token_key}
```

所有请求均使用 JSON 格式：

```
Content-Type: application/json
```

---

## 模型列表

| 模型 ID | 说明 | 是否支持图生图 | 默认输出尺寸 |
|---------|------|---------------|------------|
| `gemini-3-pro-image-preview` | Gemini 3 Pro 图像生成预览版，质量最高，适合正式产物 | ✅ | 1024×1024 / 2K |
| `gemini-3.1-flash-image-preview` | Gemini 3.1 Flash 图像生成预览版，速度快、性价比高（即 "nano banana"） | ✅ | 1024×1024 |
| `gemini-2.5-flash-image` | Gemini 2.5 Flash 图像生成稳定版 | ✅ | 1024×1024 |

> 平台还会持续接入新的 imagine 系列模型，最新支持列表请在 [Gravitex AI 平台](https://api.gravitex.ai) 模型页面查看。

---

## 接口总览

| 接口 | 方法 | 用途 |
|------|------|------|
| `/v1/images/generations` | POST | 文生图、图生图、多图融合 |
| `/v1/images/edits` | POST | 图片编辑（与 generations 协议等价，由 SDK 习惯决定走哪个） |

> 对 imagine 模型来说，`/v1/images/generations` 与 `/v1/images/edits` **完全等价**——传不传 `image` 字段决定是文生图还是图生图，而不是接口路径。选哪个看你用的 SDK 习惯（OpenAI Python SDK 中 `images.generate` vs `images.edit`）。

---

## `/v1/images/generations` — 文生图 / 图生图

**POST** `https://api.gravitex.ai/v1/images/generations`

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model` | string | **是** | — | 模型 ID，见 [模型列表](#模型列表) |
| `prompt` | string | **是** | — | 文本提示词，描述想要生成的图片 |
| `image` | string / string[] | 否 | — | 输入参考图（图生图、风格迁移、多图融合用），格式见 [`image` 字段格式](#image-字段格式) |
| `size` | string | 否 | 由模型决定 | 期望尺寸或宽高比，见 [`size` / `quality` 取值映射](#size--quality-取值映射) |
| `quality` | string | 否 | `auto` | 输出质量档位，见 [`size` / `quality` 取值映射](#size--quality-取值映射) |
| `n` | integer | 否 | `1` | **当前被忽略**：imagine 单次调用上游只产 1 张图；如需多张请客户端多次调用 |
| `response_format` | string | 否 | `b64_json` | **当前不透传**：响应永远以 `b64_json` 字段返回 base64 |

### `image` 字段格式

`image` 字段用于"图生图"——把一张或多张参考图喂给模型作为视觉上下文。支持以下两种 JSON 形态：

```jsonc
// (1) 单张：字符串
{ "image": "https://example.com/cat.jpg" }
{ "image": "data:image/png;base64,iVBORw0KGgo..." }
{ "image": "iVBORw0KGgo..." }   // 纯 base64（无 data URI 前缀，需保证是图片）

// (2) 多张：字符串数组
{ "image": [
    "https://example.com/a.jpg",
    "data:image/png;base64,iVBORw0KGgo..."
] }
```

* **URL 形态**：网关会自动 fetch 并校验图片类型；URL 必须可公网访问，建议用 https。
* **base64 / data URI 形态**：网关自动 decode 并嗅探 MIME。
* **支持的图片格式**：`image/png`、`image/jpeg`、`image/jpg`、`image/webp`、`image/heic`、`image/heif`。
* **不支持** `multipart/form-data` 上传文件——请把图片转成 base64 或先上传到对象存储拿到 URL。

### `size` / `quality` 取值映射

imagine 模型的输出有限定档位（不像传统模型可任意指定像素），网关会自动把 OpenAI 风格的取值映射到模型支持的档位。

#### `size` → 宽高比

| `size` 取值 | 映射结果（aspectRatio） |
|------------|----------------------|
| `256x256`、`512x512`、`1024x1024` | `1:1`（正方形） |
| `1536x1024` | `3:2`（横版） |
| `1024x1536` | `2:3`（竖版） |
| `1024x1792` | `9:16`（手机竖屏） |
| `1792x1024` | `16:9`（宽屏） |
| 直接传宽高比（如 `9:16`、`16:9`、`4:3`、`3:4`） | 原样使用 |
| 未传或不在上表 | 由模型自选默认（通常 `1:1`） |

#### `quality` → 输出分辨率档位

| `quality` 取值 | 映射结果（imageSize） | 适用 |
|---------------|---------------------|------|
| `hd`、`high`、`2K` | `2K` | 高清，文件更大、耗时略长 |
| `standard`、`medium`、`low`、`auto`、`1K`、未传 | `1K` | 标准清晰度，速度更快 |

### 响应格式

#### 成功响应

```json
{
  "created": 1747299537,
  "data": [
    {
      "url": "",
      "b64_json": "iVBORw0KGgoAAAANSUhEUgAA...",
      "revised_prompt": "好的，我已为您生成了一只穿唐装写春联的柴犬..."
    }
  ],
  "metadata": {
    "text": "好的，我已为您生成了一只穿唐装写春联的柴犬..."
  },
  "usage": {
    "total_tokens": 1421,
    "input_tokens": 27,
    "output_tokens": 1394,
    "input_tokens_details": {
      "text_tokens": 27,
      "image_tokens": 0
    },
    "output_tokens_details": {
      "image_tokens": 1290,
      "text_tokens": 104
    }
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `created` | integer | 时间戳（秒） |
| `data[].b64_json` | string | 图片 base64 字符串（不含 `data:` 前缀），客户端拿到后直接 decode 即可保存为 png |
| `data[].url` | string | 当前为空字符串（保留兼容字段，平台不上传图片到外部 URL） |
| `data[0].revised_prompt` | string | 模型返回的描述文本/修订后的 prompt（imagine 模型的特色：图文一起返回） |
| `metadata.text` | string | 与 `data[0].revised_prompt` 相同内容的副本，方便不识别 `revised_prompt` 的客户端拿到模型描述 |
| `usage.total_tokens` | integer | 本次请求总 token 数 |
| `usage.input_tokens` | integer | 输入 token 数（含 prompt 文本 + 输入参考图） |
| `usage.output_tokens` | integer | 输出 token 数（含图片 token + 模型同时返回的文字描述 token） |
| `usage.input_tokens_details.text_tokens` | integer | 输入 prompt 的文本 token 数 |
| `usage.input_tokens_details.image_tokens` | integer | 输入参考图的 token 数（无图生图时为 0） |
| `usage.output_tokens_details.image_tokens` | integer | 生成图片消耗的 token 数（imagine 模型每张图固定开销，不同模型不同） |
| `usage.output_tokens_details.text_tokens` | integer | 模型返回的描述文字消耗的 token 数 |

> Gemini imagine 模型在生成图片的同时常常会返回一段文字描述（"我帮您画了 X"），这段文字以 `revised_prompt` + `metadata.text` 形式返回，客户端可选择展示。
>
> `usage` 字段格式与 OpenAI gpt-image-1 完全对齐，方便用 OpenAI SDK 的项目无缝接入。

#### 错误响应

```json
{
  "error": {
    "message": "request blocked by Gemini API: SAFETY",
    "type": "invalid_request_error",
    "code": "prompt_blocked"
  }
}
```

详见 [错误处理](#错误处理) 章节。

---

## `/v1/images/edits` — 图片编辑

**POST** `https://api.gravitex.ai/v1/images/edits`

请求/响应格式与 `/v1/images/generations` **完全一致**（同样接受 JSON + `image` 字段，返回相同结构）。区别仅在于 OpenAI Python SDK 中 `images.edit` 与 `images.generate` 是两个不同的方法，部分用户习惯按"编辑"语义走 edits 入口。

> ⚠️ 与 OpenAI 官方 dall-e-2 的 `/images/edits` 不同：本接口**不支持** `multipart/form-data` 上传 + `mask` 字段（imagine 模型靠 prompt 指挥编辑，不需要 mask）。`image` 字段同 generations。

---

## 使用场景与示例

### 1. 纯文生图

最基础的使用方式：只传 `prompt`。

```bash
curl -X POST https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "一只穿唐装在写春联的柴犬，水墨风格，留白构图",
    "size": "1024x1024",
    "quality": "hd"
  }'
```

---

### 2. 图生图 / 风格迁移

把一张照片改成赛博朋克风格——`image` 字段传单张参考图。

```bash
curl -X POST https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "把这张照片改成赛博朋克风格，霓虹色调，雨夜街道",
    "size": "16:9",
    "quality": "hd",
    "image": "https://example.com/portrait.jpg"
  }'
```

也可以用 base64 / data URI 形式（适合本地图片）：

```bash
curl -X POST https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "把这张照片改成动漫人物风格",
    "image": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD/..."
  }'
```

---

### 3. 多图融合

把多张参考图合成到一张图里——`image` 字段传字符串数组。

```bash
curl -X POST https://api.gravitex.ai/v1/images/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash-image",
    "prompt": "把这两个角色画到同一个场景里，背景是日落沙滩",
    "size": "16:9",
    "image": [
      "https://example.com/character-a.png",
      "https://example.com/character-b.png"
    ]
  }'
```

---

### 4. `/v1/images/edits` 等价调用

下面这个请求和 [场景 2](#2-图生图--风格迁移) 完全等价，只是入口换成 edits：

```bash
curl -X POST https://api.gravitex.ai/v1/images/edits \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "prompt": "把这张照片改成赛博朋克风格",
    "image": "https://example.com/portrait.jpg"
  }'
```

---

### 5. 用 Python (openai SDK) 调用

```python
import base64
from openai import OpenAI

client = OpenAI(
    api_key="sk-your_token_key",
    base_url="https://api.gravitex.ai/v1",
)

# 文生图
resp = client.images.generate(
    model="gemini-3.1-flash-image-preview",
    prompt="一只穿唐装在写春联的柴犬，水墨风格",
    size="1024x1024",
    quality="hd",
)

img_b64 = resp.data[0].b64_json
print("revised_prompt:", resp.data[0].revised_prompt)

# 保存为本地 PNG
with open("output.png", "wb") as f:
    f.write(base64.b64decode(img_b64))
```

图生图（OpenAI SDK 的 `images.edit` 在我们这里也走 generations 协议；如要走 `/v1/images/edits` 入口，请用 `client.images.edit(...)`）：

```python
# 用 generations 入口做图生图（推荐）：
# OpenAI SDK 的 images.generate 不直接支持 image 字段，
# 这种情况下用底层 client.post 透传：
import httpx
resp = httpx.post(
    "https://api.gravitex.ai/v1/images/generations",
    headers={"Authorization": "Bearer sk-your_token_key"},
    json={
        "model": "gemini-3.1-flash-image-preview",
        "prompt": "改成赛博朋克风格，霓虹色调",
        "image": "https://example.com/portrait.jpg",
    },
    timeout=60,
).json()
print(resp["data"][0]["b64_json"][:80] + "...")
```

---

### 6. 用 Node.js (openai SDK) 调用

```ts
import OpenAI from "openai";
import { writeFile } from "fs/promises";

const client = new OpenAI({
  apiKey: "sk-your_token_key",
  baseURL: "https://api.gravitex.ai/v1",
});

// 文生图
const resp = await client.images.generate({
  model: "gemini-3.1-flash-image-preview",
  prompt: "一只穿唐装在写春联的柴犬，水墨风格",
  size: "1024x1024",
  quality: "hd",
});

const b64 = resp.data[0].b64_json!;
await writeFile("output.png", Buffer.from(b64, "base64"));
console.log("revised_prompt:", resp.data[0].revised_prompt);
```

带参考图（图生图）——直接用 fetch 透传：

```ts
const r = await fetch("https://api.gravitex.ai/v1/images/generations", {
  method: "POST",
  headers: {
    "Authorization": "Bearer sk-your_token_key",
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    model: "gemini-3.1-flash-image-preview",
    prompt: "把这张照片改成赛博朋克风格",
    image: "https://example.com/portrait.jpg",
  }),
});
const data = await r.json();
```

---

## 错误处理

请求失败时，HTTP 状态码 ≠ 200，响应体格式：

```json
{
  "error": {
    "message": "...",
    "type": "...",
    "code": "..."
  }
}
```

### 常见错误码

| HTTP 状态 | `code` | 说明 | 处理建议 |
|-----------|--------|------|---------|
| `400` | `prompt_blocked` | 提示词或参考图被 Gemini 安全策略拦截（如涉及暴力、敏感内容） | 修改 prompt / 替换参考图后重试 |
| `400` | `invalid_request` | 模型不在 imagine 列表里、`prompt` 为空、`image` 字段格式不对、参考图 MIME 不在白名单等 | 按错误 message 调整请求 |
| `401` | `invalid_authentication` | Bearer Token 无效 / 过期 | 在平台重新创建令牌 |
| `402` | `insufficient_quota` | 账户额度不足 | 充值或联系管理员 |
| `429` | `rate_limit_exceeded` | 触发速率限制 | 降低并发，按返回的 `Retry-After`（如有）退避重试 |
| `502` | `empty_response` | 上游 200 但没产出任何图片（极少数情况） | 重试 1–2 次，仍失败请联系客服 |
| `503` | `bad_response` | 上游临时不可用 | 退避重试 |
| `504` | `request_timeout` | 上游响应超时 | 重试 |

> 被安全策略拦截（`prompt_blocked`）通常是 prompt 涉及未成年人不适宜内容、政治敏感、版权人物等。换一种描述方式或避开敏感主题即可。

---

## 计费规则

* **按"实际生成的图片张数"计费**：上游真实返回了几张图就扣几张，请求里的 `n` 参数不影响计费（见 [限制说明](#限制说明)）。
* **被 safety 拦截或返回零图时**：**不计费**。
* 不同模型 / `quality` 档位的单价请在 [Gravitex AI 平台 - 模型价格](https://api.gravitex.ai) 页面查看。
* 计费明细可在 **令牌使用记录** 页查看，每条记录包含 `生成数量 N` 字段。

---

## 限制说明

| 限制项 | 说明 |
|--------|------|
| `n` 被忽略 | imagine 单次调用上游只出 1 张图。如需多张，请客户端循环调用 N 次（每次会按 1 张计费） |
| 不支持流式 | `/v1/images/generations` 与 `/v1/images/edits` 都是非流式接口，没有 SSE chunk |
| 不支持 multipart 上传 | 不接受 `multipart/form-data`；`image` 字段必须是 JSON 字符串（URL / base64 / data URI） |
| `response_format` 当前固定为 `b64_json` | 不支持返回 OSS URL；如需要可自行把 base64 上传到您自己的存储 |
| `mask` 字段不支持 | imagine 模型靠 prompt 指挥编辑，不需要 mask；OpenAI 官方 dall-e-2 的 mask 协议在本接口无效 |
| 单图大小建议 ≤ 8 MB | 过大的输入图会增加处理耗时，并可能触发 HTTP body size 限制 |
| 输入图 MIME 必须在白名单里 | png / jpeg / jpg / webp / heic / heif；其它格式（如 bmp、tiff、gif）会报错 |
| 单次最多输入图建议 ≤ 4 张 | 多图融合时图越多耗时越长，超过 4 张请权衡延迟 |
| 本接口下不接受 `extra_body` | 如需调整 Gemini 原生的 `imageConfig` / `safetySettings` / `thinking_config`，请改走 `/v1/chat/completions` 入口（详见附录） |

---

## 最佳实践

1. **prompt 写法**：imagine 模型对自然语言描述非常敏感，多用具体形容词（风格、色调、构图、镜头视角），避免空泛词汇。
2. **图生图先小图试**：第一次调试时建议把参考图缩到 512×512 以下，调通 prompt 后再上原图。
3. **多次重试**：图像生成有随机性，同一 prompt 可能首次效果不理想，建议提供"重新生成"按钮让用户多试几次。
4. **敏感主题降级**：如果业务可能涉及人物 / 名人 / 品牌 logo，建议在产品上加入"换一张"和"举报"按钮，被 `prompt_blocked` 拦截时友好提示用户。
5. **保存原始 base64**：拿到 `b64_json` 后立即保存到您自己的存储（OSS / S3），不要重复请求 Gravitex AI 拉取——上游不缓存历史结果。
6. **善用 `revised_prompt`**：模型返回的描述文本是免费的"产品文案"，可以直接展示在生成图下方提升用户体验。
7. **`size` 与 `quality` 组合**：先用 `1K` 出草稿，定稿后再用 `2K` 出最终成片，可显著节省成本。

---

## 常见问题

### Q1：为什么我传了 `n=4`，只拿到 1 张图？

A：Gemini imagine 模型上游单次调用只产出 1 张图，平台会按实际产出张数计费。需要多张请客户端多次调用，每次单独计费。

### Q2：图生图能精确控制要改哪些区域吗？

A：imagine 模型不接受 `mask` 字段，所有改动通过 prompt 描述（如"只把头发改成红色，其它保持不变"）。模型会理解 prompt 自动定位区域。

### Q3：返回的 `b64_json` 没有 `data:image/png;base64,` 前缀，怎么用？

A：`b64_json` 是纯 base64 字符串，方便直接 decode 保存为文件。如果你要嵌入 HTML `<img src>`，自己拼接前缀即可：`"data:image/png;base64," + b64_json`。

### Q4：能让模型只返回图片不返回文字吗？

A：当前不能——imagine 模型的特色就是图文一起返回，`revised_prompt`/`metadata.text` 字段可以选择不展示，但请求层面无法关闭。

### Q5：图生图和文生图收费一样吗？

A：是的——按"输出图片张数 + 输出 quality 档位"计费，与是否带 `image` 字段无关。

### Q6：可以同时传文字 + 多张参考图吗？

A：可以。`image` 字段支持 string[]，配合 `prompt` 描述融合方式，参考 [场景 3：多图融合](#3-多图融合)。

### Q7：`/v1/images/generations` 和 `/v1/chat/completions` 调 imagine 模型有什么区别？

A：详见 [附录](#附录与-v1chatcompletions-入口的对比)。简单来说：

* `/v1/images/generations`：协议简单（OpenAI Image API 标准），适合"我只要图片"的场景。
* `/v1/chat/completions`：能控制 Gemini 原生参数（`imageConfig`、`safetySettings`、`thinking_config` 等），适合"需要细粒度控制"的场景。

---

## 附录：与 `/v1/chat/completions` 入口的对比

Gemini imagine 模型同时也可以走 OpenAI 的 `/v1/chat/completions` 接口（把 prompt 写成 message，把图片放进 `content` 数组）。两个入口对比：

| 维度 | `/v1/images/generations` & `/v1/images/edits` | `/v1/chat/completions` |
|------|----------------------------------------------|------------------------|
| 协议 | OpenAI Image API（轻量） | OpenAI Chat Completions API（功能丰富） |
| 输入 | `prompt` + `image` 字段 | `messages[].content`（多模态数组） |
| 输出 | `data[].b64_json` + `revised_prompt` | `choices[].message.content`（数组：text + image_url） |
| 流式 | ❌ 不支持 | ❌ imagine 模型在 chat 入口下也强制非流式 |
| `extra_body` 透传 | ❌ 不接受 | ✅ 通过 `extra_body.google.*` 完全透传 Gemini 原生参数 |
| 多对话上下文 | ❌ 单次请求 | ✅ 支持多轮 messages |
| 适合场景 | 单次出图、批量生图脚本、与 dall-e 兼容的客户端 | 对话式交互（"再改一下"）、需要原生参数（`imageConfig`/`safetySettings`/`thinking_config`） |

切换示例（chat 入口图生图）：

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.1-flash-image-preview",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "把这张照片改成赛博朋克风格" },
          { "type": "image_url", "image_url": { "url": "https://example.com/portrait.jpg" } }
        ]
      }
    ],
    "extra_body": {
      "google": {
        "generationConfig": {
          "imageConfig": { "aspectRatio": "16:9", "imageSize": "2K" }
        }
      }
    }
  }'
```

返回的 `choices[0].message.content` 是数组：

```json
[
  { "type": "text", "text": "好的，已为您改好..." },
  { "type": "image_url", "image_url": {
      "url": "data:image/png;base64,iVBORw0KGgo...",
      "mime_type": "image/png"
  } }
]
```
