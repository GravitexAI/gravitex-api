# 对话模型传 GCS（gs://）媒体输入 —— 图片 / 音频 / 视频 / 文档

> 路径：`POST https://api.gravitex.ai/v1/chat/completions`
> 适用渠道：**仅 Vertex AI 渠道（channel type = 41）**。原生 Gemini 渠道（Gemini 开发者版 API）不支持裸 `gs://` 直传，传了会被平台直接拒绝，见 [§1.1](#11-为什么原生-gemini-渠道不支持)。

---

## 0. 先说结论

| 场景 | 是否支持 gs:// | 说明 |
| --- | --- | --- |
| **对话模型 + Vertex AI 渠道**（`/v1/chat/completions`，本文档） | ✅ 支持 | 图片 / 视频 / 音频 / PDF / 纯文本均可用 `gs://bucket/object` 直传，见下文 |
| **对话模型 + 原生 Gemini 渠道** | ❌ 不支持 | Gemini 开发者版 API 的 `fileData.fileUri` 不认裸 `gs://`，平台会直接报错拒绝，不会把错误请求发给上游，见 [§1.1](#11-为什么原生-gemini-渠道不支持) |
| 图片生成模型（`/v1/images/generations`，Gemini imagine "nano banana" 系列） | ❌ 暂不支持 | 参考图字段目前只认 `http(s)://` URL 或 base64，见 [§6](#6-暂不支持-gcs-的场景) |
| 视频生成模型（`/v1/videos`，Veo 系列） | ❌ 暂不支持 | 首帧/尾帧图片、续写视频目前只认 base64，上游 `storageUri` 输出参数是预留但未接通的死代码，见 [§6](#6-暂不支持-gcs-的场景) |

本文档只覆盖**对话模型 + Vertex AI 渠道**这一条已经打通的链路。其它场景如果有需求，需要单独排期开发，不要照抄本文档的写法去试图生图/生视频接口，也不要指望原生 Gemini 渠道能一样用。

---

## 1. 原理

对话模型的多模态输入原本的处理方式是：**下载 → 转 base64 → 塞进 `inlineData`**。这个方式对 `gs://` 完全不适用——`gs://` 不是 HTTP(S) 协议，平台没法直接下载，视频这种大文件也不适合内联成 base64（体积大、耗时长）。

Vertex AI 的 `generateContent` 接口原生支持第二种方式：**`fileData.fileUri` 直接引用 Cloud Storage 对象**，上游会自己去读这个 GCS 对象，平台完全不需要下载、不需要转码，只要把 `gs://...` 原样透传即可。官方字段说明：

> `fileUri`：Cloud Storage bucket URI —— 该对象必须公开可读，**或者与发起请求的 GCP 项目属于同一个项目**。
> 来源：https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/inference

平台现在会自动识别：内容块里的 URL 只要是 `gs://` 开头，且当前渠道是 Vertex AI（channel type 41），就走 `fileData.fileUri` 透传；`http(s)://` 或 base64 仍然走原来的下载 + `inlineData` 逻辑，行为不变。

### 1.1 为什么原生 Gemini 渠道不支持

平台的原生 Gemini 渠道调用的是 Google **Gemini 开发者版 API**（`generativelanguage.googleapis.com`），这条 API 上的 `fileData.fileUri` **不认裸 `gs://` URI**——直接传会被上游拒绝，报 `400 Invalid or unsupported file uri`（这是真实发生过的公开案例：https://stackoverflow.com/questions/78909501/gemini-api-error-400-invalid-or-unsupported-file-uri）。

Google 在 2026-01-12 官方博客（[Increased file size limits and expanded inputs support in Gemini API](https://blog.google/innovation-and-ai/technology/developers-tools/gemini-api-new-file-limits/)）里确实新增了 Gemini 开发者版 API 对 GCS 的支持，但走的是**完全不同的两步机制**：

1. 先用带 `https://www.googleapis.com/auth/devstorage.read_only` scope 的 OAuth 凭证，调 Files API 的 `files.register_files`，把 `gs://...` **注册**成一个 Gemini 自己的 File 资源（返回形如 `https://generativelanguage.googleapis.com/v1beta/files/xxx` 的 URI）；
2. 后续请求里传的是这个**注册后的 File URI**，不是原始的 `gs://...`。

这跟 Vertex AI 那种"请求里直接写 `gs://...` 就能读"的方式完全不是一回事，平台目前**没有实现**这套注册流程（需要额外的凭证 scope、注册接口调用、结果缓存等，工作量不小）。所以平台在识别到 `gs://` 时会先检查渠道类型，非 Vertex 渠道直接报错拒绝，不会把裸 `gs://` 当 fileUri 发给 Gemini 开发者版 API（那样只会拿到一个更难排查的上游 400）。

---

## 2. 前置条件

### 2.1 权限（重要，客户自己配置）

调用 Vertex AI 时用的是**该渠道配置的 service account**。跟一般的 GCS 场景不同，Vertex AI 的 `fileData.fileUri` 机制**不是**通用的"给 bucket 加 IAM 权限就能跨 project 访问"——它有专门的产品级限制（见 §1 引用的官方原文）：

> 目标对象必须满足以下**两个条件之一**：
> 1. **公开可读**（bucket/object 设置了公开读权限）；
> 2. **和发起请求的 Vertex AI 项目属于同一个 GCP 项目**。

也就是说：
- 如果 bucket 和这个 Vertex AI 渠道绑定的 GCP 项目是**同一个项目** → 不需要额外配置，直接能读。
- 如果 bucket 在**另一个项目**，且不是公开的 → **无法通过给 service account 单独授权 IAM 的方式绕过**，这跟其他 GCP 产品的常规跨项目授权方式不一样，是 Google 在这个字段上的产品限制，请提前跟客户说明清楚，不要按"授权 IAM 就能跨 project"的思路去排查问题。
- 如果客户确实需要跨 project，目前唯一可行的路径是把该 bucket/object 设为公开可读，或者把对象复制一份到 Vertex AI 渠道所在的项目里。

> 之前的版本文档在这里写过"授权 IAM 即可跨 project"，是不准确的，已按官方文档订正。

如果客户开了 VPC Service Controls / Domain Restricted Sharing 组织策略，同项目内的访问也可能被拦，需要客户自己在这些边界配置里放行，平台侧无法代为处理。

### 2.2 支持的 MIME 类型白名单

不管是 `gs://` 直传还是原来的下载方式，MIME 类型都要在这个白名单内，否则会被平台拒绝（400，不会发给上游）：

| 类别 | 支持的 MIME |
| --- | --- |
| 图片 | `image/png`、`image/jpeg`、`image/jpg`、`image/webp`、`image/heic`、`image/heif` |
| 音频 | `audio/mpeg`、`audio/mp3`、`audio/wav` |
| 视频 | `video/mp4`、`video/mov`、`video/mpeg`、`video/mpg`、`video/avi`、`video/wmv`、`video/mpegps`、`video/flv` |
| 文档 | `application/pdf`、`text/plain` |

### 2.3 MIME 类型怎么确定

`gs://` 对象没法像 `http(s)://` 那样下载后嗅探 `Content-Type`，MIME 类型按下面优先级确定：

1. **客户端显式传 `mime_type` 字段**（推荐，最稳妥）；
2. 没传时，按 `gs://` 路径里对象名的**扩展名**猜测（比如 `.mp4` → `video/mp4`）；
3. `file` 类型内容块还会兜底看 `file_name` 字段的扩展名；
4. 都猜不出来（比如对象名是纯 UUID、没有扩展名）→ 直接报错，要求显式传 `mime_type`。

> **建议**：只要对象名不带常见扩展名，一律显式传 `mime_type`，避免猜错或猜不出来。

---

## 3. 三种内容块写法

| 内容块类型 | 典型用途 | gs:// 写法 |
| --- | --- | --- |
| `image_url` | 图片（也可以复用来传视频，只是命名上叫 image） | `{"type":"image_url","image_url":{"url":"gs://...","mime_type":"..."}}` |
| `video_url` | 视频专用 | `{"type":"video_url","video_url":{"url":"gs://...","mime_type":"..."}}` |
| `file` | 音频 / PDF / 纯文本等其它文件（`input_audio` 类型只接受 base64，没有 URL 字段，所以音频走 `file`） | `{"type":"file","file":{"file_data":"gs://...","mime_type":"...","filename":"可选，用于兜底猜类型"}}` |

三种类型的 `mime_type` 字段都是可选的，不传就按 §2.3 的规则自动猜测。

---

## 4. 调用示例

以下示例均省略了部分无关字段，实际请求按需补充 `Authorization`、`Content-Type` 头。

### 4.1 图片（gs:// via `image_url`）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "这张图里有什么？" },
          { "type": "image_url", "image_url": { "url": "gs://my-bucket/images/cat.png" } }
        ]
      }
    ]
  }'
```

> `cat.png` 带扩展名，`mime_type` 可以省略，会自动猜成 `image/png`。

### 4.2 视频（gs:// via `video_url`）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "总结一下这段视频讲了什么，按时间轴列出关键节点" },
          { "type": "video_url", "video_url": { "url": "gs://my-bucket/videos/meeting.mp4" } }
        ]
      }
    ]
  }'
```

对象名没有扩展名，或者扩展名猜不准时，显式传 `mime_type`：

```json
{ "type": "video_url", "video_url": { "url": "gs://my-bucket/videos/obj-8f2a1c", "mime_type": "video/mp4" } }
```

### 4.3 音频（gs:// via `file`）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "把这段录音转成文字，并总结要点" },
          { "type": "file", "file": { "file_data": "gs://my-bucket/audio/call-2026-09-09.wav", "mime_type": "audio/wav" } }
        ]
      }
    ]
  }'
