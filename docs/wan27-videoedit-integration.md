# Wan 2.7 视频编辑 API 接入文档

本文档说明通过 Gravitex 网关调用阿里云百炼 `wan2.7-videoedit` 模型的方式。

该模型用于对已有视频进行指令式编辑，例如替换人物、修改服装、调整场景风格等。接口为异步接口，需要先提交任务，再轮询任务结果。

## 1. 接口地址

### 创建视频编辑任务

```http
POST https://api.gravitex.ai/v1/video/generations
```

### 查询任务状态

```http
GET https://api.gravitex.ai/v1/video/generations/{task_id}
```

其中 `{task_id}` 为创建任务接口返回的 `id`。

## 2. 公共请求头

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| `Authorization` | 是 | Gravitex 网关 API Key，格式为 `Bearer sk-xxx` |
| `Content-Type` | 是 | 固定为 `application/json` |
| `X-Trace-ID` | 否 | 请求跟踪 ID，建议每次请求生成唯一值 |

示例：

```http
Authorization: Bearer sk-xxxxxxxx
Content-Type: application/json
X-Trace-ID: 76dc2556f4a747e29c6b7cf17eb29247
```

## 3. 创建任务

### 3.1 最小请求

```bash
curl --location 'https://api.gravitex.ai/v1/video/generations' \
  --header 'Authorization: Bearer sk-xxxxxxxx' \
  --header 'Content-Type: application/json' \
  --header 'X-Trace-ID: wan27-videoedit-test-001' \
  --data '{
    "model": "wan2.7-videoedit",
    "prompt": "背景不变，把视频中的小羊换成黑色的",
    "duration": 4,
    "metadata": {
      "input": {
        "prompt": "背景不变，把视频中的小羊换成黑色的",
        "media": [
          {
            "type": "video",
            "url": "https://your-public-oss.example.com/input.mp4"
          }
        ]
      },
      "parameters": {
        "resolution": "720P",
        "prompt_extend": true,
        "watermark": false
      }
    }
  }'
```

### 3.2 请求体字段

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model` | string | 是 | 固定为 `wan2.7-videoedit` |
| `prompt` | string | 是 | 视频编辑指令 |
| `duration` | integer | 否 | 输出视频时长，示例为 4 秒 |
| `metadata.input.prompt` | string | 是 | 传给阿里云的原始提示词，建议与顶层 `prompt` 保持一致 |
| `metadata.input.media` | array | 是 | 输入媒体数组，至少包含一个视频 |
| `metadata.input.media[].type` | string | 是 | 原视频使用 `video` |
| `metadata.input.media[].url` | string | 是 | 视频公网 URL 或可访问的临时 URL |
| `metadata.parameters.resolution` | string | 否 | `720P` 或 `1080P` |
| `metadata.parameters.prompt_extend` | boolean | 否 | 是否启用提示词智能改写 |
| `metadata.parameters.watermark` | boolean | 否 | 是否添加水印 |

## 4. 添加参考图

可以在同一个 `media` 数组中添加参考图：

```json
{
  "model": "wan2.7-videoedit",
  "prompt": "保持视频中的人物动作不变，把人物衣服替换成参考图中的黑色西装",
  "duration": 4,
  "metadata": {
    "input": {
      "prompt": "保持视频中的人物动作不变，把人物衣服替换成参考图中的黑色西装",
      "media": [
        {
          "type": "video",
          "url": "https://your-public-oss.example.com/input.mp4"
        },
        {
          "type": "reference_image",
          "url": "https://your-public-oss.example.com/reference.png"
        }
      ]
    },
    "parameters": {
      "resolution": "720P",
      "prompt_extend": true,
      "watermark": false
    }
  }
}
```

参考图在提示词中可以用“图 1”指代。Wan 2.7 视频编辑最多支持 4 张参考图。

## 5. 创建任务响应

创建接口成功后，Gravitex 返回标准化的视频任务对象，不带 `code/message/data` 外层：

```json
{
  "id": "76dc2556-f4****************29247",
  "object": "video",
  "model": "wan2.7-videoedit",
  "status": "queued",
  "progress": 0,
  "created_at": 1784714517,
  "metadata": {
    "output": {
      "task_id": "76dc2556-f4****************29247",
      "task_status": "PENDING"
    },
    "request_id": "86c168af-****************bf96d"
  }
}
```

客户端需要保存：

```text
id = 76dc2556-f4****************29247
```

## 6. 查询任务

```bash
curl --location \
  'https://api.gravitex.ai/v1/video/generations/76dc2556-f4****************29247' \
  --header 'Authorization: Bearer sk-xxxxxxxx' \
  --header 'X-Trace-ID: wan27-videoedit-query-001'
