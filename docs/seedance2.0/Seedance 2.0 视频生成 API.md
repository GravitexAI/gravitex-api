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
  - [接口总览](#接口总览)
  - [创建虚拟素材组（aigc）](#创建素材组)
  - [真人素材组：H5 真人核验（liveness_face）](#真人素材组h5-真人核验)
  - [列出素材组](#列出素材组)
  - [删除素材组](#删除素材组)
  - [创建素材](#创建素材)
  - [列出素材](#列出素材)
  - [查询单个素材](#查询单个素材)
  - [删除素材](#删除素材)
  - [素材状态](#素材状态)
- [参数参考](#参数参考)
- [错误处理](#错误处理)
- [查询审核拦截原因](#查询审核拦截原因)
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

| 模型 ID | 说明 | 支持分辨率 | 特点 |
|---------|------|-----------|------|
| `seedance-2-0` | Seedance 2.0 标准版 | `480p`、`720p`、`1080p` | 高质量生成，适合正式生产 |
| `seedance-2-0-fast` | Seedance 2.0 快速版 | `480p`、`720p` | 更快速度，适合快速迭代，**不支持 1080p** |

---

## 创建视频生成任务

**POST** `https://api.gravitex.ai/v1/video/generations`

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|------|--------|------|
| `model` | string | **是** | — | 模型 ID：`seedance-2-0` 或 `seedance-2-0-fast` |
| `content` | array | 否* | — | 内容数组，包含文本提示、图片、视频、音频（详见下方） |
| `prompt` | string | 否* | — | 文本提示词（`content` 的简化替代，两者二选一） |
| `duration` | integer | 否 | `5` | 视频时长，`-1`（自动）或 `4`~`15` 秒 |
| `resolution` | string | 否 | `"720p"` | 分辨率：`480p`、`720p`（标准版还支持 `1080p`，fast 版不支持 `1080p`） |
| `ratio` | string | 否 | `"16:9"` | 画面比例：`16:9`、`9:16`、`1:1`、`4:3`、`3:4`、`21:9`、`adaptive` |
| `generate_audio` | boolean | 否 | `true` | 是否自动生成音频 |
| `watermark` | boolean | 否 | `false` | 是否添加水印 |
| `seed` | integer | 否 | `-1`（随机） | 随机种子，固定种子可复现结果 |

> \* `content` 和 `prompt` 至少提供一个。推荐使用 `content` 数组，功能更强大。

### Content 数组详解

`content` 是一个数组，每个元素代表一种输入类型：

| type | role | 说明 | 数量限制 |
|------|------|------|----------|
| `text` | — | 文本提示词，描述想要生成的视频内容 | 1 条 |
| `image_url` | （可省略） | **图片输入**，不指定 role 时默认作为首帧图片 | 1 张 |
| `image_url` | `first_frame` | **首帧图片**，视频将以此图片作为第一帧开始生成 | 1 张 |
| `image_url` | `last_frame` | **尾帧图片**，需搭配 `first_frame` 使用 | 1 张 |
| `image_url` | `reference_image` | **参考图片**，提供视觉参考风格 | 最多 9 张 |
| `video_url` | `reference_video` | **参考视频**，提供运动/风格参考 | 最多 3 个 |
| `audio_url` | `reference_audio` | **参考音频**，提供音乐/声音参考 | 最多 3 段 |

**互斥规则：**
- `first_frame` 与 `reference_image` **互斥**，不能同时使用
- `last_frame` 必须搭配 `first_frame`
- `reference_audio` 需搭配图片或视频输入

> ⚠️ **`asset://` 引用必须按 `asset_type` 严格分流到对应字段**
>
> 同一个 `asset_url` 在素材库 `GET /v1/assets` 返回的 `asset_type` 是什么类型，组装到 `content[]` 时就必须用对应的 `type` / 字段名 / `role`，**不能统一塞到 `image_url`**。否则上游会返回：
> ```json
> {"error":{"code":"InvalidParameter","message":"the specified asset is not an image","param":"content[N].image_url.url","type":"BadRequest"}}
> ```
>
> | 素材的 `asset_type` | `type` | 字段 | `role` |
> |---|---|---|---|
> | `Image` | `image_url` | `image_url.url` | `reference_image`（或 `first_frame` / `last_frame`） |
> | `Video` | `video_url` | `video_url.url` | `reference_video` |
> | `Audio` | `audio_url` | `audio_url.url` | `reference_audio` |
>
> ✅ 正确（用图片素材 + 音频素材）：
> ```json
> [
>   {"type":"text","text":"素材1 用素材2唱歌"},
>   {"type":"image_url","image_url":{"url":"asset://asset-xxx-image"},"role":"reference_image"},
>   {"type":"audio_url","audio_url":{"url":"asset://asset-xxx-audio"},"role":"reference_audio"}
> ]
> ```
>
> ❌ 错误（音频素材塞 image_url，触发 InvalidParameter）：
> ```json
> [
>   {"type":"text","text":"素材1 用素材2唱歌"},
>   {"type":"image_url","image_url":{"url":"asset://asset-xxx-image"},"role":"reference_image"},
>   {"type":"image_url","image_url":{"url":"asset://asset-xxx-audio"},"role":"reference_image"}
> ]
> ```

### 响应

```json
{
  "id": "ut-abc123def456",
  "task_id": "ut-abc123def456",
  "object": "video",
  "model": "seedance-2-0",
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
  "model": "seedance-2-0",
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
  "model": "seedance-2-0",
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
  "model": "seedance-2-0",
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
| `failed` | 生成失败，查看 `error.message` 获取原因；如果是 Seedance 2.0 被审核拦截，还可以调用 [查询审核拦截原因](#查询审核拦截原因) 拿到具体分类 |

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
    "model": "seedance-2-0",
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
    "model": "seedance-2-0",
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
    "model": "seedance-2-0",
    "content": [
      {"type": "text", "text": "镜头缓慢推进，花瓣随风飘落"},
      {
        "type": "image_url",
        "image_url": {"url": "https://example.com/garden.jpg"}
      }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "adaptive",
    "generate_audio": true
  }'
```

> **注意**：`image_url` 不指定 `role` 时默认作为首帧图片。使用首帧图片时，`ratio` 参数建议设为 `adaptive`，让模型自动匹配图片比例。

---

### 3. 图生视频（首尾帧驱动）

同时提供首帧和尾帧图片，AI 生成从首帧到尾帧的过渡视频。

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0",
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
    "model": "seedance-2-0",
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
    "model": "seedance-2-0",
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

素材库用于沉淀视频生成所需的多模态参考资源（图片、视频、音频），经上游预处理后即可在视频生成时复用。素材按"素材组（asset group）"组织管理，一个素材组可以容纳多种类型的素材。

**素材库分两类：**

| 库类型 | `group_type` | 创建方式 | 典型用途 |
|---|---|---|---|
| 虚拟素材库（默认） | `aigc` | `POST /v1/asset-groups` 直接创建 | 任意人物 / 动物 / 风景 / 物品的图片、视频、音频 |
| 真人素材库 | `liveness_face` | `POST /v1/visual-validate/session` H5 真人核验后创建 | 真人写真，**首次创建必须通过本人真人核验**；后续追加图/视/音不再校验人脸 |

**完整流程：**

```
1. 创建素材组         → POST /v1/asset-groups   或   /v1/visual-validate/session
2. 创建素材           → POST /v1/assets          （JSON：仅接受公网 https URL）
3. 等待状态变为 active → GET  /v1/assets         （轮询）
4. 在视频生成中引用    → POST /v1/video/generations（使用 asset:// URL）
```

> **请求格式说明**：所有素材库接口均使用 `Content-Type: application/json`，**不再支持 multipart 文件上传**。素材本体通过 `url` 字段提交，**只支持 BytePlus 能直接拉取的公网 https URL**；本地文件请先放到任意对象存储拿到 URL 再调本接口（不支持 Base64 / Data URI 直传）。
>
> ℹ️ **历史变更**：火山方舟 `CreateAsset` 早期版本曾支持 Base64 / Data URI 直传，**自 2026 起已正式下线**（参见火山官方文档 [Create an Asset](https://docs.byteplus.com/en/docs/ModelArk/CreateAsset) 中的 "*For image/video/audio assets, only URL upload is supported. Base64 is not supported.*"）。如果你的老代码还在传 Data URI，请改造为先上传到对象存储再用 URL。

**支持的素材类型与格式（虚拟 / 真人 两类素材库均一致，约束以火山方舟官方为准）：**

| `asset_type` | 支持格式 | 单文件大小 | 其他约束 |
|---|---|---|---|
| `Image`（默认） | jpeg / png / webp / bmp / tiff / gif / heic / heif | < 30 MB | 长宽比 0.4–2.5；宽高 300–6000 px |
| `Video` | mp4 / mov | ≤ 50 MB | 分辨率 480p / 720p；时长 2–15 秒；FPS 24–60；长宽比 0.4–2.5；宽高 300–6000 px；总像素 409600–927408 |
| `Audio` | mp3 / wav | ≤ 15 MB | 时长 2–15 秒 |

> 真人素材库（`liveness_face`）虽可上传图/视/音三类，但首次创建时仍需以**真人正脸图片**完成 H5 真人核验；后续追加视频/音频不会再触发人脸比对。

#### 步骤 1：创建素材组（首次使用时执行一次）

```bash
curl -X POST https://api.gravitex.ai/v1/asset-groups \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "角色A",
    "description": "可选描述"
  }'
```

响应：

```json
{
  "group_id": "group-20260508120000-abcde",
  "name": "角色A",
  "description": "可选描述",
  "channel_id": 123
}
```

> 创建成功后请保存 `group_id`，后续上传素材都需要用到。**每个用户最多可创建 100 个虚拟素材组、100 个真人素材组**（两类配额独立计数）。

#### 步骤 2：创建素材（JSON 方式）

`POST /v1/assets` 的 `url` 字段**只接受可被 BytePlus 公网访问的 HTTP(S) URL**——网关会把这个 URL 原样透传给上游，不做任何解码或转存。

> ⚠️ **不支持 Base64 / Data URI 直传**。如果要使用本地文件，请先把文件上传到任一对象存储（OSS / TOS / S3 等）拿到一个公网可访问的 https URL（带或不带签名都可以，只要 BytePlus 能拉取到即可），再调用本接口。

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-cdn.com/portrait.jpg",
    "group_id": "group-20260508120000-abcde",
    "asset_type": "Image",
    "name": "portrait.jpg"
  }'
```

响应：

```json
{
  "virtual_id": "asset-20260508120145-pqwhc",
  "asset_url": "asset://asset-20260508120145-pqwhc",
  "group_id": "group-20260508120000-abcde",
  "asset_type": "Image",
  "status": "pending"
}
```

| 字段 | 说明 |
|------|------|
| `virtual_id` | 素材唯一 ID |
| `asset_url` | 在视频生成中引用素材时使用的 URL（`asset://` 协议） |
| `asset_type` | 实际入库的素材类型，已规整为 `Image` / `Video` / `Audio` |
| `group_id` | 回显请求里的 `group_id` |
| `status` | 素材状态：`pending` → `active`（成功）或 `failed`（失败） |

#### 步骤 3：等待素材处理完成

```bash
curl "https://api.gravitex.ai/v1/assets?group_id=group-20260508120000-abcde" \
  -H "Authorization: Bearer sk-your_token_key"
```

轮询直到素材 `status` 变为 `active`（通常需要 1~3 分钟）。

#### 步骤 4：使用素材生成面部一致性视频

```bash
curl -X POST https://api.gravitex.ai/v1/video/generations \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0",
    "content": [
      {"type": "text", "text": "一个女孩在海边跳舞，阳光明媚"},
      {
        "type": "image_url",
        "image_url": {"url": "asset://asset-20260508120145-pqwhc"},
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
> - 同一次视频生成中引用的多张素材必须来自同一个素材组（同一个上游空间）

---

## 素材库 API

素材库为每个用户独立管理，用户只能查看和操作自己的素材。所有接口统一使用 `Content-Type: application/json`。

### 接口总览

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/asset-groups` | 创建素材组（虚拟 `aigc`） |
| `GET` | `/v1/asset-groups` | 列出素材组，支持 `?group_type=aigc\|liveness_face\|all`；**未指定时默认仅返回 `aigc` 素材组** |
| `DELETE` | `/v1/asset-groups/{group_id}` | 删除素材组（级联删除组内所有素材） |
| `POST` | `/v1/assets` | 创建素材（**仅接受公网 https URL**；支持 `Image`/`Video`/`Audio`） |
| `GET` | `/v1/assets` | 列出素材，支持 `?group_id=`、`?group_type=aigc\|liveness_face\|all` 过滤；**未指定 `group_type` 时默认仅返回 `aigc` 类素材** |
| `GET` | `/v1/assets/{virtual_id}` | 查询单个素材，自动刷新上游状态 |
| `DELETE` | `/v1/assets/{virtual_id}` | 删除素材 |
| `POST` | `/v1/visual-validate/session` | 真人素材组：发起 H5 真人核验 |
| `POST` | `/v1/visual-validate/result` | 真人素材组：H5 回调内部使用，业务侧无需直接调用 |

---

### 创建素材组

**POST** `https://api.gravitex.ai/v1/asset-groups`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | **是** | 素材组名称 |
| `description` | string | 否 | 描述。**省略或传空串时，网关自动用调用 API Key 所属用户的 `username` 兜底**，便于在火山方舟控制台识别归属 |
| `channel_id` | integer | 否 | 指定上游渠道 ID，省略则自动选择 |
| `group_type` | string | 否 | `aigc`（默认）。**`liveness_face` 不能在此创建**，需走 `POST /v1/visual-validate/session` |

**限额**：同一个用户下，**虚拟素材组（`aigc`）与真人素材组（`liveness_face`）各最多 100 个**，两类配额相互独立、不共用。例如用户已经创建了 100 个虚拟素材组，仍可继续通过 H5 真人核验创建至多 100 个真人素材组。

```bash
curl -X POST https://api.gravitex.ai/v1/asset-groups \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"name": "角色A", "description": "测试角色"}'
```

响应：

```json
{
  "group_id": "group-20260508120000-abcde",
  "name": "角色A",
  "description": "测试角色",
  "channel_id": 123,
  "group_type": "aigc"
}
```

| 字段 | 说明 |
|---|---|
| `group_id` | 后续 `POST /v1/assets`、`DELETE /v1/asset-groups/{group_id}` 都用这个 |
| `description` | 网关回填后的实际入库值（用户传空 → 兜底为 username） |
| `channel_id` | 实际选中的上游渠道（自动选择时返回所选渠道的 ID） |
| `group_type` | 固定为 `aigc`；`liveness_face` 不在本接口创建 |

---

### 真人素材组：H5 真人核验

真人素材库不能直接通过 `POST /v1/asset-groups` 创建——必须先在 BytePlus H5 页面完成真人真人核验。

**两步流程：**

```
1. 客户端发起核验  → POST /v1/visual-validate/session
   ↓ 返回 {h5_link, state}
2. 客户端打开 popup(h5_link)，用户完成核验
   ↓ BytePlus 重定向至 网关 /asset-validate-callback.html?state=…&bytedToken=…&resultCode=10000
3. 回调页自动 fetch /v1/visual-validate/result，落库 group_type=liveness_face
   ↓ window.opener.postMessage({type:'gravitex-asset-validate-result', ok, group_id, …})
4. 客户端拿到 group_id，后续上传素材完全复用 POST /v1/assets（同样支持 Image/Video/Audio）
```

#### POST /v1/visual-validate/session

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | **是** | 素材组名称（用于落库 + 回写到火山方舟控制台） |
| `description` | string | 否 | 描述。同 `POST /v1/asset-groups`，缺省时网关自动用 `username` 兜底 |
| `channel_id` | integer | 否 | 指定上游渠道 ID，省略则自动选择 |

> 网关在拿到火山返回的 `GroupId` 后，会立刻调用 `UpdateAssetGroup` 把上面的 `name` / `description` 回写到火山控制台（火山的 `CreateVisualValidateSession` 接口本身不接受这两个字段）。

```bash
curl -X POST https://api.gravitex.ai/v1/visual-validate/session \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"name": "真人A"}'
```

响应：

```json
{
  "h5_link": "https://verify.byteplus.com/h5/?token=…&lang=zh-CN&lng=zh",
  "state": "<base64url>.<hmac>",
  "channel_id": 123,
  "byted_token": "bp-token-xxxxxxxx",
  "expires_in": 900
}
```

| 字段 | 说明 |
|---|---|
| `h5_link` | BytePlus 真人核验 H5 页面 URL；网关已强制附加 `lang=zh-CN&lng=zh` 以默认显示简体中文 |
| `state` | 网关签发的 HMAC `state` 令牌（HMAC-SHA256），已绑定 `user/channel/group_name/byted_token`，回调页自动用它换取结果 |
| `byted_token` | 火山方舟下发的本次核验唯一凭据；回调页校验时也会用它和 `state` 内的值比对 |
| `expires_in` | **`state` 令牌的有效期（秒）**，固定 `900`（15 分钟）。**注意：这并不是 H5 页面本身的寿命**——BytePlus 的 H5 链接只在约 120 秒内有效，超时后 `byted_token` 会被火山作废，必须重新调用本接口拿新链接 |

> 客户端通常只需把 `h5_link` 在 popup 中打开，并监听 `window.message` 的 `gravitex-asset-validate-result`；`state` 字段已被网关内置回调页自动转发，业务侧无需自行调用 `/v1/visual-validate/result`。

#### 客户端监听 postMessage 的 payload

回调页（`/asset-validate-callback.html`）会向 `window.opener` 投递如下结构的消息（`targetOrigin = '*'`）：

**核验成功：**

```json
{
  "type": "gravitex-asset-validate-result",
  "ok": true,
  "group_id": "group-20260512083014-zyxwv",
  "name": "真人A",
  "channel_id": 123,
  "group_type": "liveness_face"
}
```

**核验失败：**

```json
{
  "type": "gravitex-asset-validate-result",
  "ok": false,
  "result_code": "10003",
  "error": "真人核验未通过：人脸与底图不匹配"
}
```

**最小客户端示例：**

```js
const popup = window.open(session.h5_link, 'asset-validate', 'width=480,height=720');

const listener = (event) => {
  // 回调页与主页若不同源（如 maas:80 ↔ api:3000），origin 会不同；
  // 业务侧建议通过 type 字段而非 event.origin 识别。
  const data = event.data;
  if (!data || data.type !== 'gravitex-asset-validate-result') return;

  window.removeEventListener('message', listener);
  if (data.ok) {
    console.log('素材组创建成功:', data.group_id);
  } else {
    console.error('核验失败:', data.error, data.result_code);
  }
};
window.addEventListener('message', listener);
```

> H5 链接默认 120 秒内有效（BytePlus 上游限制，与网关返回的 `expires_in: 900` 是两个概念）；建议在客户端侧设置一个略大的超时（例如 130s）兜底，超时后关闭 popup + 移除 listener，并在用户重试时调用 `POST /v1/visual-validate/session` 拿新链接。

---

### 列出素材组

**GET** `https://api.gravitex.ai/v1/asset-groups`

**Query 参数：**

| 名称 | 可选值 | 默认 | 说明 |
|---|---|---|---|
| `group_type` | `aigc` / `liveness_face` / `all` | `aigc` | 按素材组类型过滤。**未传时默认只返回虚拟素材组**——要看真人素材组请显式传 `liveness_face` 或 `all` |

```bash
# 默认只返回虚拟素材组
curl https://api.gravitex.ai/v1/asset-groups \
  -H "Authorization: Bearer sk-your_token_key"

# 仅查看真人素材组
curl "https://api.gravitex.ai/v1/asset-groups?group_type=liveness_face" \
  -H "Authorization: Bearer sk-your_token_key"

# 一次拿全部（虚拟 + 真人）
curl "https://api.gravitex.ai/v1/asset-groups?group_type=all" \
  -H "Authorization: Bearer sk-your_token_key"
```

响应：

```json
{
  "groups": [
    {
      "id": 1,
      "user_id": 100,
      "channel_id": 123,
      "group_id": "group-20260508120000-abcde",
      "group_type": "aigc",
      "name": "角色A",
      "description": "测试角色",
      "project_name": "default",
      "space_label": "A",
      "asset_count": 3,
      "created_at": 1715140800,
      "updated_at": 1715140800
    }
  ],
  "total": 1,
  "has_byteplus_channels": true
}
```

| 字段 | 说明 |
|---|---|
| `group_type` | `aigc` 或 `liveness_face` |
| `space_label` | A / B / C…，用于多上游空间场景下区分不同渠道 |
| `asset_count` | 该组当前的素材数量 |
---

### 删除素材组

**DELETE** `https://api.gravitex.ai/v1/asset-groups/{group_id}`

```bash
curl -X DELETE https://api.gravitex.ai/v1/asset-groups/group-20260508120000-abcde \
  -H "Authorization: Bearer sk-your_token_key"
```

> 删除素材组会同时删除该组内的所有素材（上游 + 本地映射）。

---

### 创建素材

**POST** `https://api.gravitex.ai/v1/assets`

`Content-Type: application/json`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `url` | string | **是** | 素材的 **公网 https URL**（BytePlus 必须能直接拉取到）。**不支持 Base64 / Data URI / 本地文件直传**（火山方舟官方已明确下线，详见下方"历史变更"） |
| `group_id` | string | **是** | 素材组 ID（来自 `POST /v1/asset-groups` 或 `/v1/visual-validate/session`） |
| `asset_type` | string | 否 | `Image`（默认）/ `Video` / `Audio`，大小写不敏感 |
| `name` | string | 否 | 素材名称（≤ 64 字符，仅用于素材库 UI 展示和 `ListAssets` 模糊搜索，**不参与模型推理**） |

> 自 2026-05 起，**虚拟（aigc）与真人（liveness_face）两类素材库均支持图片、视频、音频三种 `asset_type`**。网关会根据 URL 后缀做基础格式校验，最终内容审核与转码由 BytePlus 异步完成。

> ℹ️ **关于 `description`：火山方舟 `CreateAsset` 接口本身不支持单素材级别的描述字段**。如需对一组素材打统一标签，请在「素材组」级别使用 `description`（见 [创建素材组](#创建素材组)）。

> ⚠️ **关于本地文件：** 网关只把 `url` 透传给 BytePlus，自身不做 base64 解码或落盘。如果素材在本地，请先上传到任一对象存储（OSS / TOS / S3 等）拿到公网 https URL（带或不带签名都可以，只要 BytePlus 服务端能拉取到），再调用本接口。

> 📜 **历史变更（重要）：** 火山方舟 `CreateAsset` 早期版本的 `URL` 字段曾接受 Base64 / Data URI 直传（部分老文档或老 SDK 也是这么写的）。**自 2026 年起官方已下线该能力**，新文档（[Create an Asset](https://docs.byteplus.com/en/docs/ModelArk/CreateAsset)）明确写："*For image/video/audio assets, only URL upload is supported. Base64 is not supported.*" 因此本网关接口也只接受公网 URL。如老代码传了 Data URI，会被火山以 `InvalidParameter` 类错误拒绝。

**素材格式与大小约束（与火山方舟官方 [`CreateAsset`](https://docs.byteplus.com/en/docs/ModelArk/CreateAsset) 完全一致）：**

#### 图片 (`asset_type: "Image"`)

| 项目 | 限制 |
|---|---|
| 格式 | jpeg / png / webp / bmp / tiff / gif / heic / heif |
| 大小 | < 30 MB / 张 |
| 长宽比（W/H） | 0.4 ~ 2.5 |
| 宽高（像素） | 300 ~ 6000 |

#### 视频 (`asset_type: "Video"`)

| 项目 | 限制 |
|---|---|
| 格式 | mp4 / mov |
| 大小 | ≤ 50 MB / 个 |
| 分辨率 | 480p、720p（不支持 1080p+） |
| 时长 | 2 ~ 15 秒 |
| FPS | 24 ~ 60 |
| 长宽比（W/H） | 0.4 ~ 2.5 |
| 宽高（像素） | 300 ~ 6000 |
| 总像素（W×H） | 409600 ~ 927408（如 640×640=409600、834×1112=927408） |

#### 音频 (`asset_type: "Audio"`)

| 项目 | 限制 |
|---|---|
| 格式 | mp3 / wav |
| 大小 | ≤ 15 MB / 个 |
| 时长 | 2 ~ 15 秒 |

> 网关只对 URL 后缀做基本格式校验，**真正的尺寸 / 时长 / 像素 / 帧率检查全部在 BytePlus 异步预处理阶段完成**。如果素材不达标，最终 `GET /v1/assets` 会得到 `status: "failed"`。

**示例 — 公网 URL（图片）：**

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-cdn.com/portrait.jpg",
    "group_id": "group-20260508120000-abcde",
    "asset_type": "Image",
    "name": "portrait.jpg"
  }'
```

**示例 — 公网 URL（视频）：**

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-cdn.com/dance-clip.mp4",
    "group_id": "group-20260508120000-abcde",
    "asset_type": "Video",
    "name": "dance-clip.mp4"
  }'
```

**示例 — 公网 URL（音频）：**

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://your-cdn.com/voice-sample.mp3",
    "group_id": "group-20260508120000-abcde",
    "asset_type": "Audio",
    "name": "voice-sample.mp3"
  }'
```

响应：

```json
{
  "virtual_id": "asset-20260508120145-pqwhc",
  "asset_url": "asset://asset-20260508120145-pqwhc",
  "group_id": "group-20260508120000-abcde",
  "asset_type": "Image",
  "status": "pending"
}
```

| 字段 | 说明 |
|---|---|
| `virtual_id` | 素材唯一 ID（同时也是 `asset_url` 中 `asset://` 后的部分） |
| `asset_url` | 在 `POST /v1/video/generations` 的 `content` 数组中以 `image_url.url` / `video_url.url` / `audio_url.url` 形式引用的 URL |
| `asset_type` | 实际入库的类型，`Image` / `Video` / `Audio` |
| `status` | `pending`（上游异步处理中）/ `active`（可用）/ `failed`（处理失败） |

---

### 列出素材

**GET** `https://api.gravitex.ai/v1/assets`

返回当前用户所有素材，自动刷新处理中的素材状态。

**Query 参数：**

| 名称 | 可选值 | 默认 | 说明 |
|---|---|---|---|
| `group_id` | 任意素材组 ID | — | 按素材组 ID 过滤；**指定后会忽略 `group_type`**（`group_id` 已经隐含类型） |
| `group_type` | `aigc` / `liveness_face` / `all` | `aigc` | 按素材所属组的类型过滤。**未传时默认只返回虚拟素材组里的素材**——要看真人素材请显式传 `liveness_face` 或 `all` |

```bash
# 默认只返回虚拟素材库下的素材
curl https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key"

# 仅看某个素材组（无论虚拟还是真人）
curl "https://api.gravitex.ai/v1/assets?group_id=group-20260508120000-abcde" \
  -H "Authorization: Bearer sk-your_token_key"

# 仅列出真人素材库的全部素材
curl "https://api.gravitex.ai/v1/assets?group_type=liveness_face" \
  -H "Authorization: Bearer sk-your_token_key"

# 一次拿全部（虚拟 + 真人）
curl "https://api.gravitex.ai/v1/assets?group_type=all" \
  -H "Authorization: Bearer sk-your_token_key"
```

响应：

```json
{
  "assets": [
    {
      "id": 1,
      "user_id": 100,
      "channel_id": 123,
      "group_id": "group-20260508120000-abcde",
      "virtual_id": "asset-20260508120145-pqwhc",
      "asset_url": "asset://asset-20260508120145-pqwhc",
      "url": "https://signed-url.example.com/...",
      "filename": "portrait.jpg",
      "asset_type": "Image",
      "status": "active",
      "space_label": "A",
      "created_at": 1715140800,
      "updated_at": 1715141100
    },
    {
      "id": 2,
      "user_id": 100,
      "channel_id": 123,
      "group_id": "group-20260508120000-abcde",
      "virtual_id": "asset-20260508130022-mp4xy",
      "asset_url": "asset://asset-20260508130022-mp4xy",
      "url": "https://signed-url.example.com/dance.mp4?...",
      "filename": "dance-clip.mp4",
      "asset_type": "Video",
      "status": "active",
      "space_label": "A",
      "created_at": 1715144400,
      "updated_at": 1715144700
    }
  ],
  "total": 2
}
```

> `url` 是上游返回的临时签名 URL（约 12 小时有效），仅用于在素材库 UI 中预览。**视频生成请求中务必使用 `asset_url`（`asset://` 协议），不要使用 `url`。**

---

### 查询单个素材

**GET** `https://api.gravitex.ai/v1/assets/{virtual_id}`

```bash
curl https://api.gravitex.ai/v1/assets/asset-20260508120145-pqwhc \
  -H "Authorization: Bearer sk-your_token_key"
```

---

### 删除素材

**DELETE** `https://api.gravitex.ai/v1/assets/{virtual_id}`

```bash
curl -X DELETE https://api.gravitex.ai/v1/assets/asset-20260508120145-pqwhc \
  -H "Authorization: Bearer sk-your_token_key"
```

响应：

```json
{
  "deleted": true,
  "virtual_id": "asset-20260508120145-pqwhc"
}
```

---

### 素材状态

| 状态 | 说明 |
|------|------|
| `pending` | 素材已提交，上游正在异步处理（包含人脸识别等） |
| `active` | 素材就绪，可用于面部一致性视频生成 |
| `failed` | 素材处理失败，请检查图片质量后重新上传 |

---

## 参数参考

### 视频参数

| 参数 | 可选值 | 说明 |
|------|--------|------|
| `duration` | `-1`, `4` ~ `15` | 输出视频时长（秒），`-1` 为自动时长 |
| `resolution` | `480p`, `720p`, `1080p` | 输出分辨率。⚠️ `seedance-2-0-fast` 仅支持 `480p` 和 `720p`，不支持 `1080p` |
| `ratio` | `16:9`, `9:16`, `1:1`, `4:3`, `3:4`, `21:9`, `adaptive` | 画面比例。`adaptive` 在使用首帧图片时自动匹配图片比例；参考模式下不可用 |
| `generate_audio` | `true`, `false` | 是否自动生成音频 |
| `watermark` | `true`, `false` | 是否添加水印 |
| `seed` | `-1` 或正整数 | 随机种子，`-1` 为随机 |

> 以下三张表是 **`POST /v1/video/generations` 的 `content` 数组里 `image_url` / `video_url` / `audio_url` 直接裸传 URL 时**，火山方舟对资源本身的限制。
>
> 通过 `asset://` 引用素材库素材时**也是这套硬约束**（火山方舟全平台共享）；素材库的「图片」`asset_type` 在格式上更宽容一些，能多收 bmp/tiff/gif/heic/heif 这些后缀，但单文件大小、宽高、像素总数等数值与下方完全一致——详见 [创建素材](#创建素材) 中的"素材格式与大小约束"小节。

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

网关自身校验失败：

```json
{
  "error": {
    "message": "具体错误描述",
    "type": "invalid_request_error"
  }
}
```

**火山方舟上游错误透传**——当请求到达上游再被拒（例如内容审核、参数类型不匹配），网关会原样转发火山的错误结构：

```json
{
  "error": {
    "code": "InvalidParameter",
    "message": "The parameter `content[2].image_url.url` specified in the request is not valid: the specified asset is not an image. Request id: 02177850...",
    "param": "content[2].image_url.url",
    "type": "BadRequest"
  }
}
```

| 字段 | 含义 |
|---|---|
| `code` | 火山方舟错误码（如 `InvalidParameter` / `InternalServiceError` / `RateLimitExceeded`） |
| `param` | 出错字段的点路径定位（如 `content[2].image_url.url`），可直接对照请求 body 排查 |
| `Request id` | 火山请求 ID，反馈火山工单时必带 |

> 常见 `InvalidParameter` 触发点：
> - `asset://` 引用塞错字段（详见 [Content 数组详解](#content-数组详解)）
> - 视频尺寸/时长超出限制
> - 内容安全审核未通过

---

## 查询审核拦截原因

当 Seedance 2.0 任务因为**内容安全 / 版权 / 公众人物 / 真人脸**等审核策略被拦截时，`GET /v1/video/generations/{task_id}` 只会告诉你 `status: failed`，错误信息比较笼统。可以使用本接口主动查询上游具体的拦截分类与子原因。

> ⚠️ **本接口默认关闭**——上游 BytePlus 要求**白名单授权**才能调用。请联系火山方舟商务申请，再让平台管理员在对应渠道的「BytePlus 素材库配置」里勾选「启用审核拦截原因查询」。未开通时调用会返回 `403 channel_not_whitelisted`。

### 限制

| 限制 | 说明 |
|---|---|
| 模型范围 | 仅 `seedance-2-0` / `seedance-2-0-fast` 失败任务可查 |
| 时间窗口 | 任务创建后 **14 天内**可查；超时返回 `400 query_window_expired` |
| 任务状态 | 仅 `failed` 任务有意义；其他状态返回 `400 invalid_task_status` |
| 任务归属 | 只能查询自己 token 创建的任务（按 `user_id + task_id` 限定） |
| 频率限制 | 每个渠道 **10 QPM**（与上游 QPM 一致）；超限返回 `429 rate_limited` |
| 自动缓存 | 首次查询成功后结果会持久化到任务记录里，重复查询不消耗 QPM，响应中带 `"cached": true` |

### 请求

**GET** `https://api.gravitex.ai/v1/video/generations/moderation-result/{task_id}`

```bash
curl https://api.gravitex.ai/v1/video/generations/moderation-result/cgt-20260430135030-xxxxx \
  -H "Authorization: Bearer sk-your_token_key"
```

### 响应 — 拦截原因列表

```json
{
  "task_id": "cgt-20260430135030-xxxxx",
  "block_reasons": [
    {
      "label": "Celebrity",
      "sub_label": "Celebrity",
      "detail": "Potentially contains Public Figures."
    },
    {
      "label": "Copyright",
      "sub_label": "IP",
      "detail": "Potential copyright restriction. Potentially related content: Spider-Man: Homecoming-Peter Parker"
    }
  ],
  "cached": false,
  "queried_at": 1715140800
}
```

### `block_reasons` 字段说明（对齐火山官方枚举）

| 字段 | 说明 |
|---|---|
| `label` | 拦截大类，枚举值：`Safety`（内容安全）/ `Copyright`（版权）/ `Celebrity`（公众人物）/ `Deepfake`（真人脸） |
| `sub_label` | 子分类：`Safety` 大类下为 `Safety`；`Copyright` 下为 `IP` / `Other`；`Celebrity` 下为 `Celebrity`；`Deepfake` 下为 `RealHuman` |
| `detail` | 自然语言描述，可能包含命中的具体 IP / 公众人物名称（如 "Spider-Man: Homecoming-Peter Parker"） |

### 错误响应

| HTTP | `error.type` | 含义 |
|------|---|---|
| `400` | `invalid_request` | 缺少 `task_id` |
| `400` | `invalid_task_status` | 任务不是 `failed` 状态，无拦截原因可查 |
| `400` | `unsupported_model` | 任务不属于 seedance-2-0 系列 |
| `400` | `query_window_expired` | 任务已超出 14 天可查询窗口 |
| `403` | `channel_not_whitelisted` | 渠道未开启审核拦截原因查询开关 |
| `404` | `not_found` | 任务不存在，或上游返回 `NotFound.Id`（任务未被审核拦截 / ID 无效 / 渠道未真正进入白名单） |
| `429` | `rate_limited` | 当前渠道触发 10 QPM 限流，请稍后重试 |
| `502` | `upstream_error` | 上游返回非 404 的错误，详细原因在 `error.message` 中透传 |

错误体格式与其他接口一致：

```json
{
  "error": {
    "message": "moderation query is not enabled on this channel; please contact admin to apply for BytePlus whitelist activation",
    "type": "channel_not_whitelisted"
  }
}
```

### 典型集成流程

```python
import requests

def explain_failed_task(task_id: str) -> str:
    """轮询拿到 status=failed 后再调本接口，把人类可读的拦截原因拼出来"""
    resp = requests.get(
        f"{BASE_URL}/v1/video/generations/moderation-result/{task_id}",
        headers=HEADERS,
    )
    if resp.status_code == 403:
        return "渠道未开通审核拦截原因查询，请联系管理员"
    if resp.status_code == 404:
        return "未查到拦截记录（可能不是被审核拦截，或已超 14 天）"
    resp.raise_for_status()
    data = resp.json()
    if not data.get("block_reasons"):
        return "任务失败但未被审核拦截"
    return "；".join(
        f"{r['label']}/{r['sub_label']}: {r['detail']}" for r in data["block_reasons"]
    )
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
            "model": "seedance-2-0",
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
        },
    ],
    ratio="adaptive",
)

# === 示例 3：面部一致性视频 ===
print("\n--- 面部一致性视频（素材库） ---")


def get_or_create_group(name: str) -> str:
    """获取或创建素材组，返回 group_id"""
    # 先尝试找已有的同名组
    list_resp = requests.get(
        f"{BASE_URL}/v1/asset-groups", headers=HEADERS
    ).json()
    for g in list_resp.get("groups", []):
        if g["name"] == name:
            return g["group_id"]

    # 没有则创建
    create_resp = requests.post(
        f"{BASE_URL}/v1/asset-groups",
        headers=HEADERS,
        json={"name": name, "description": ""},
    ).json()
    return create_resp["group_id"]


def create_asset_by_url(
    group_id: str, asset_url: str, asset_type: str = "Image", name: str = ""
) -> dict:
    """通过公网 https URL 创建素材（仅支持此种方式；本地文件请先上传到对象存储）"""
    resp = requests.post(
        f"{BASE_URL}/v1/assets",
        headers=HEADERS,
        json={
            "url": asset_url,
            "group_id": group_id,
            "asset_type": asset_type,
            "name": name,
        },
    )
    resp.raise_for_status()
    return resp.json()


# 3a. 准备素材组（首次执行时创建，之后复用）
group_id = get_or_create_group("角色A")
print(f"素材组: {group_id}")

# 3b. 创建素材（用你已上传到 OSS / TOS / S3 等对象存储的公网 https URL）
asset = create_asset_by_url(
    group_id,
    "https://your-cdn.com/portrait.jpg",
    asset_type="Image",
    name="portrait.jpg",
)
asset_url = asset["asset_url"]
print(f"素材已创建: {asset_url}, 状态: {asset['status']}")

# 3c. 等待素材就绪
while True:
    assets_resp = requests.get(
        f"{BASE_URL}/v1/assets",
        headers=HEADERS,
        params={"group_id": group_id},
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

# 3d. 使用素材生成视频
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
      model: "seedance-2-0",
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
    "model": "seedance-2-0",
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

使用**素材库**功能：

1. 通过 `POST /v1/asset-groups` 创建一个素材组（仅首次需要，可复用）
2. 通过 `POST /v1/assets` 提交人像照片的**公网 https URL**（不接受 Base64；本地文件请先放到对象存储）
3. 轮询 `GET /v1/assets` 等待 `status` 变为 `active`
4. 在视频生成时使用 `asset://` URL 引用该素材，并设置 `role` 为 `reference_image`

### Q: 素材库支持哪些上传格式？可以直接传 Base64 吗？

**不能直接传 Base64**。`POST /v1/assets` 接口的 `url` 字段**只接受 BytePlus 服务端能直接拉取的公网 https URL**（带或不带签名都可以）。如果素材在本地，请先上传到任意对象存储（OSS / TOS / S3 等）拿到公网 URL 再调用本接口。

通过 `asset_type` 字段声明素材类型，限制以火山方舟官方为准（完整版见 [创建素材 → 素材格式与大小约束](#创建素材)）：

| `asset_type` | 支持后缀 | 单文件大小 | 关键约束 |
|---|---|---|---|
| `Image`（默认） | jpeg / png / webp / bmp / tiff / gif / heic / heif | < 30 MB | 长宽比 0.4–2.5；宽高 300–6000 px |
| `Video` | mp4 / mov | ≤ 50 MB | 480p / 720p；时长 2–15 秒；FPS 24–60；总像素 409600–927408 |
| `Audio` | mp3 / wav | ≤ 15 MB | 时长 2–15 秒 |

> 不再支持 `multipart/form-data` 文件上传。如果你之前用 `-F file=@xxx.jpg` 调用会得到 `No file-upload channel available...` 错误，请改为 JSON 格式提交、`url` 字段填写一个公网可访问的 https URL。

### Q: 真人素材库（liveness_face）能传视频或音频吗？

可以。**首次创建素材组时**仍需通过 `POST /v1/visual-validate/session` 走 H5 真人核验（核验只看脸部图片）；**核验通过后向该组追加素材**，与虚拟素材库完全一致：`POST /v1/assets` 同时支持 `Image` / `Video` / `Audio`，不会再触发人脸比对。

### Q: `generate_audio` 和 `reference_audio` 如何配合？

- 如果不提供 `reference_audio`，`generate_audio: true` 会让模型自动生成与视频内容匹配的音频
- 如果提供了 `reference_audio`，建议设置 `generate_audio: false`，让生成的视频使用参考音频

### Q: 视频生成通常需要多长时间？

取决于视频时长和分辨率。通常：
- 5 秒 480p：约 30~60 秒
- 5 秒 720p：约 60~90 秒
- 5 秒 1080p（仅标准版）：约 90~120 秒
- 15 秒 720p：约 90~180 秒
- `fast` 模型通常比标准版快 30%~50%

### Q: `ratio` 设为 `adaptive` 是什么意思？

当提供了 `first_frame` 图片时，`adaptive` 会自动检测图片比例并使用匹配的输出比例，避免裁剪或变形。

### Q: 素材上传后一直是 `pending` 状态怎么办？

素材处理通常需要 1~3 分钟。如果超过 5 分钟仍为 `pending`，请检查：
1. 上传的图片是否包含清晰的人脸
2. 图片分辨率是否在 300~6000px 范围内
3. 如果持续异常，尝试删除后重新上传

### Q: 可以使用其他用户的素材吗？

不可以。网关会在提交视频生成任务前验证 `asset://` 引用的素材是否属于当前用户（通过 API Key 识别）。使用他人素材 ID 会返回 `"asset not found or access denied"` 错误。

### Q: `seedance-2-0-fast` 和 `seedance-2-0` 有什么区别？

主要区别：
- **分辨率**：`seedance-2-0` 支持 `480p`、`720p`、`1080p`；`seedance-2-0-fast` 仅支持 `480p`、`720p`，**不支持 1080p**
- **速度**：`fast` 版生成速度通常比标准版快 30%~50%
- **质量**：标准版在细节、稳定性上通常优于 `fast` 版
- 其他参数（时长、比例、音频、多模态输入等）完全一致

### Q: `duration` 设为 `-1` 是什么意思？

当 `duration` 设为 `-1` 时，模型会根据输入内容和提示词自动决定合适的视频时长，无需手动指定。
