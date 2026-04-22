# GPT-Image-2 图像生成 API 文档

## 概览

支持 OpenAI GPT-Image-2 模型进行文生图和图生图。GPT-Image-2 具备真实世界智能、多语言理解、4K 分辨率支持和智能路由层。

**统一端点**：推荐使用 `/v1/images/generations` 端点同时支持文生图和图生图，无需切换端点。只需在请求中携带 `image` 字段即可自动进入图生图模式。

---

## 通用参数说明

### size 可选值

| 值 | 说明 |
|----|------|
| `1024x1024` | 正方形（默认） |
| `1024x1536` | 竖版 |
| `1536x1024` | 横版 |
| `2880x2880` | 4K 正方形 |
| `2048x3072` | 4K 竖版 |
| `3072x2048` | 4K 横版 |
| 自定义 `WxH` | 每维须为 16 的倍数，总像素 655,360 ~ 8,294,400 |

### quality 可选值

| 值 | 说明 |
|----|------|
| `low` | 低质量，生成速度快 |
| `medium` | 中等质量 |
| `high` | 高质量（默认） |

---

## 文生图

**POST** `https://api.gravitex.ai/v1/images/generations`

### 请求头

| Header | 值 |
|--------|-----|
| `Content-Type` | `application/json` |
| `Authorization` | `Bearer <your_api_key>` |

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model` | string | 是 | - | 固定值 `gpt-image-2` |
| `prompt` | string | 是 | - | 图像描述文本 |
| `n` | integer | 否 | `1` | 生成图片数量，1-10 |
| `size` | string | 否 | `1024x1024` | 图片尺寸 |
| `quality` | string | 否 | `high` | 图片质量 |

### 请求示例

```bash
curl -X POST "https://api.gravitex.ai/v1/images/generations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "一只在星空下奔跑的白色猫咪，赛博朋克风格",
    "size": "1024x1536",
    "quality": "medium",
    "n": 1
  }'
```

### 响应示例

```json
{
  "created": 1776847985,
  "background": "opaque",
  "data": [
    {
      "b64_json": "iVBORw0KGgo..."
    }
  ],
  "output_format": "png",
  "quality": "medium",
  "size": "1024x1536",
  "usage": {
    "input_tokens": 7,
    "input_tokens_details": {
      "image_tokens": 0,
      "text_tokens": 7
    },
    "output_tokens": 1415,
    "total_tokens": 1422
  }
}
```

---

## 图生图（推荐方式：通过 generations 端点）

**POST** `https://api.gravitex.ai/v1/images/generations`

**推荐使用此端点进行图生图**。只需在文生图的请求中额外携带 `image` 字段，系统会自动将请求路由到图像编辑处理流程。无需切换端点，接口统一、使用简单。

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model` | string | 是 | - | 固定值 `gpt-image-2` |
| `prompt` | string | 是 | - | 编辑描述文本 |
| `image` | string \| string[] | 是 | - | 单张图片传字符串，多张传数组；支持 URL 或 base64 data URI |
| `n` | integer | 否 | `1` | 生成图片数量，1-10 |
| `size` | string | 否 | `1024x1024` | 图片尺寸 |
| `quality` | string | 否 | `high` | 图片质量 |
| `input_fidelity` | string | 否 | - | 输入图片保真度: `low`/`medium`/`high` |

> `image` 字段传单张字符串或多张数组均可。输入图片数量无硬性上限，受上游总 token 限制约束；单张图片最大 50MB。

### 请求示例（单图编辑）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/generations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将背景改为海滩日落",
    "image": "https://example.com/photo.png",
    "size": "1024x1024",
    "quality": "high"
  }'
```

### 请求示例（多图融合）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/generations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将两张图片风格融合",
    "image": [
      "https://example.com/photo1.png",
      "https://example.com/photo2.png"
    ],
    "size": "1024x1024",
    "quality": "low",
    "n": 2
  }'
```

### 请求示例（base64 图片输入）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/generations" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将图片中的猫变成狗",
    "image": "data:image/png;base64,iVBORw0KGgo...",
    "size": "1536x1024",
    "quality": "medium",
    "input_fidelity": "high"
  }'
```

### 响应示例（单图图生图）

```json
{
  "created": 1776847985,
  "background": "opaque",
  "data": [
    {
      "b64_json": "iVBORw0KGgo..."
    }
  ],
  "output_format": "png",
  "quality": "high",
  "size": "1024x1024",
  "usage": {
    "input_tokens": 1037,
    "input_tokens_details": {
      "image_tokens": 1024,
      "text_tokens": 13
    },
    "output_tokens": 326,
    "total_tokens": 1363
  }
}
```

### 响应示例（多图图生图）

