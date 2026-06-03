# Seedance 2.0 NSFW — API 使用文档

> Base URL：`https://api.gravitex.ai`  
> 鉴权方式：`Authorization: Bearer <YOUR_API_KEY>`  
> 内容类型：`Content-Type: application/json`

---

## 概述

Seedance 2.0 NSFW 系列模型在标准 Seedance 2.0 的基础上关闭了内容预审核限制，支持生成成人向视频内容。使用该系列模型时，上传到素材库的图片/视频素材同样需要跳过审核，方可正常用于视频生成。

完整使用流程如下：

```
1. 创建素材组（一次性）
        ↓
2. 上传素材到素材组（跳过审核）
        ↓
3. 轮询素材状态，等待变为 active
        ↓
4. 发起视频生成任务（引用 asset:// URI）
        ↓
5. 轮询任务状态，获取视频结果
```

---

## 一、创建素材组

每个素材必须归属于一个素材组。素材组只需创建一次，后续复用。

**POST `/v1/asset-groups`**

```http
POST /v1/asset-groups
Authorization: Bearer <YOUR_API_KEY>
Content-Type: application/json

{
  "name": "my-nsfw-group",
  "description": "NSFW 素材组"
}
```

**响应**

```json
{
  "group_id": "group-20260603120000-xxxxx",
  "name": "my-nsfw-group",
  "description": "NSFW 素材组"
}
```

> `group_id` 后续上传素材和生成视频时都会用到，请妥善保存。

---

## 二、上传素材（跳过审核）

**POST `/v1/assets`**

使用 NSFW 模型时，必须传入 `"moderation": { "strategy": "Skip" }` 以跳过内容预审核，否则审核会拦截成人内容素材。

```http
POST /v1/assets
Authorization: Bearer <YOUR_API_KEY>
Content-Type: application/json

{
  "url": "https://your-cdn.com/portrait.jpg",
  "group_id": "group-20260603120000-xxxxx",
  "asset_type": "Image",
  "name": "portrait-01",
  "moderation": {
    "strategy": "skip"
  }
}
```

**参数说明**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | 是 | 素材的公网可访问 URL，需为海外可访问地址 |
| `group_id` | string | 是 | 素材组 ID，来自第一步 |
| `asset_type` | string | 是 | `Image` / `Video` / `Audio` |
| `name` | string | 否 | 素材名称，用于管理检索，不影响生成 |
| `moderation.strategy` | string | 否 | `skip` = 跳过内容预审核（大小写不敏感，`skip` / `SKIP` 均可）；不传则走默认审核 |

**素材格式要求**

| 类型 | 格式 | 尺寸/时长限制 | 大小 |
|------|------|------------|------|
| 图片 | jpg / png / webp / gif / bmp / tiff / heic | 宽高 300–6000px，宽高比 0.4–2.5 | ≤ 30 MB |
| 视频 | mp4 / mov | 480p / 720p，2–15 秒，FPS 24–60 | ≤ 50 MB |
| 音频 | mp3 / wav | 2–15 秒 | ≤ 15 MB |

**响应**

```json
{
  "virtual_id": "asset-20260603120001-xxxxx",
  "asset_url": "asset://asset-20260603120001-xxxxx",
  "group_id": "group-20260603120000-xxxxx",
  "asset_type": "Image",
  "status": "pending"
}
```

> 上传接口为**异步**，`status: pending` 表示正在处理，需要轮询等待。

---

## 三、轮询素材状态

**GET `/v1/assets/{virtual_id}`**

```http
GET /v1/assets/asset-20260603120001-xxxxx
Authorization: Bearer <YOUR_API_KEY>
```

**响应**

```json
{
  "virtual_id": "asset-20260603120001-xxxxx",
  "asset_url": "asset://asset-20260603120001-xxxxx",
  "status": "active",
  "asset_type": "Image",
  "group_id": "group-20260603120000-xxxxx",
  "filename": "portrait-01",
  "skip_moderation": true,
  "created_at": 1748923261
}
```

**`status` 状态说明**

| 状态 | 含义 | 处理建议 |
|------|------|---------|
| `pending` | 火山侧正在处理 | 等待后重试 |
| `active` | 处理成功，可用于生成 | 可继续下一步 |
| `failed` | 处理失败 | 检查素材格式/尺寸后重新上传 |

**推荐轮询策略**：每 10 秒查一次，总计等待不超过 5 分钟。

```python
import time, requests

headers = {"Authorization": "Bearer <YOUR_API_KEY>"}
virtual_id = "asset-20260603120001-xxxxx"

for _ in range(30):
    resp = requests.get(
        f"https://api.gravitex.ai/v1/assets/{virtual_id}",
        headers=headers
    ).json()
    if resp["status"] == "active":
        print("素材就绪，asset_url:", resp["asset_url"])
        break
    if resp["status"] == "failed":
        raise Exception("素材处理失败")
    time.sleep(10)
```

---

## 四、发起视频生成任务

素材状态变为 `active` 后，使用 `asset://` URI 在视频生成请求中引用。

**POST `/v1/video/generations`**（或你的视频生成端点）

```http
POST /v1/video/generations
Authorization: Bearer <YOUR_API_KEY>
Content-Type: application/json

{
  "model": "seedance-2-0-xxxx-NSFW",
  "content": [
    {
      "type": "text",
      "text": "Image 1 的角色走进房间，镜头缓慢推进，电影感灯光"
    },
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": {
        "url": "asset://asset-20260603120001-xxxxx"
      }
    }
  ],
  "ratio": "16:9",
  "duration": 5
}
```

