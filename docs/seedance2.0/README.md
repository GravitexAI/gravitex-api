# Seedance 2.0 视频生成 API 用户文档

Seedance 2.0 是一款先进的 AI 视频生成模型，支持文生视频、图生视频（首帧/首尾帧）、多模态参考生成以及面部一致性素材库。通过 Gravitex AI 网关，您可以使用统一的 API 接口调用 Seedance 2.0 的全部能力。

**Base URL**: `https://api.gravitex.ai`

---

## 目录

- [认证](#认证)
- [模型列表](#模型列表)
- [创建视频生成任务](#创建视频生成任务)
- [查询任务状态](#查询任务状态)
- [使用场景](#使用场景)
  - [文生视频](#1-文生视频)
  - [图生视频（首帧驱动）](#2-图生视频首帧驱动)
  - [图生视频（首尾帧驱动）](#3-图生视频首尾帧驱动)
  - [多模态参考生成](#4-多模态参考生成)
  - [面部一致性（素材库）](#5-面部一致性素材库)
- [素材库 API](#素材库-api)
- [参数参考](#参数参考)
- [错误处理](#错误处理)
- [完整代码示例](#完整代码示例)
- [常见问题](#常见问题)

---

## 认证

所有接口使用 **Bearer Token** 认证。在 Gravitex AI 平台创建令牌后，在请求头中添加：

```
Authorization: Bearer sk-{your_token_key}
```

所有请求均使用 JSON 格式：

```
Content-Type: application/json
```

---

## 模型列表

| 模型 ID | 说明 | 特点 |
|---------|------|------|
| `seedance-2-0-pro` | Seedance 2.0 专业版 | 更高质量，适合正式生产 |
| `seedance-2-0-fast` | Seedance 2.0 快速版 | 更快速度，适合快速迭代 |

---

## 创建视频生成任务

**POST** `https://api.gravitex.ai/v1/video/generations`

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model` | string | **是** | — | 模型 ID：`seedance-2-0-pro` 或 `seedance-2-0-fast` |
| `content` | array | 否* | — | 内容数组，包含文本提示、图片、视频、音频（详见下方） |
| `prompt` | string | 否* | — | 文本提示词（`content` 的简化替代，两者二选一） |
| `duration` | integer | 否 | `5` | 视频时长，范围 4~15 秒 |
| `resolution` | string | 否 | `"720p"` | 分辨率：`480p` 或 `720p` |
| `ratio` | string | 否 | `"16:9"` | 画面比例：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive` |
| `generate_audio` | boolean | 否 | `true` | 是否自动生成音频 |
| `seed` | integer | 否 | `-1`（随机） | 随机种子，固定种子可复现结果 |

> \* `content` 和 `prompt` 至少提供一个。推荐使用 `content` 数组，功能更强大。

### Content 数组详解

`content` 是一个数组，每个元素代表一种输入类型：

| type | role | 说明 | 数量限制 |
|------|------|------|----------|
| `text` | — | 文本提示词，描述想要生成的视频内容 | 1 条 |
| `image_url` | `first_frame` | **首帧图片**，视频将以此图片作为第一帧开始生成 | 1 张 |
| `image_url` | `last_frame` | **尾帧图片**，需搭配 `first_frame` 使用 | 1 张 |
| `image_url` | `reference_image` | **参考图片**，提供视觉参考风格 | 最多 9 张 |
| `video_url` | `reference_video` | **参考视频**，提供运动/风格参考 | 最多 3 个 |
| `audio_url` | `reference_audio` | **参考音频**，提供音乐/声音参考 | 最多 3 段 |

**互斥规则：**
- `first_frame` 与 `reference_image` **互斥**，不能同时使用
- `last_frame` 必须搭配 `first_frame`
- `reference_audio` 需搭配图片或视频输入

### 响应

```json
{
  "id": "ut-abc123def456",
  "task_id": "ut-abc123def456",
  "object": "video",
  "model": "seedance-2-0-pro",
  "status": "queued",
  "progress": 0,
  "created_at": 1712563200
}
```

---

## 查询任务状态

**GET** `https://api.gravitex.ai/v1/video/generations/{task_id}`

提交任务后，通过轮询此接口获取生成进度和最终结果。

### 请求

```
Authorization: Bearer sk-{your_token_key}
```

### 响应 — 生成中

```json
{
  "id": "ut-abc123def456",
  "task_id": "ut-abc123def456",
  "object": "video",
  "model": "seedance-2-0-pro",
  "status": "in_progress",
  "progress": 50,
  "created_at": 1712563200
}
```

### 响应 — 生成成功

```json
{
  "id": "ut-abc123def456",
  "task_id": "ut-abc123def456",
  "object": "video",
  "model": "seedance-2-0-pro",
  "status": "completed",
  "progress": 100,
  "video_url": "https://uptoken.cc/v1/media/proxy?...",
  "url": "https://uptoken.cc/v1/media/proxy?...",
  "created_at": 1712563200,
  "completed_at": 1712563320,
  "metadata": {
    "url": "https://uptoken.cc/v1/media/proxy?...",
    "video_url": "https://uptoken.cc/v1/media/proxy?...",
    "id": "ut-abc123def456",
    "status": "succeeded",
    "usage": {
      "total_tokens": 97605
    }
  }
}
```

### 响应 — 生成失败

```json
{
  "id": "ut-abc123def456",
  "task_id": "ut-abc123def456",
  "object": "video",
  "model": "seedance-2-0-pro",
  "status": "failed",
  "progress": 100,
  "created_at": 1712563200,
  "error": {
    "message": "Content moderation failed",
    "code": "failed"
  }
}
```

### 状态流转

```
queued → in_progress → completed / failed
```

| 状态 | 说明 |
|------|------|
| `queued` | 任务已提交，排队中 |
| `in_progress` | 正在生成 |
| `completed` | 生成成功，`video_url` 字段可用 |
| `failed` | 生成失败，查看 `error.message` 获取原因 |

**建议轮询间隔**：每 5 秒查询一次，视频通常在 30~120 秒内生成完毕。

---

## 使用场景

### 1. 文生视频

仅通过文本描述生成视频，最基础的使用方式。

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "黄金时刻，无人机航拍连绵山脉，云海翻涌，阳光洒落"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

也可以使用简化的 `prompt` 字段：

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "prompt": "黄金时刻，无人机航拍连绵山脉，云海翻涌，阳光洒落",
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }'
```

---

### 2. 图生视频（首帧驱动）

提供一张图片作为视频的第一帧，AI 在此基础上生成动态视频。

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "镜头缓慢推进，花瓣随风飘落"},
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/garden.jpg"},
        "role": "first_frame"
      }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }'
```

> **注意**：当使用 `first_frame` 时，`ratio` 参数建议设为 `adaptive`，让模型自动匹配图片比例。

---

### 3. 图生视频（首尾帧驱动）

同时提供首帧和尾帧图片，AI 生成从首帧到尾帧的过渡视频。

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "日出到日落的延时摄影效果"},
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/sunrise.jpg"},
        "role": "first_frame"
      },
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/sunset.jpg"},
        "role": "last_frame"
      }
    ],
    "duration": 10,
    "resolution": "720p",
    "ratio": "adaptive"
  }'
```

---

### 4. 多模态参考生成

同时使用参考图片、参考视频、参考音频来控制生成效果。

#### 4.1 参考图片生成

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "一个女孩在樱花树下奔跑"},
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/girl-portrait.jpg"},
        "role": "reference_image"
      },
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/sakura-scene.jpg"},
        "role": "reference_image"
      }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }'
```

#### 4.2 参考视频 + 参考音频

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "一段充满活力的舞蹈视频"},
      {
        "type": "video_url",
        "video_url": {"url": "https://example.com/dance-reference.mp4"},
        "role": "reference_video"
      },
      {
        "type": "audio_url",
        "audio_url": {"url": "https://example.com/upbeat-music.mp3"},
        "role": "reference_audio"
      }
    ],
    "duration": 10,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": false
  }'
```

> 当提供了 `reference_audio` 时，建议将 `generate_audio` 设为 `false`，避免自动生成的音频与参考音频冲突。

---

### 5. 面部一致性（素材库）

素材库用于上传人像照片，经处理后可在视频生成时保持面部特征的一致性。

**完整流程：**

```
1. 上传人像 → POST /v1/assets
2. 等待状态变为 active → GET /v1/assets（轮询）
3. 在视频生成中引用 → POST /v1/video/generations（使用 asset:// URL）
```

#### 步骤 1：上传人像素材

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -F "file=@portrait.jpg"
```

响应：

```json
{
  "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
  "asset_url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
  "url": "https://r2.uptoken.cc/...",
  "filename": "portrait.jpg",
  "content_type": "image/jpeg",
  "size_bytes": 1827362,
  "status": "pending",
  "created_at": "2026-04-08T12:00:00Z"
}
```

#### 步骤 2：等待素材处理完成

```bash
curl https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key"
```

轮询直到素材 `status` 变为 `active`（通常需要 1~3 分钟）。

#### 步骤 3：使用素材生成面部一致性视频

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [
      {"type": "text", "text": "一个女孩在海边跳舞，阳光明媚"},
      {
        "type": "image_url",
        "image_url": {"url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44"},
        "role": "reference_image"
      }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }'
```

> **重要**：
> - 使用 `asset://` 协议引用素材，而不是 HTTP URL
> - 仅 `active` 状态的素材可用于视频生成
> - 网关会验证素材所有权，您只能使用自己上传的素材

---

## 素材库 API

素材库为每个用户独立管理，用户只能查看和操作自己的素材。

### 上传素材

**POST** `https://api.gravitex.ai/v1/assets`

`Content-Type: multipart/form-data`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file | File | 是 | 人像图片文件 |

**支持格式**：JPEG、PNG、WebP、GIF、HEIC

**大小限制**：最大 30MB

**图片要求**：300~6000px，清晰的人脸正面照效果最佳

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -F "file=@portrait.jpg"
```

---

### 列出素材

**GET** `https://api.gravitex.ai/v1/assets`

返回当前用户所有素材，自动刷新处理中的素材状态。

```bash
curl https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key"
```

响应：

```json
{
  "assets": [
    {
      "id": 1,
      "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
      "asset_url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
      "url": "https://r2.uptoken.cc/...",
      "filename": "portrait.jpg",
      "content_type": "image/jpeg",
      "size_bytes": 1827362,
      "status": "active",
      "created_at": 1712563200,
      "updated_at": 1712563260
    }
  ],
  "total": 1
}
```

---

### 查询单个素材

**GET** `https://api.gravitex.ai/v1/assets/{virtual_id}`

```bash
curl https://api.gravitex.ai/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer sk-your_token_key"
```

---

### 删除素材

**DELETE** `https://api.gravitex.ai/v1/assets/{virtual_id}`

```bash
curl -X DELETE https://api.gravitex.ai/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer sk-your_token_key"
```

响应：

```json
{
  "deleted": true,
  "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44"
}
```

---

### 素材状态

| 状态 | 说明 |
|------|------|
| `pending` | 素材已上传，正在异步处理中 |
| `ready` | 素材已存储，正在进行人脸识别 |
| `active` | 素材就绪，可用于面部一致性视频生成 |
| `failed` | 素材处理失败，请检查图片质量后重新上传 |

---

## 参数参考

### 视频参数

| 参数 | 可选值 | 说明 |
|------|--------|------|
| `duration` | `4` ~ `15` | 输出视频时长（秒） |
| `resolution` | `480p`, `720p` | 输出分辨率 |
| `ratio` | `16:9`, `9:16`, `1:1`, `4:3`, `3:4`, `21:9`, `adaptive` | 画面比例 |
| `generate_audio` | `true`, `false` | 是否自动生成音频 |
| `seed` | `-1` 或正整数 | 随机种子，`-1` 为随机 |

### 图片输入限制

| 项目 | 限制 |
|------|------|
| 格式 | JPEG、PNG、WebP |
| 大小 | 最大 30MB |
| 分辨率 | 300~6000px |
| 参考图片数量 | 最多 9 张（`reference_image`） |
| 首帧/尾帧 | 各 1 张 |

### 视频输入限制

| 项目 | 限制 |
|------|------|
| 格式 | MP4、MOV |
| 大小 | 最大 50MB |
| 单个时长 | 2~15 秒 |
| 总时长 | 最多 15 秒 |
| 数量 | 最多 3 个（`reference_video`） |

### 音频输入限制

| 项目 | 限制 |
|------|------|
| 格式 | WAV、MP3 |
| 大小 | 最大 15MB |
| 时长 | 2~15 秒/段 |
| 数量 | 最多 3 段（`reference_audio`） |
| 前提 | 需搭配图片或视频输入 |

---

## 错误处理

### HTTP 状态码

| 状态码 | 含义 | 处理方式 |
|--------|------|---------|
| `200` | 成功 | — |
| `400` | 请求参数错误 | 检查参数格式和取值范围 |
| `401` | 未授权 | 检查 API Key 是否正确 |
| `402` | 余额不足 | 前往平台充值 |
| `429` | 请求频繁 | 降低请求频率，稍后重试 |
| `502` | 上游服务错误 | 等待片刻后重试 |

### 错误响应格式

```json
{
  "error": {
    "message": "具体错误描述",
    "type": "invalid_request_error"
  }
}
```

---

## 完整代码示例

### Python

```python
import requests
import time

BASE_URL = "https://api.gravitex.ai"
API_KEY = "sk-your_token_key"
HEADERS = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
}


def generate_video(content, duration=5, resolution="720p", ratio="16:9"):
    """提交视频生成任务并轮询直到完成"""
    # 1. 提交任务
    resp = requests.post(
        f"{BASE_URL}/v1/video/generations",
        headers=HEADERS,
        json={
            "model": "seedance-2-0-pro",
            "content": content,
            "duration": duration,
            "resolution": resolution,
            "ratio": ratio,
            "generate_audio": True,
        },
    )
    resp.raise_for_status()
    task_id = resp.json()["id"]
    print(f"任务已提交: {task_id}")

    # 2. 轮询结果
    while True:
        result = requests.get(
            f"{BASE_URL}/v1/video/generations/{task_id}",
            headers=HEADERS,
        ).json()

        status = result["status"]
        progress = result.get("progress", 0)
        print(f"状态: {status}, 进度: {progress}%")

        if status == "completed":
            video_url = result.get("video_url") or result.get("url")
            print(f"生成成功! 视频地址: {video_url}")
            return video_url
        elif status == "failed":
            error_msg = result.get("error", {}).get("message", "未知错误")
            print(f"生成失败: {error_msg}")
            return None

        time.sleep(5)


# === 示例 1：文生视频 ===
print("--- 文生视频 ---")
generate_video(
    content=[
        {"type": "text", "text": "黄金时刻，无人机航拍连绵山脉，云海翻涌"}
    ],
    duration=5,
)

# === 示例 2：图生视频（首帧） ===
print("\n--- 图生视频 ---")
generate_video(
    content=[
        {"type": "text", "text": "镜头缓缓推进，花瓣随风飘落"},
        {
            "type": "image_url",
            "image_url": {"url": "https://example.com/garden.jpg"},
            "role": "first_frame",
        },
    ],
    ratio="adaptive",
)

# === 示例 3：面部一致性视频 ===
print("\n--- 面部一致性视频（素材库） ---")

# 3a. 上传人像素材
upload_resp = requests.post(
    f"{BASE_URL}/v1/assets",
    headers={"Authorization": f"Bearer {API_KEY}"},
    files={"file": open("portrait.jpg", "rb")},
)
asset = upload_resp.json()
asset_url = asset["asset_url"]
print(f"素材已上传: {asset_url}, 状态: {asset['status']}")

# 3b. 等待素材就绪
while True:
    assets_resp = requests.get(
        f"{BASE_URL}/v1/assets",
        headers=HEADERS,
    ).json()

    my_asset = next(
        (a for a in assets_resp["assets"] if a["asset_url"] == asset_url), None
    )
    if my_asset and my_asset["status"] == "active":
        print("素材已就绪!")
        break
    elif my_asset and my_asset["status"] == "failed":
        print("素材处理失败，请重新上传")
        exit(1)

    print(f"素材处理中... 状态: {my_asset['status'] if my_asset else 'unknown'}")
    time.sleep(10)

# 3c. 使用素材生成视频
generate_video(
    content=[
        {"type": "text", "text": "一个女孩在海边跳舞，阳光明媚"},
        {
            "type": "image_url",
            "image_url": {"url": asset_url},
            "role": "reference_image",
        },
    ],
)
```

### JavaScript / Node.js

```javascript
const BASE_URL = "https://api.gravitex.ai";
const API_KEY = "sk-your_token_key";

async function generateVideo(content, options = {}) {
  const { duration = 5, resolution = "720p", ratio = "16:9" } = options;

  // 1. 提交任务
  const submitResp = await fetch(`${BASE_URL}/v1/video/generations`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model: "seedance-2-0-pro",
      content,
      duration,
      resolution,
      ratio,
      generate_audio: true,
    }),
  });
  const { id: taskId } = await submitResp.json();
  console.log(`任务已提交: ${taskId}`);

  // 2. 轮询结果
  while (true) {
    const pollResp = await fetch(
      `${BASE_URL}/v1/video/generations/${taskId}`,
      { headers: { Authorization: `Bearer ${API_KEY}` } }
    );
    const result = await pollResp.json();
    console.log(`状态: ${result.status}, 进度: ${result.progress}%`);

    if (result.status === "completed") {
      const videoUrl = result.video_url || result.url;
      console.log(`生成成功! ${videoUrl}`);
      return videoUrl;
    } else if (result.status === "failed") {
      console.error(`生成失败: ${result.error?.message}`);
      return null;
    }

    await new Promise((r) => setTimeout(r, 5000));
  }
}

// 文生视频
generateVideo([{ type: "text", text: "黄金时刻，无人机航拍连绵山脉" }]);
```

### cURL — 完整流程

```bash
# 1. 提交文生视频任务
TASK_ID=$(curl -s -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-pro",
    "content": [{"type": "text", "text": "黄金时刻无人机航拍山脉"}],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9"
  }' | jq -r '.id')

echo "任务ID: $TASK_ID"

# 2. 轮询结果（每5秒查询一次）
while true; do
  RESULT=$(curl -s https://api.gravitex.ai/v1/video/generations/$TASK_ID \
    -H "Authorization: Bearer sk-your_token_key")

  STATUS=$(echo $RESULT | jq -r '.status')
  echo "状态: $STATUS"

  if [ "$STATUS" = "completed" ]; then
    echo "视频地址: $(echo $RESULT | jq -r '.video_url')"
    break
  elif [ "$STATUS" = "failed" ]; then
    echo "失败原因: $(echo $RESULT | jq -r '.error.message')"
    break
  fi

  sleep 5
done
```

---

## 常见问题

### Q: `first_frame` 和 `reference_image` 有什么区别？

`first_frame` 是指定视频的第一帧画面，视频会从这张图片开始生成动态内容。`reference_image` 是提供视觉参考，模型会参考图片中的风格、人物等元素，但不要求视频画面与图片完全一致。两者不能同时使用。

### Q: 如何保持视频中人脸的一致性？

使用**素材库**功能：先通过 `POST /v1/assets` 上传人像照片，等待处理为 `active` 状态后，在视频生成时使用 `asset://` URL 引用该素材，并设置 `role` 为 `reference_image`。

### Q: `generate_audio` 和 `reference_audio` 如何配合？

- 如果不提供 `reference_audio`，`generate_audio: true` 会让模型自动生成与视频内容匹配的音频
- 如果提供了 `reference_audio`，建议设置 `generate_audio: false`，让生成的视频使用参考音频

### Q: 视频生成通常需要多长时间？

取决于视频时长和分辨率。通常：
- 5 秒 480p：约 30~60 秒
- 5 秒 720p：约 60~90 秒
- 15 秒 720p：约 90~180 秒
- `fast` 模型通常比 `pro` 模型快 30%~50%

### Q: `ratio` 设为 `adaptive` 是什么意思？

当提供了 `first_frame` 图片时，`adaptive` 会自动检测图片比例并使用匹配的输出比例，避免裁剪或变形。

### Q: 素材上传后一直是 `pending` 状态怎么办？

素材处理通常需要 1~3 分钟。如果超过 5 分钟仍为 `pending`，请检查：
1. 上传的图片是否包含清晰的人脸
2. 图片分辨率是否在 300~6000px 范围内
3. 如果持续异常，尝试删除后重新上传

### Q: 可以使用其他用户的素材吗？

不可以。网关会在提交视频生成任务前验证 `asset://` 引用的素材是否属于当前用户（通过 API Key 识别）。使用他人素材 ID 会返回 `"asset not found or access denied"` 错误。