```

查询接口带有 `code/message/data` 外层：

```json
{
  "code": "success",
  "message": "",
  "data": {
    "id": "76dc2556-f4****************29247",
    "object": "video",
    "model": "wan2.7-videoedit",
    "status": "completed",
    "progress": 100,
    "created_at": 1784714517,
    "completed_at": 1784714687,
    "seconds": "8",
    "url": "https://signed-result-url.example.com/result.mp4",
    "video_url": "https://signed-result-url.example.com/result.mp4",
    "metadata": {
      "output": {
        "task_status": "SUCCEEDED",
        "input_video_duration": 4,
        "output_video_duration": 4,
        "video_url": "https://signed-result-url.example.com/result.mp4"
      }
    }
  }
}
```

## 7. 状态处理

客户端应以 `data.status` 作为平台标准状态：

| `data.status` | 说明 | 客户端处理 |
| --- | --- | --- |
| `queued` | 排队中 | 继续轮询 |
| `in_progress` | 生成中 | 继续轮询 |
| `completed` | 生成成功 | 读取 `data.video_url` 或 `data.url` |
| `failed` | 生成失败 | 读取 `data.error.message` |

上游状态位于 `data.metadata.output.task_status`，例如 `PENDING`、`SUCCEEDED`。该字段用于排查，不建议作为客户端主状态判断依据。

建议轮询间隔为 5～15 秒，不要高频轮询。任务查询有效期以网关和上游任务保留策略为准。

## 8. 输出视频 URL

成功后优先读取：

```text
data.video_url
```

也可以读取：

```text
data.url
```

两者在当前 Gravitex 适配器中通常相同。

输出 URL 是带签名的临时 URL，不应永久保存为业务资源地址。如果需要长期保存，应在 URL 有效期内下载或转存到自己的 OSS。

## 9. 时长和计费说明

本次实测请求中：

```json
{
  "duration": 4
}
```

上游返回：

```json
{
  "usage": {
    "duration": 8,
    "input_video_duration": 4,
    "output_video_duration": 4
  }
}
```

字段含义如下：

| 字段 | 含义 |
| --- | --- |
| `input_video_duration` | 输入视频时长，4 秒 |
| `output_video_duration` | 输出视频时长，4 秒 |
| `usage.duration` | 输入视频 + 输出视频的总计费时长，8 秒 |
| `data.seconds` | 当前网关响应中对应上游总时长的字段，示例为 `8` |

因此，`data.seconds = 8` 不代表输出视频生成了 8 秒，而是本次 Wan 视频编辑的输入和输出合计计费时长。

阿里云官方计费说明为：视频编辑按“输入视频时长 + 输出视频时长”计费；输入图像不计费。[官方计费说明](https://www.alibabacloud.com/help/en/model-studio/wan-video-editing-guide)

## 10. 常见错误排查

### 10.1 `media` 缺少视频

必须至少存在：

```json
{
  "type": "video",
  "url": "https://.../input.mp4"
}
```

### 10.2 视频 URL 无法访问

阿里云无法访问以下地址：

- 需要登录才能访问的页面 URL
- OSS 权限不足或已过期的 URL

应使用公网可访问的 HTTP/HTTPS URL 或符合上游要求的临时 URL。

### 10.3 请求体不是合法 JSON

以下写法错误：

```json
"prompt_extend": **true**
```

正确写法：

```json
"prompt_extend": true
```

### 10.4 只传了 `video_url`

推荐使用新格式：

```json
"input": {
  "media": [
    { "type": "video", "url": "..." }
  ]
}
```

Gravitex 后端保留了旧客户端 `video_url` 的兼容转换，但新接入方应直接使用 `input.media`。

### 10.5 把 Wan 2.1 和 Wan 2.7 参数混用

`wan2.7-videoedit` 使用：

```text
input.media
```

旧版 `wan2.1-vace-plus` 视频重绘使用：

```text
input.function = video_repainting
input.video_url
```

两套参数不能混用。

## 11. Python 最小调用示例

```python
import time
import uuid
import requests

BASE_URL = "https://api.gravitex.ai"
API_KEY = "sk-xxxxxxxx"

headers = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
    "X-Trace-ID": uuid.uuid4().hex,
}

payload = {
    "model": "wan2.7-videoedit",
    "prompt": "背景不变，把视频中的小羊换成黑色的",
    "duration": 4,
    "metadata": {
        "input": {
            "prompt": "背景不变，把视频中的小羊换成黑色的",
            "media": [
                {
                    "type": "video",
                    "url": "https://your-public-oss.example.com/input.mp4",
                }
            ],
        },
        "parameters": {
            "resolution": "720P",
            "prompt_extend": True,
            "watermark": False,
        },
    },
}

response = requests.post(
    f"{BASE_URL}/v1/video/generations",
    headers=headers,
    json=payload,
    timeout=30,
)
response.raise_for_status()
task = response.json()
task_id = task["id"]

while True:
    result = requests.get(
        f"{BASE_URL}/v1/video/generations/{task_id}",
        headers={
            "Authorization": f"Bearer {API_KEY}",
            "X-Trace-ID": uuid.uuid4().hex,
        },
        timeout=30,
    )
    result.raise_for_status()
    data = result.json()["data"]
    status = data["status"]
    print(status, data.get("progress", 0))

    if status == "completed":
        print("video_url:", data.get("video_url") or data.get("url"))
        break
    if status == "failed":
        raise RuntimeError(data.get("error", {}).get("message", "video generation failed"))

    time.sleep(10)
```
