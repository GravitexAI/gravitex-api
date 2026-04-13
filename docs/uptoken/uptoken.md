# UpToken 视频生成 API

## 快速开始

1. 前往 https://uptoken.cc/keys 创建 API Key
2. 提交任务: `POST https://uptoken.cc/v1/video/generations`
3. 轮询结果: `GET https://uptoken.cc/v1/video/generations/:id`

## 认证

```
Authorization: Bearer ut-YOUR_API_KEY
```

## 模型

| 模型 ID | 说明 |
|---------|------|
| uptoken-2.0-pro | 视频生成 — 专业质量，最长15秒 |
| uptoken-2.0-fast | 视频生成 — 快速，最长15秒 |

## 创建任务

```bash
curl -X POST https://uptoken.cc/v1/video/generations \
  -H "Authorization: Bearer ut-YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "uptoken-2.0-pro",
    "content": [
      {"type": "text", "text": "你的提示词"},
      {"type": "image_url", "image_url": {"url": "https://..."}, "role": "reference_image"},
      {"type": "video_url", "video_url": {"url": "https://..."}, "role": "reference_video"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

**返回:** `{"id": "ut-123"}`

## 轮询状态

```bash
curl https://uptoken.cc/v1/video/generations/ut-123 \
  -H "Authorization: Bearer ut-YOUR_API_KEY"
```

**返回（成功时）:**
```json
{
  "id": "ut-123",
  "status": "succeeded",
  "content": {"video_url": "https://uptoken.cc/v1/media/proxy?..."},
  "usage": {"total_tokens": 97605}
}
```

**状态流转:** queued → running → succeeded / failed

## 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| model | string | 是 | uptoken-2.0-pro 或 uptoken-2.0-fast |
| content | array | 否 | 内容数组（text/image/video/audio） |
| prompt | string | 否 | 文本提示词（content 的替代方式） |
| duration | integer | 否 | 4-15 秒（默认 5） |
| resolution | string | 否 | 480p 或 720p（默认 720p） |
| ratio | string | 否 | 16:9、9:16、1:1、4:3、3:4、21:9、adaptive |
| generate_audio | boolean | 否 | 生成音频（默认 true） |
| seed | integer | 否 | 随机种子（-1 为随机） |
| image_urls | string[] | 否 | 参考图片（最多 9 张） |
| video_urls | string[] | 否 | 参考视频（最多 3 个，总时长≤15s，仅 URL） |
| audio_urls | string[] | 否 | 参考音频（最多 3 段，需搭配图片或视频） |
| first_frame_url | string | 否 | 首帧图片（与 image_urls 互斥） |
| last_frame_url | string | 否 | 尾帧图片（需搭配 first_frame_url） |

## Content 数组 Role

| type | role | 说明 |
|------|------|------|
| text | — | 文本提示词 |
| image_url | reference_image | 参考图片（最多 9 张） |
| image_url | first_frame | 首帧图片（仅 1 张，与 reference_image 互斥） |
| image_url | last_frame | 尾帧图片（需搭配 first_frame） |
| video_url | reference_video | 参考视频（最多 3 个，≤15s，仅 URL） |
| audio_url | reference_audio | 参考音频（最多 3 段，需搭配图片或视频） |

## 场景（三种互斥）

| 场景 | 输入 |
|------|------|
| 文生视频 | 仅文本提示词 |
| 图生视频（首帧） | first_frame + 文本（可选） |
| 图生视频（首尾帧） | first_frame + last_frame + 文本（可选） |
| 多模态参考 | 图片(0-9) + 视频(0-3) + 音频(0-3) + 文本（可选） |

## Python 示例

```python
import requests, time

BASE = "https://uptoken.cc"
KEY = "ut-YOUR_API_KEY"
HEADERS = {"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"}

resp = requests.post(f"{BASE}/v1/video/generations", headers=HEADERS, json={
    "model": "uptoken-2.0-pro",
    "content": [{"type": "text", "text": "黄金时刻无人机航拍山脉"}],
    "duration": 5, "resolution": "720p", "ratio": "16:9", "generate_audio": True,
})
task_id = resp.json()["id"]

while True:
    result = requests.get(f"{BASE}/v1/video/generations/{task_id}", headers=HEADERS).json()
    if result["status"] == "succeeded":
        print(result["content"]["video_url"])
        break
    elif result["status"] == "failed":
        print(result["error"]["message"])
        break
    time.sleep(5)
```

## 错误处理

| 状态码 | 含义 | 处理方式 |
|--------|------|---------|
| 200 | 成功 | — |
| 400 | 请求参数错误 | 检查参数 |
| 401 | 未授权 | 检查 API key |
| 402 | 余额不足 | 充值 |
| 429 | 请求频繁 | 等待后重试 |
| 502 | 上游错误 | 重试 |

## 速率限制

- 60 次/分钟（每个 API key）
- 1,000 次/小时
- 10,000 次/天

## 文件限制

| 类型 | 数量上限 | 限制 |
|------|---------|------|
| 图片 | 9 | jpeg/png/webp，≤30MB，300-6000px |
| 视频 | 3 | mp4/mov，≤50MB，2-15s/个，总时长≤15s |
| 音频 | 3 | wav/mp3，≤15MB，2-15s/段，需搭配图片或视频 |
| 输出 | — | 4-15 秒，480p 或 720p |