```json
{
  "created": 1776856022,
  "background": "opaque",
  "data": [
    {
      "b64_json": "iVBORw0KGgo..."
    },
    {
      "b64_json": "iVBORw0KGgo..."
    }
  ],
  "output_format": "png",
  "quality": "low",
  "size": "1024x1024",
  "usage": {
    "input_tokens": 2074,
    "input_tokens_details": {
      "image_tokens": 2048,
      "text_tokens": 26
    },
    "output_tokens": 415,
    "total_tokens": 2489
  }
}
```

> 图生图时 `input_tokens_details.image_tokens > 0`，反映输入图片消耗的 token 数。

---

## 图生图（edits 端点）

**POST** `https://api.gravitex.ai/v1/images/edits`

标准的 OpenAI 图像编辑端点，支持 JSON 和 multipart/form-data 两种请求格式。

### 方式一：JSON 格式

适用于传入图片 URL 或 base64 编码的图片。请求参数与上述 generations 端点的图生图完全一致。

#### 请求示例（单图）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/edits" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将背景改为海滩日落",
    "image": "https://example.com/photo.png",
    "size": "1024x1024",
    "quality": "high"
  }'
```

#### 请求示例（多图）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/edits" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "将两张图片风格融合",
    "image": [
      "https://example.com/photo1.png",
      "https://example.com/photo2.png"
    ],
    "size": "1024x1024",
    "quality": "low",
    "n": 2
  }'
```

### 方式二：multipart/form-data 格式

适用于直接上传图片文件。

#### 请求示例（单图）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=gpt-image-2" \
  -F "prompt=将背景改为海滩日落" \
  -F "image[]=@photo.png" \
  -F "size=1024x1024" \
  -F "quality=high"
```

#### 请求示例（多图）

```bash
curl -X POST "https://api.gravitex.ai/v1/images/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=gpt-image-2" \
  -F "prompt=将两张图片风格融合为赛博朋克风" \
  -F "image[]=@photo1.png" \
  -F "image[]=@photo2.png" \
  -F "size=1536x1024" \
  -F "quality=medium" \
  -F "n=2"
```

> **注意**: 多图时图片字段使用 `image[]`（即使只有一张图片也可用 `image[]`）。

---

## 响应字段说明

所有端点返回格式一致：

| 字段 | 类型 | 说明 |
|------|------|------|
| `created` | integer | 创建时间戳 |
| `background` | string | 背景类型（`opaque` 不透明 / `transparent` 透明） |
| `data` | array | 生成的图片数组 |
| `data[].b64_json` | string | base64 编码的图片数据 |
| `output_format` | string | 输出图片格式（`png` / `jpeg` / `webp`） |
| `quality` | string | 实际使用的图片质量 |
| `size` | string | 实际使用的图片尺寸 |
| `usage` | object | token 用量统计 |

### usage 字段说明

| 字段 | 说明 |
|------|------|
| `input_tokens` | 输入 token 总数（文本 + 图片） |
| `output_tokens` | 输出 token 总数（图片输出） |
| `total_tokens` | 总 token 数 |
| `input_tokens_details.text_tokens` | 文本输入 token 数 |
| `input_tokens_details.image_tokens` | 图片输入 token 数（文生图时为 0，图生图时 > 0） |

> **注意**:
> - GPT-Image-2 始终返回 base64 编码的图片数据（`b64_json`），不支持 `response_format=url`。
> - `output_tokens` 全部为图片输出 token，该模型无文本输出。
> - 计费按 token 维度区分：文本输入、图片输入、图片输出各有独立单价，详见[计费配置说明](./gpt-image-config.md)。

---

## 定价

所有价格均为 **每 1M tokens** 的美元价格，基于上游返回的 usage tokens 计费。

| 类型 | 价格 ($/1M tokens) |
|------|-------------------|
| Text Input Tokens | $5.00 |
| Image Input Tokens | $8.00 |
| Cached Text Input Tokens | $1.25 |
| Cached Image Input Tokens | $2.00 |
| Image Output Tokens | $30.00 |

---

## 注意事项

1. 图片生成通常需要 **10-30 秒**，具体取决于尺寸和质量设置
2. 始终返回 **base64 编码**的图片数据
3. 支持的输入/输出图片格式：PNG、JPEG、WebP
4. 4K 分辨率生成时间更长，建议优先使用标准尺寸
5. `quality` 设置为 `low` 可以显著加快生成速度
6. `input_fidelity` 参数仅在图生图模式下有效
7. 单张上传图片最大 **50MB**
8. 输出图片数量 `n` 范围为 **1-10**
9. 输入图片数量无硬性上限，受上游总 token 限制约束
10. 该模型无文本输出，`output_tokens` 全部为图片输出 token
11. **推荐使用 `/v1/images/generations` 端点**统一处理文生图和图生图，无需区分端点
