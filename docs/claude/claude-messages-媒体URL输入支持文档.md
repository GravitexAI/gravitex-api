# Claude Messages API 媒体 URL 直传支持文档（图片 / PDF）

> 用途：说明 `/v1/messages` 端点的 `image` / `document` content block 是否支持直接传入 URL（而非 base64），以及该能力在 **Anthropic 官方 API / Claude Platform on AWS / Amazon Bedrock / Google Vertex AI** 各渠道下的差异。
>
> 数据来源（联网核实，非训练记忆）：
> - `platform.claude.com/docs/en/build-with-claude/vision.md`（官方 Vision 文档）
> - `platform.claude.com/docs/en/build-with-claude/pdf-support.md`（官方 PDF 支持文档）
> - `platform.claude.com/docs/en/build-with-claude/claude-on-amazon-bedrock.md`（官方 Bedrock 文档）
>
> 更新时间：2026-07-21

---

## 0. 结论先行

| | 图片 URL (`source.type: "url"`) | PDF/文档 URL (`source.type: "url"`) | Files API (`file_id`) |
|---|---|---|---|
| **Anthropic 官方 API** | ✅ 支持 | ✅ 支持 | ✅ 支持（beta） |
| **Claude Platform on AWS** | ✅ 支持（与官方同步） | ✅ 支持 | ✅ 支持 |
| **Amazon Bedrock** | ❌ 不支持，仅 base64 | ❌ 不支持，仅 base64 | ❌ 不支持 |
| **Google Vertex AI** | ❌ 不支持，仅 base64 | ❌ 不支持，仅 base64 | ❌ 不支持 |

官方原话（Vision 文档 & PDF 文档均有此提示）：

> On Amazon Bedrock and Google Cloud, only base64-encoded sources are currently available.

官方 Bedrock 文档「Features not supported」明确列出：

> **Input sources (URL sources for images and documents, Files API)**

**对本网关的意义**：如果渠道是 Bedrock 或 Vertex，网关必须在转发前把客户端传来的图片/文档 URL 拉取下来并转成 base64，不能把 `source.type: "url"` 直接透传给这两个渠道，否则上游会报 `invalid_request_error`。

---

## 1. 图片 URL 输入

### 1.1 请求结构

```json
{
  "type": "image",
  "source": {
    "type": "url",
    "url": "https://example.com/image.png"
  }
}
```

三种 `source.type` 均受官方支持：`base64` / `url` / `file`（Files API）。

### 1.2 curl 示例

```bash
curl https://api.anthropic.com/v1/messages \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-opus-4-8",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "type": "image",
            "source": {
              "type": "url",
              "url": "https://upload.wikimedia.org/wikipedia/commons/a/a7/Camponotus_flavomarginatus_ant.jpg"
            }
          },
          {
            "type": "text",
            "text": "Describe this image."
          }
        ]
      }
    ]
  }'
```

### 1.3 限制

- 支持格式：JPEG、PNG、GIF、WebP（动图仅取第一帧）
- 官方 API 单图 base64 上限 10MB；Bedrock / Vertex 是 5MB（但这两个渠道本来就不接受 URL，只能走 base64）
- 单请求最多 600 张图（200k 上下文模型为 100 张）；超过 20 张图/文档 block 时，单图分辨率限制更严格

---

## 2. PDF / 文档 URL 输入

### 2.1 请求结构

```json
{
  "type": "document",
  "source": {
    "type": "url",
    "url": "https://example.com/some.pdf"
  }
}
```

同样支持三种方式：`url` / `base64` / `file`（Files API）。

### 2.2 curl 示例

```bash
curl https://api.anthropic.com/v1/messages \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-opus-4-8",
    "max_tokens": 1024,
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "type": "document",
            "source": {
              "type": "url",
              "url": "https://assets.anthropic.com/m/1cd9d098ac3e6467/original/Claude-3-Model-Card-October-Addendum.pdf"
            }
          },
          {
            "type": "text",
            "text": "What are the key findings in this document?"
          }
        ]
      }
    ]
  }'
```

### 2.3 限制

- 请求总大小上限 32MB（含同请求内其他内容），Bedrock 是 20MB
- 单请求最多 600 页（上下文窗口 < 1M tokens 的模型为 100 页）
- 仅支持标准 PDF（不能有密码/加密）
- Bedrock 走 **Converse API** 时，视觉理解（图表/图片）需要显式开启 `citations`，否则自动降级为纯文本抽取；走 **InvokeModel API** 则无此限制

---

## 3. Bedrock / Vertex 渠道降级方案（base64-only）

由于这两个渠道不接受 `source.type: "url"`，网关侧需要自行拉取 URL 内容并转 base64 后再转发。以下是等价请求形态：

### 3.1 图片（Bedrock 示例，走 Mantle 客户端对应的 `/v1/messages` 形态）

```bash
curl -sL "https://upload.wikimedia.org/wikipedia/commons/a/a7/Camponotus_flavomarginatus_ant.jpg" \
  | base64 | tr -d '\n' > image_base64.txt

jq -n --rawfile IMAGE_BASE64 image_base64.txt '{
  "model": "anthropic.claude-opus-4-8",
  "max_tokens": 1024,
  "messages": [{
    "role": "user",
    "content": [
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/jpeg",
          "data": $IMAGE_BASE64
        }
      },
      { "type": "text", "text": "Describe this image." }
    ]
  }]
}' > request.json
```

### 3.2 PDF（同理，media_type 换成 `application/pdf`）

```bash
curl -sL "https://example.com/some.pdf" | base64 | tr -d '\n' > pdf_base64.txt

jq -n --rawfile PDF_BASE64 pdf_base64.txt '{
  "model": "anthropic.claude-opus-4-8",
  "max_tokens": 1024,
  "messages": [{
    "role": "user",
    "content": [
      {
        "type": "document",
        "source": {
          "type": "base64",
          "media_type": "application/pdf",
          "data": $PDF_BASE64
        }
      },
      { "type": "text", "text": "What are the key findings in this document?" }
    ]
  }]
}' > request.json
```

> Bedrock 模型 ID 需加 `anthropic.` 前缀；Vertex 当前代模型不加前缀，用裸模型 ID（如 `claude-opus-4-8`），日期快照模型用 `@` 分隔（如 `claude-opus-4-5@20251101`）。

---

## 4. 快速排查

| 现象 | 原因 | 处理 |
|---|---|---|
| Bedrock/Vertex 渠道请求携带 `source.type: "url"` 返回 `invalid_request_error` | 这两个渠道不支持 URL source | 网关侧拉取 URL 转 base64 后再转发 |
| 官方 API 直连渠道用 `url` source 正常，切到 Bedrock/Vertex 后报错 | 渠道差异，不是网关 bug | 属预期行为，需按渠道类型分支处理 |
| Files API `file_id` 在 Bedrock/Vertex 上报错 | Files API 在这两个渠道均不支持 | 同样需要网关侧转 base64 兜底 |
