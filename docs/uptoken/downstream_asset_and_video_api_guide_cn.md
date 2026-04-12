# UpToken 下游分销商接入指南（素材提交 + 视频请求）

这份文档面向 UpToken 的下游分销商 / reseller，说明两件事：

1. 素材如何提交
2. 视频生成请求如何提交

文档只描述 **下游应该如何调用 UpToken API**，不涉及 UpToken 与上游平台之间的内部实现。

---

## 1. 基本信息

### Base URL

生产环境：

```text
https://uptoken.cc
```

### 鉴权

所有请求都需要带：

```http
Authorization: Bearer ut-xxxxxxxx
```

### 内容类型

- JSON 接口：`Content-Type: application/json`
- 文件上传接口：`multipart/form-data`

---

## 2. 两种素材路径

对下游客户来说，素材分成两类：

### A. 普通参考素材

适用于：

- 产品图
- 场景图
- 普通参考视频
- 普通参考音频
- 不依赖“人脸一致性 / 角色身份一致性”的素材

这类素材 **不需要先上传到 Asset Library**。  
你可以在生成请求里直接传：

- 公网可访问的 `https://` URL
- 图片 / 音频的 base64 Data URL（推荐优先用公网 URL）

说明：

- 普通参考视频应使用公网 URL
- 如果素材涉及人脸一致性 / 角色身份稳定，不应走这条路径，而应走下面的 Asset Library 路径

### B. 可信肖像素材（Asset Library）

适用于：

- 人脸一致性
- 服装一致性
- 角色身份稳定性

这类素材必须先进入 UpToken 的素材库，拿到 UpToken 的 `virtual_id`，然后在生成请求里使用：

```text
asset://ut-asset-...
```

重要：

- 下游永远使用 **UpToken 的 virtual_id**
- 不要传上游原始 `asset://asset-...`
- 不要在 prompt 文本里直接写 asset id

---

## 3. 提交可信肖像素材

### 3.1 上传素材

**接口**

```http
POST /v1/assets
```

**请求格式**

`multipart/form-data`

**字段**

- `file`：必填，图片文件

**当前支持格式**

- `image/jpeg`
- `image/png`
- `image/webp`
- `image/gif`
- `image/heic`

**大小限制**

- 单张图片最大 `30MB`

### cURL 示例

```bash
curl -X POST https://uptoken.cc/v1/assets \
  -H "Authorization: Bearer ut-YOUR_API_KEY" \
  -F "file=@portrait.jpg"
```

### 成功响应示例

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

### 状态说明

`status` 含义：

- `pending`：素材已接收，仍在异步处理中，暂时不能用于人脸一致性生成
- `active`：素材可用于生成
- `failed`：素材处理失败，不能用于生成
- `ready`：素材已存储，但当前还没有可用的可信肖像状态；如果你要做人脸一致性，请继续等待 `active`

推荐规则：

- **只有 `status=active` 才可用于可信肖像生成**

---

## 4. 查询素材状态

### 4.1 查询素材列表

**接口**

```http
GET /v1/assets
```

返回当前账号名下的素材列表，按创建时间倒序，最多 100 条。

### cURL 示例

```bash
curl https://uptoken.cc/v1/assets \
  -H "Authorization: Bearer ut-YOUR_API_KEY"
```

### 响应示例

```json
{
  "assets": [
    {
      "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
      "asset_url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44",
      "url": "https://r2.uptoken.cc/...",
      "filename": "portrait.jpg",
      "content_type": "image/jpeg",
      "size_bytes": 1827362,
      "byteplus_status": "Active",
      "status": "active",
      "created_at": "2026-04-08T12:00:00Z"
    }
  ],
  "total": 1
}
```

### 4.2 查询单个素材

**接口**

```http
GET /v1/assets/:virtual_id
```

### cURL 示例

```bash
curl https://uptoken.cc/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer ut-YOUR_API_KEY"
```

### 轮询建议

上传成功后，建议每 `5-10` 秒查询一次，直到：

