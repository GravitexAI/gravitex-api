# Gemini Omni Flash Preview API

通过 Gravitex API 调用 Google `gemini-omni-flash-preview` 视频生成模型。

本文档面向 API 使用方，说明认证、接口、请求参数、轮询、响应、计费和限制。

- [认证](#2-认证)
- [模型与接口总览](#3-模型与接口总览)
- [平台视频接口](#4-平台视频接口)
- [Google 原生格式兼容接口](#5-google-原生格式兼容接口)
- [图生视频](#6-图生视频)
- [响应与轮询](#7-响应与轮询)
- [结果交付](#8-结果交付)
- [计费与用量](#9-计费与用量)
- [错误处理](#10-错误处理)

## 1. 能力概览

| 能力 | 平台支持 | 说明 |
| --- | --- | --- |
| 文生视频 | 支持 | `input` 传文本，内部使用 `text_to_video` |
| 图生视频 | 支持 | `input` 同时传文本和 Base64 图片，内部使用 `image_to_video` |
| 视频输出 | 支持 | 返回最终视频地址 |
| 视频音频 | 支持 | - |
| 异步任务 | 支持 | 提交任务后通过 GET 轮询 |
| 同步阻塞调用 | 不支持 | - |
| SSE 流式输出 | 不支持 | - |
| 视频编辑 | 不支持 | - |
| 多图参考 | 不支持 | - |

## 2. 认证

请求使用平台 Token：

```http
Authorization: Bearer sk-xxxxxxxxxxxxxxxx
Content-Type: application/json
```

平台 API 地址：

```text
https://api.gravitex.ai
```

以下示例均以 `https://api.gravitex.ai` 为例。

## 3. 模型与接口总览

| 模型 ID | 类型 | 文生视频 | 图生视频 | 输出 | 任务方式 |
| --- | --- | --- | --- | --- | --- |
| `gemini-omni-flash-preview` | Gemini Omni Flash Preview | 支持 | 支持单图 | 视频（含音频） | 异步提交 + 轮询 |

| 接口 | 方法 | 用途 |
| --- | --- | --- |
| `/v1/videos` | POST | OpenAI 视频格式提交任务 |
| `/v1/videos/{task_id}` | GET | OpenAI 视频格式查询任务 |
| `/v1beta/interactions` | POST | Google Interactions 格式提交任务 |
| `/v1beta/interactions/{interaction_id}` | GET | Google Interactions 格式查询任务 |

> `/v1beta` 兼容接口最终仍使用平台视频任务管线，不会改变其他模型的路由行为。

## 4. 平台视频接口

### 4.1 提交视频任务

```http
POST https://api.gravitex.ai/v1/video/generations
```

请求示例：

```json
{
  "model": "gemini-omni-flash-preview",
  "prompt": "生成一只正在草地上吃草的小羊",
  "width": 1280,
  "height": 720,
  "duration": 3,
  "seed": -1,
  "metadata": {
    "seconds": 3,
    "resolution": "720p",
    "aspectRatio": "16:9",
    "durationSeconds": 3,
    "watermark": false,
    "camera_fixed": false
  }
}
```

`prompt` 是必填字段。对于 Gemini Omni，平台主要使用以下字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model` | string | 固定为 `gemini-omni-flash-preview` |
| `prompt` | string | 文本提示词，必填 |
| `metadata.resolution` | string | 当前推荐 `720p` |
| `metadata.aspectRatio` | string | `16:9` 或 `9:16` |
| `metadata.durationSeconds` | number | 当前建议 3–10 秒，实际受上游限制 |
| `metadata.image` | string | 图生视频时传图片 Data URL |

提交成功后返回平台任务：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "object": "video",
    "model": "gemini-omni-flash-preview",
    "status": "in_progress",
    "progress": 0
  }
}
```

### 4.2 轮询任务

```http
GET https://api.gravitex.ai/v1/video/generations/{task_id}
```

轮询直到 `status` 为成功或失败：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "object": "video",
    "model": "gemini-omni-flash-preview",
    "status": "completed",
    "progress": 100,
    "url": "https://your-oss-host/video/xxxxxxxx.mp4"
  }
}
```

实际状态可能经历 `in_progress`。如果上游仍在生成，平台继续保留任务并由后台轮询；如果上游返回失败，平台返回失败原因，不应把 `unknown` 当作最终成功。

## 5. Google Interactions 格式兼容接口

本平台额外提供 Google Interactions API 形状的兼容路由。它只改变外部请求和响应格式，内部仍复用平台现有的视频任务、轮询、结果交付和计费链路。

### 5.1 提交与查询地址

```http
POST https://api.gravitex.ai/v1beta/interactions
GET  https://api.gravitex.ai/v1beta/interactions/{interaction_id}
```

### 5.2 原生格式提交示例

```bash
curl -X POST "https://api.gravitex.ai/v1beta/interactions" \
  -H "Authorization: Bearer sk-xxxxxxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-omni-flash-preview",
    "input": "生成一只正在草地上吃草的小羊",
    "generation_config": {
      "video_config": {
        "task": "text_to_video"
      }
    },
    "response_format": {
      "type": "video",
      "aspect_ratio": "16:9",
      "duration": "3s"
    },
    "background": true
  }'
```

原生格式提交返回 Interaction 风格的任务信息：

```json
{
  "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "object": "interaction",
  "model": "gemini-omni-flash-preview",
  "status": "in_progress"
}
```

### 5.3 原生格式轮询示例

```bash
curl "https://api.gravitex.ai/v1beta/interactions/video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" \
  -H "Authorization: Bearer sk-xxxxxxxxxxxxxxxx"
```

完成后返回：

```json
{
  "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "object": "interaction",
  "model": "gemini-omni-flash-preview",
  "status": "completed",
  "steps": [
    {
      "type": "video",
      "content": [
        {
          "type": "video",
          "uri": "https://your-oss-host/video/xxxxxxxx.mp4"
        }
      ]
    }
  ]
}
```

## 6. 图生视频

当前平台兼容路由支持 `input` 数组中的文本和单张图片 Data URL：

```json
{
  "model": "gemini-omni-flash-preview",
  "input": [
    {
      "type": "text",
      "text": "让这张图片中的小羊在草地上慢慢走动"
    },
    {
      "type": "image",
      "mime_type": "image/png",
      "data": "iVBORw0KGgoAAAANSUhEUg..."
    }
  ],
  "generation_config": {
    "video_config": {
      "task": "image_to_video"
    }
  },
  "response_format": {
    "type": "video",
    "aspect_ratio": "16:9",
    "duration": "3s"
  },
  "background": true
}
```

平台会把图片转换为内部视频任务所需的图片输入，并沿用现有渠道选择、任务持久化和结果交付流程。当前兼容适配器不会把 `image.uri` 直接转换为图片输入；需要图片时优先传 `data`。

## 7. 响应与轮询

平台接口和原生兼容接口均为异步任务。提交接口只返回任务 ID 和进行中状态；客户端应按提交响应中的 ID 轮询，直到任务完成或失败。

平台视频接口完成响应示例：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
    "object": "video",
    "model": "gemini-omni-flash-preview",
    "status": "completed",
    "progress": 100,
    "url": "https://your-oss-host/video/xxxxxxxx.mp4"
  }
}
```

原生兼容接口完成响应示例：

```json
{
  "id": "video-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "object": "interaction",
  "model": "gemini-omni-flash-preview",
  "status": "completed",
  "steps": [
    {
      "type": "video",
      "content": [
        {
          "type": "video",
          "uri": "https://your-oss-host/video/xxxxxxxx.mp4"
        }
      ]
    }
  ]
}
```

状态映射：

| 平台任务状态 | 原生兼容状态 | 说明 |
| --- | --- | --- |
| `NOT_START` / `IN_PROGRESS` | `in_progress` | 继续轮询 |
| `SUCCESS` | `completed` | 可以读取视频地址 |
| `FAILURE` | `failed` | 读取错误信息并结束轮询 |

## 8. 计费与用量

Google 官方 Gemini Omni 价格为：输入 $1.50 / 1M tokens，文本输出（回答和推理）$9 / 1M tokens，视频输出 $0.10 / 秒；官方同时给出 720p、含音频视频输出按 $17.50 / 1M 视频输出 tokens 计价的等价口径。[官方价格页](https://cloud.google.com/gemini-enterprise-agent-platform/generative-ai/pricing#gemini-omni)

| 上游字段 | 平台含义 | 平台计费 |
| --- | --- | --- |
| `total_input_tokens` 或 `input_tokens_by_modality` | 输入 token | 输入价格 $1.50 / 1M |
| `total_thought_tokens`、文本输出模态 | 文本回答和推理 token | 文本输出价格 $9 / 1M |
| `output_tokens_by_modality[modality=video]` | 视频输出 token | 视频输出价格 $17.50 / 1M |
| `total_tokens` | 上游总用量记录 | 用于审计，不直接替代三类计费拆分 |