```

> 音频**不要用 `input_audio` 类型**——`input_audio` 协议本身只接受 base64 编码的 `data` 字段，没有 URL 字段，无法承载 `gs://`。音频统一走 `file` 类型。

### 4.4 PDF 文档（gs:// via `file`）

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "帮我提炼这份合同的关键条款" },
          { "type": "file", "file": { "file_data": "gs://my-bucket/docs/contract-final" , "filename": "contract-final.pdf" } }
        ]
      }
    ]
  }'
```

> `file_data` 本身（`gs://my-bucket/docs/contract-final`）没有扩展名，靠 `filename` 兜底猜出 `application/pdf`。两者都没有扩展名信息时必须显式传 `mime_type`。

### 4.5 一条消息里混合图片 + 视频 + 文本

```bash
curl -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "对比一下这张产品图和这段演示视频，指出不一致的地方" },
          { "type": "image_url", "image_url": { "url": "gs://my-bucket/images/product-shot.jpg" } },
          { "type": "video_url", "video_url": { "url": "gs://my-bucket/videos/demo.mov" } }
        ]
      }
    ]
  }'
```

### 4.6 流式调用（gs:// 视频 + stream）

```bash
curl -N -X POST https://api.gravitex.ai/v1/chat/completions \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.5-flash",
    "stream": true,
    "messages": [
      { "role": "user", "content": [
          { "type": "text", "text": "逐段解说这段视频" },
          { "type": "video_url", "video_url": { "url": "gs://my-bucket/videos/demo.mp4" } }
        ]
      }
    ]
  }'
```