- `status=active`：可以使用
- `status=failed`：停止使用并重新上传

---

## 5. 删除素材

**接口**

```http
DELETE /v1/assets/:virtual_id
```

### cURL 示例

```bash
curl -X DELETE https://uptoken.cc/v1/assets/ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44 \
  -H "Authorization: Bearer ut-YOUR_API_KEY"
```

### 响应示例

```json
{
  "deleted": true,
  "virtual_id": "ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44"
}
```

---

## 6. 提交视频生成请求

**接口**

```http
POST /v1/video/generations
```

你可以使用两种格式：

- 简单平铺格式
- 官方推荐的 `content[]` 格式

推荐优先使用 `content[]` 格式，因为它更适合多模态引用，也更便于后续扩展。

### 6.1 文生视频（最简）

```bash
curl -X POST https://uptoken.cc/v1/video/generations \
  -H "Authorization: Bearer ut-YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "uptoken-2.0-pro",
    "prompt": "A cinematic puppy running through a meadow",
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

### 6.2 使用普通参考素材

普通素材可直接使用公网 URL：

```bash
curl -X POST https://uptoken.cc/v1/video/generations \
  -H "Authorization: Bearer ut-YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "uptoken-2.0-pro",
    "content": [
      {"type": "text", "text": "Use Image 1 as the product reference."},
      {"type": "image_url", "image_url": {"url": "https://your-cdn.com/product.jpg"}, "role": "reference_image"},
      {"type": "video_url", "video_url": {"url": "https://your-cdn.com/demo.mp4"}, "role": "reference_video"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": false
  }'
```

### 6.3 使用可信肖像素材

当素材已经上传并且 `status=active` 后，可在生成请求中这样写：

```bash
curl -X POST https://uptoken.cc/v1/video/generations \
  -H "Authorization: Bearer ut-YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "uptoken-2.0-pro",
    "content": [
      {"type": "text", "text": "Image 1 is the character face. Keep the identity consistent."},
      {"type": "image_url", "image_url": {"url": "asset://ut-asset-7d8c6d3e3b8b4f0db2f6f8d6f29f6c44"}, "role": "reference_image"},
      {"type": "image_url", "image_url": {"url": "https://your-cdn.com/outfit.jpg"}, "role": "reference_image"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

### 6.4 首帧 / 末帧

也可以在 `content[]` 中使用：

- `role: "first_frame"`
- `role: "last_frame"`

例如：

```json
{
  "type": "image_url",
  "image_url": {
    "url": "https://your-cdn.com/frame-1.jpg"
  },
  "role": "first_frame"
}
```

---

## 7. 请求参数说明

### 顶层参数

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `model` | string | 建议填写 | 推荐：`uptoken-2.0-pro` / `uptoken-2.0-fast` |
| `content` | array | 推荐 | 多模态输入，推荐格式 |
| `prompt` | string | 简单模式可用 | 与 `content[]` 互斥；推荐二选一，不要混用 |
| `duration` | integer | 否 | `4-15`，也支持 `-1`（智能时长） |
| `resolution` | string | 否 | `480p` 或 `720p` |
| `ratio` | string | 否 | `adaptive`、`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9` |
| `generate_audio` | boolean | 否 | 是否生成同步音频 |
| `watermark` | boolean | 否 | 是否带水印 |

### `content[]` 支持项

| type | 字段 | 说明 |
|------|------|------|
| `text` | `text` | 文本提示词 |
| `image_url` | `image_url.url` | 图片 URL 或 `asset://ut-asset-...` |
| `video_url` | `video_url.url` | 视频 URL，建议带 `role: "reference_video"` |
| `audio_url` | `audio_url.url` | 音频 URL 或 data URL，建议带 `role: "reference_audio"` |

### `image_url.role` 支持值

- `reference_image`
- `first_frame`
- `last_frame`

### 其他 role

- `video_url.role`：`reference_video`
- `audio_url.role`：`reference_audio`

### 注意事项

1. `content[]` 和平铺字段不能混用，否则返回 `error-211`
2. Prompt 中引用素材时请写 `Image 1`、`Video 1`、`Audio 1`
3. 不要在 prompt 文本里直接写 asset id
4. 只有 `status=active` 的可信肖像素材才可用于稳定人脸一致性
5. 如果是人脸 / 角色身份稳定场景，不要只传普通公网 URL，必须先上传 Asset Library

---

## 8. 视频任务响应

### 提交成功响应

```json
{
  "id": "ut-a8f3K9mN2pQx"
}
```

保存这个 `id`，用于后续查询。

---

## 9. 查询视频任务结果

**接口**

```http
GET /v1/video/generations/:task_id
```

### cURL 示例

```bash
curl https://uptoken.cc/v1/video/generations/ut-a8f3K9mN2pQx \
  -H "Authorization: Bearer ut-YOUR_API_KEY"
```

### 状态流转

```text
queued -> running -> succeeded / failed
```

### 成功响应示例

```json
{
  "id": "ut-a8f3K9mN2pQx",
  "status": "succeeded",
  "progress": 100,
  "duration": 5,
  "content": {
    "video_url": "https://uptoken.cc/v1/media/..."
  },
  "usage": {
    "total_tokens": 97605
  }
}
```

### 失败响应示例

```json
{
  "id": "ut-a8f3K9mN2pQx",
  "status": "failed",
  "progress": 100,
  "error": {
    "code": "error-303",
    "message": "Generated video rejected by content filter. Try adjusting your prompt."
  }
}
```

### 轮询建议

- 优先使用响应头 `Retry-After`
- 如果未读取 `Retry-After`，建议每 `10` 秒轮询一次
- 不建议快于 `5` 秒

---

## 10. 最佳实践

### 推荐调用顺序

#### 场景 A：普通素材

1. 准备公网可访问素材 URL
2. 直接调用 `POST /v1/video/generations`
3. 用 `GET /v1/video/generations/:id` 轮询结果

#### 场景 B：人脸一致性 / 角色一致性

1. 上传图片到 `POST /v1/assets`
2. 轮询 `GET /v1/assets/:id`
3. 等到 `status=active`
4. 在生成请求里引用 `asset://ut-asset-...`
5. 轮询视频任务结果

### 强烈建议

- 普通素材和可信肖像素材分开管理
- 仅在人脸 / 服装一致性场景使用 Asset Library
- 你自己的下游系统不要暴露内部实现细节给终端客户
- 如果你也是二次分销商，建议在你自己的网关层再包一层你自己的素材 ID

---

## 11. 账户与素材归属

UpToken 里的素材所有权是 **用户级**，不是 key 级。

这意味着：

- 同一用户下的多个 active key，共享同一素材池
- 更换 key，不会丢素材
- prod key / staging key 也可能共享素材

如果你自己再向下游客户二次分销，建议你也采用同样策略：

- 素材归属绑定到你的 `tenant / customer`
- 不要绑定到你发出去的某一把 key

---

## 12. 常见错误

| code | 含义 | 说明 |
|------|------|------|
| `error-101` | 缺少 API Key | 检查 `Authorization` |
| `error-211` | 混用 content 与平铺字段 | 只能选一种格式 |
| `error-401` | 未上传文件或文件过大 | 上传素材时检查 multipart |
| `error-402` | 文件类型不支持 | 目前素材库仅支持图片 |
| `error-403` | Asset 不可访问或未就绪 | 常见于 `asset://...` 引用错误或状态未 active |
| `error-404` | Asset 不存在 | 检查 virtual_id 是否属于当前账号 |
| `error-701` | 任务不存在 | 检查 task_id |

---

## 13. 联系方式

如果你需要：

- BytePlus 官方兼容路由
- Webhook / SSE 文档
- 多租户 reseller 封装建议
- 终端用户版素材库说明

请联系 UpToken 团队获取对应文档。