> 提示词中引用素材使用 `Image 1`、`Video 1`、`Audio 1` 等格式（类型 + 序号），**不要**直接写 asset ID。

**响应**

```json
{
  "id": "task-20260603120010-xxxxx",
  "status": "pending"
}
```

---

## 五、轮询视频生成结果

**GET `/v1/video/generations/{task_id}`**

```http
GET /v1/video/generations/task-20260603120010-xxxxx
Authorization: Bearer <YOUR_API_KEY>
```

**响应（成功）**

```json
{
  "id": "task-20260603120010-xxxxx",
  "status": "succeeded",
  "video_url": "https://cdn.gravitex.ai/xxx.mp4"
}
```

---

## 六、查看素材列表

**GET `/v1/assets`**

```http
GET /v1/assets?group_id=group-20260603120000-xxxxx
Authorization: Bearer <YOUR_API_KEY>
```

**响应**

```json
{
  "assets": [
    {
      "virtual_id": "asset-20260603120001-xxxxx",
      "asset_url": "asset://asset-20260603120001-xxxxx",
      "filename": "portrait-01",
      "asset_type": "Image",
      "status": "active",
      "skip_moderation": true,
      "gravitex_url": "https://cdn.gravitex.ai/xxx.jpg",
      "created_at": 1748923261
    }
  ],
  "total": 1
}
```

---

## 七、删除素材 / 素材组

**删除单个素材**

```http
DELETE /v1/assets/{virtual_id}
Authorization: Bearer <YOUR_API_KEY>
```

**删除素材组（连同组内所有素材）**

```http
DELETE /v1/asset-groups/{group_id}
Authorization: Bearer <YOUR_API_KEY>
```

---

## 八、完整 Python 示例

```python
import time
import requests

BASE_URL = "https://api.gravitex.ai"
API_KEY = "<YOUR_API_KEY>"
HEADERS = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
}

# 1. 创建素材组
group_resp = requests.post(
    f"{BASE_URL}/v1/asset-groups",
    json={"name": "nsfw-group-01", "description": "NSFW 素材"},
    headers=HEADERS,
).json()
group_id = group_resp["group_id"]
print("素材组:", group_id)

# 2. 上传素材（跳过审核）
asset_resp = requests.post(
    f"{BASE_URL}/v1/assets",
    json={
        "url": "https://your-cdn.com/portrait.jpg",
        "group_id": group_id,
        "asset_type": "Image",
        "name": "portrait-01",
        "moderation": {"strategy": "Skip"},
    },
    headers=HEADERS,
).json()
virtual_id = asset_resp["virtual_id"]
print("素材已提交:", virtual_id)

# 3. 等待素材就绪
for _ in range(30):
    time.sleep(10)
    status_resp = requests.get(
        f"{BASE_URL}/v1/assets/{virtual_id}",
        headers=HEADERS,
    ).json()
    print("素材状态:", status_resp["status"])
    if status_resp["status"] == "active":
        asset_url = status_resp["asset_url"]
        print("素材就绪:", asset_url)
        break
    if status_resp["status"] == "failed":
        raise Exception("素材处理失败，请检查格式和尺寸")
else:
    raise Exception("素材等待超时")

# 4. 发起视频生成
task_resp = requests.post(
    f"{BASE_URL}/v1/video/generations",
    json={
        "model": "seedance-2-0-xxxx-NSFW",
        "content": [
            {"type": "text", "text": "Image 1 的角色走进房间，柔和灯光"},
            {
                "type": "image_url",
                "role": "reference_image",
                "image_url": {"url": asset_url},
            },
        ],
        "ratio": "16:9",
        "duration": 5,
    },
    headers=HEADERS,
).json()
task_id = task_resp["id"]
print("任务已提交:", task_id)

# 5. 等待视频生成完成
for _ in range(60):
    time.sleep(15)
    result = requests.get(
        f"{BASE_URL}/v1/video/generations/{task_id}",
        headers=HEADERS,
    ).json()
    print("任务状态:", result["status"])
    if result["status"] == "succeeded":
        print("视频地址:", result["video_url"])
        break
    if result["status"] == "failed":
        raise Exception("视频生成失败")
else:
    raise Exception("视频生成超时")
```

---

## 九、注意事项

1. **素材 URL 必须海外可访问**：火山素材库服务位于海外节点（ap-southeast-1），国内存储域名（如 `tos-cn-*.volces.com`）会导致拉取极慢（30s+），请确保素材托管在海外 CDN 或海外 S3。

2. **跳过审核为永久设置**：一旦账号开启 `Moderation.Skip`，火山控制台将无法再通过 Web 界面查看素材，所有素材管理只能通过 API 进行。

3. **异步处理**：`CreateAsset` 接口为异步，提交后需轮询 `status` 直到变为 `active`，处理时间通常在 10–60 秒，视频素材可能更长。

4. **同一任务只能使用同一素材组的素材**：引用多个素材时，所有 `asset://` URI 必须属于同一个 group_id 下的素材。

5. **API Key 权限**：使用前请确认你的 API Key 已开通 Seedance 2.0 NSFW 模型权限。

---

## 十、错误码

| HTTP 状态 | 错误类型 | 常见原因 |
|-----------|---------|---------|
| 400 | `invalid_request_error` | 参数缺失或格式错误（如 URL 不合法、asset_type 不支持） |
| 401 | 未鉴权 | API Key 无效或未传 Authorization 头 |
| 404 | 资源不存在 | virtual_id / group_id 不存在或不属于当前用户 |
| 502 | 上游错误 | 火山接口异常，可重试；若持续失败请联系支持 |

---

> 如有问题，请联系技术支持并提供 `virtual_id` 或 `task_id`，可快速定位日志。