gs:// 媒体输入和普通请求一样支持流式/非流式、`extra_body.google.*` 透传、`tools`/`googleSearch` 等所有既有能力，互不冲突。

### 4.7 Python（OpenAI SDK）

```python
from openai import OpenAI

client = OpenAI(
    api_key="sk-xxx",
    base_url="https://api.gravitex.ai/v1",
)

resp = client.chat.completions.create(
    model="gemini-3.5-flash",
    messages=[
        {
            "role": "user",
            "content": [
                {"type": "text", "text": "总结这段视频的关键信息"},
                {
                    "type": "video_url",
                    "video_url": {"url": "gs://my-bucket/videos/demo.mp4"},
                },
            ],
        }
    ],
)
print(resp.choices[0].message.content)
```

> `video_url` 不是 OpenAI 官方标准字段，用官方 SDK 时以 `dict` 形式直接塞进 `content` 数组即可（如上），SDK 不会做额外校验。

### 4.8 Node.js（OpenAI SDK）

```javascript
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "sk-xxx",
  baseURL: "https://api.gravitex.ai/v1",
});

const resp = await client.chat.completions.create({
  model: "gemini-3.5-flash",
  messages: [
    {
      role: "user",
      content: [
        { type: "text", text: "描述一下这份录音的内容" },
        {
          type: "file",
          file: { file_data: "gs://my-bucket/audio/note.mp3" },
        },
      ],
    },
  ],
});
console.log(resp.choices[0].message.content);
```

---

## 5. 报错场景

| 报错信息 | 原因 | 解决办法 |
| --- | --- | --- |
| `Cloud Storage (gs://) media input is only supported on a Vertex AI channel, got '...'` | 请求命中的是原生 Gemini 渠道，不是 Vertex AI 渠道（channel 41） | 确认这个模型在平台上路由到的是 Vertex AI 渠道；原生 Gemini 渠道目前不支持，见 §1.1 |
| `cannot determine mime type for Cloud Storage URI '...', please specify mime_type explicitly` | `gs://` 对象名没有扩展名（或扩展名无法识别），且没有传 `mime_type` | 在 `image_url` / `video_url` / `file` 里显式加 `mime_type` 字段 |
| `mime type is not supported by Gemini: '...'` | 猜出来 / 显式传的 `mime_type` 不在 §2.2 白名单内 | 换成白名单内的 MIME，或联系平台确认该格式是否可以加白名单 |
| 上游返回权限/找不到对象错误（403/400 等） | 目标对象既不是公开可读，也不在 Vertex AI 渠道所属的同一个 GCP 项目里（见 §2.1 的产品级限制，不是普通 IAM 问题） | 确认对象和 Vertex AI 渠道项目是否同一个项目，或者把对象设为公开可读 |

---

## 6. 暂不支持 GCS 的场景

