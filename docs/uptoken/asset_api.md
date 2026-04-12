# Asset Library API (素材库接口文档)

网关代理 UpToken 素材库 API，实现多用户隔离。每个用户只能看到和使用自己上传的素材。

## 认证

所有接口使用 Bearer Token 认证，与视频生成接口一致：

```
Authorization: Bearer sk-{your_token_key}
```

---

## 1. 上传素材

**POST** `/v1/assets`

上传人像图片到素材库，用于面部一致性视频生成。

### 请求

`Content-Type: multipart/form-data`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| file | File | 是 | 图片文件 |

**支持格式**: `image/jpeg`, `image/png`, `image/webp`, `image/gif`, `image/heic`
**大小限制**: 最大 30MB

### 响应

```json
{
  "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
  "asset_url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
  "url": "https://r2.uptoken.cc/...",
  "filename": "portrait.jpg",
  "content_type": "image/jpeg",
  "size_bytes": 1827362,
  "status": "pending",
  "byteplus_status": "",
  "created_at": "2026-04-08T12:00:00Z"
}
```

### cURL 示例

```bash
curl -X POST https://your-gateway/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -F "file=@portrait.jpg"
```

---

## 2. 列出素材

**GET** `/v1/assets`

返回当前用户的所有素材，按创建时间降序排列。对于 `pending`/`ready` 状态的素材，会自动刷新上游状态。

### 响应

```json
{
  "assets": [
    {
      "id": 1,
      "user_id": 100,
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

### cURL 示例

```bash
curl https://your-gateway/v1/assets \
  -H "Authorization: Bearer sk-your_token_key"
```

---

## 3. 查询单个素材

**GET** `/v1/assets/:virtual_id`

获取单个素材的详情，自动从上游刷新最新状态。需要所有权验证。

### 响应

返回上游最新的素材详情（与上传响应格式一致）。

### cURL 示例

```bash
curl https://your-gateway/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer sk-your_token_key"
```

---

## 4. 删除素材

**DELETE** `/v1/assets/:virtual_id`

删除素材（同时从上游和本地删除）。需要所有权验证。

### 响应

```json
{
  "deleted": true,
  "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44"
}
```

### cURL 示例

```bash
curl -X DELETE https://your-gateway/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer sk-your_token_key"
```

---

## 素材状态流转

| 状态 | 说明 |
|------|------|
| `pending` | 素材已上传，正在异步处理中 |
| `ready` | 素材已存储，但尚未获得可信肖像状态 |
| `active` | 素材就绪，可用于面部一致性视频生成 |
| `failed` | 素材处理失败，需重新上传 |

**重要**: 只有 `active` 状态的素材可用于面部一致性视频生成。

---

## 在视频生成中使用素材

上传素材并等待状态变为 `active` 后，在 `POST /v1/video/generations` 的 `content[]` 中引用：

```json
{
  "model": "seedance-2-0-pro",
  "content": [
    { "type": "text", "text": "一个女孩在海边跳舞" },
    {
      "type": "image_url",
      "image_url": { "url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44" },
      "role": "reference_image"
    }
  ],
  "duration": 5,
  "resolution": "720p",
  "ratio": "16:9"
}
```

**所有权验证**: 网关会在提交视频生成任务前验证 `asset://` 引用的素材是否属于当前用户。使用他人素材 ID 将返回错误。

---

## 错误格式

```json
{
  "error": {
    "message": "Asset not found",
    "type": "invalid_request_error"
  }
}
```