以下两类接口目前**不支持** `gs://` 输入，传了会报错或被当成非法 base64 处理，不要照抄本文档的写法：

### 6.1 图片生成模型（`/v1/images/generations` / `/v1/images/edits`，imagine "nano banana" 系列）

参考图字段（`image`）目前只支持 `http(s)://` URL 或 base64，代码里没有对 `gs://` 做特殊处理，传 `gs://` 会被当成非法 base64 直接报错。

### 6.2 视频生成模型（`/v1/videos`，Veo 系列，channel 41）

- 首帧/尾帧图片输入目前只支持 base64，传 `gs://` 会被原样塞进 `bytesBase64Encoded` 字段，上游会报请求非法。
- 输出参数 `storageUri`（生成结果直接写回指定 GCS 路径而非返回 base64）在代码里有预留字段，但客户端请求解析函数并不会读取这个参数，等于没接通；即使接通了，返回结果解析结构体也还没加 `gcsUri` 字段去接收上游返回的路径。

这两块如果有实际客户需求，需要单独排期开发（工作量：图生成需要给 `image` 字段加 `gs://` 分支；视频生成需要打通请求侧 `storageUri`/`gcsUri` 输入输出解析，两处改动都在 `relay/channel/task/vertex/adaptor.go` 附近）。

### 6.3 对话模型 + 原生 Gemini 渠道

不是"没实现"，是 Google 官方在这条 API 上本身就不接受裸 `gs://`。要支持的话需要额外接入 Files API 的 `register_files` 两步注册机制（见 §1.1），工作量和 Vertex 那种直传完全不是一个量级，目前没有排期。

---

## 7. Google 官方 API 版本说明（v1beta / v1beta1）

平台里两个渠道分别打到 Google 两条不同的 API 上，版本号命名很像但完全是两回事：

| 渠道 | 打到的 Google API | 用的版本 | 是否支持 gs:// fileUri |
| --- | --- | --- | --- |
| 原生 Gemini 渠道 | Gemini 开发者版 API（`generativelanguage.googleapis.com`） | `v1beta`（平台固定用这个版本，见 `relay/channel/gemini/relay-gemini.go:2358`） | ❌ 不支持（需要走 Files API 注册，见 §1.1） |
| Vertex AI 渠道（channel 41） | Vertex AI API（`aiplatform.googleapis.com`） | `v1beta1`（平台固定用这个版本，见 `relay/channel/vertex/url_builder.go` 的 `GeminiAPIVersion` 常量） | ✅ 支持（同项目或公开可读，见 §2.1） |

**结论：不是同一个 `v1beta`。** `v1beta` 是 Gemini 开发者版 API 自己的版本号，`v1beta1` 是 Vertex AI API 的版本号，两边命名相似但是两套完全独立的 API、独立的鉴权方式、独立的功能集合。是否支持 `gs://` 由**打的是哪条 API** 决定，不是由版本号里带不带 `1` 决定：

- 即使 Vertex AI 未来切到 `v1`（`DefaultAPIVersion` 常量），`fileData.fileUri` 支持 `gs://` 这个能力也不会变，因为这是 Vertex AI API 本身的产品能力，跟 `v1`/`v1beta1` 版本区分无关（`v1beta1` 主要是为了拿 `urlContext`+`googleSearch` 组合等预览期特性，见 `relay/channel/vertex/url_builder.go` 里的注释）。
- Gemini 开发者版 API 不管是 `v1`、`v1beta` 还是 `v1alpha`，都不接受裸 `gs://` 直接塞进 `fileData.fileUri`——这是这条 API 的产品设计（GCS 走注册制），不是某个版本号还没升级到的问题。

---

## 8. 关联代码位置（维护用）

| 内容 | 位置 |
| --- | --- |
| `gs://` 识别、渠道类型校验与 MIME 猜测 | `relay/channel/gemini/relay-gemini.go` 的 `extractGCSMediaPart` / `guessMimeTypeFromGCSUri` |
| 转换入口（Gemini + Vertex 共用，内部按 `info.ChannelType` 分流） | `relay/channel/gemini/relay-gemini.go` 的 `CovertOpenAI2Gemini` |
| MIME 白名单 | `relay/channel/gemini/relay-gemini.go` 的 `geminiSupportedMimeTypes` |
| `video_url` / `file` 的 `mime_type` 字段定义 | `relaykit/dto/openai_request.go` 的 `MessageVideoUrl` / `MessageFile` |
| Vertex/Gemini 各自的 API 版本常量 | `relay/channel/vertex/url_builder.go`（`GeminiAPIVersion = "v1beta1"`） / `relay/channel/gemini/relay-gemini.go:2358`（`v1beta`） |
| 回归测试 | `relay/channel/gemini/gemini_gcs_media_test.go` |
