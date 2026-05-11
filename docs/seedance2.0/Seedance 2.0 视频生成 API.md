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
  - [创建素材组](#创建素材组)
  - [列出素材组](#列出素材组)
  - [删除素材组](#删除素材组)
  - [创建素材](#创建素材)
  - [列出素材](#列出素材)
  - [查询单个素材](#查询单个素材)
  - [删除素材](#删除素材)
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
| 虚拟人素材库（默认） | `aigc` | `POST /v1/asset-groups` 直接创建 | 任意非真人画风的角色/物品 |
| 真人素材库 | `liveness_face` | `POST /v1/visual-validate/session` H5 活体核验后创建 | 真人写真，仅可上传同一真人不同照片 |

**完整流程：**

```
1. 创建素材组         → POST /v1/asset-groups   或   /v1/visual-validate/session
2. 创建素材           → POST /v1/assets          （JSON：URL / Base64 / Data URI）
3. 等待状态变为 active → GET  /v1/assets         （轮询）
4. 在视频生成中引用    → POST /v1/video/generations（使用 asset:// URL）
```

> **请求格式说明**：所有素材库接口均使用 `Content-Type: application/json`，**不再支持 multipart 文件上传**。素材本体通过 `url` 字段提交，支持公网 URL 或 Base64。

**支持的素材类型与格式（虚拟人 / 真人 两类素材库均一致）：**

| `asset_type` | 支持格式 | 大小约束 |
|---|---|---|
| `Image`（默认） | jpg / jpeg / png / webp / bmp / tiff / gif / heic / heif | 单文件 ≤ 30 MB；长宽比 0.4–2.5；宽高 300–6000 px |
| `Video` | mp4 / mov | 上游异步预处理，建议短片段 |
| `Audio` | mp3 / wav | 用于音色/旁白参考 |

> 真人素材库（`liveness_face`）虽可上传图/视/音三类，但首次创建时仍需以**真人正脸图片**完成 H5 活体核验；后续追加视频/音频不会再触发人脸比对。

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

> 创建成功后请保存 `group_id`，后续上传素材都需要用到。每个 API Key 在每个上游空间最多可创建 **5 个素材组**。

#### 步骤 2：创建素材（JSON 方式）

`POST /v1/assets` 的 `url` 字段支持三种格式，三种方式任选其一：

| 格式 | 示例 | 适用场景 |
|------|------|---------|
| **公网 URL**（推荐） | `https://your-cdn.com/face.jpg` | 已有图床/OSS，性能最好 |
| **Data URI** | `data:image/png;base64,iVBORw0KGgoAAAANS...` | 本地图片，无需图床 |
| **纯 Base64 字符串** | `iVBORw0KGgoAAAANS...` | 同上，自动按 PNG 处理 |

> Base64/Data URI 提交后，上游会自动落到对象存储再返回信号 URL，全过程对调用方透明。

**方式 A：使用公网 URL**

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

**方式 B：使用 Base64 / Data URI**

```bash
# 把本地图片编码成 base64
B64=$(base64 -i portrait.jpg | tr -d '\n')

curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d "{
    \"url\": \"data:image/jpeg;base64,${B64}\",
    \"group_id\": \"group-20260508120000-abcde\",
    \"asset_type\": \"Image\",
    \"name\": \"portrait.jpg\"
  }"
```

响应：

```json
{
  "virtual_id": "asset-20260508120145-pqwhc",
  "asset_url": "asset://asset-20260508120145-pqwhc",
  "group_id": "group-20260508120000-abcde",
  "status": "pending"
}
```

| 字段 | 说明 |
|------|------|
| `virtual_id` | 素材唯一 ID |
| `asset_url` | 在视频生成中引用素材时使用的 URL（`asset://` 协议） |
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
| `POST` | `/v1/asset-groups` | 创建素材组（虚拟人 `aigc`） |
| `GET` | `/v1/asset-groups` | 列出素材组，支持 `?group_type=aigc\|liveness_face\|all` |
| `DELETE` | `/v1/asset-groups/{group_id}` | 删除素材组（级联删除组内所有素材） |
| `POST` | `/v1/assets` | 创建素材（URL / Base64 / Data URI；支持 `Image`/`Video`/`Audio`） |
| `GET` | `/v1/assets` | 列出素材，支持 `?group_id=`、`?group_type=` 过滤 |
| `GET` | `/v1/assets/{virtual_id}` | 查询单个素材，自动刷新上游状态 |
| `DELETE` | `/v1/assets/{virtual_id}` | 删除素材 |
| `POST` | `/v1/visual-validate/session` | 真人素材组：发起 H5 活体核验 |
| `POST` | `/v1/visual-validate/result` | 真人素材组：H5 回调内部使用，业务侧无需直接调用 |

---

### 创建素材组

**POST** `https://api.gravitex.ai/v1/asset-groups`

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | **是** | 素材组名称 |
| `description` | string | 否 | 描述 |
| `channel_id` | integer | 否 | 指定上游渠道 ID，省略则自动选择 |
| `group_type` | string | 否 | `aigc`（默认）。**`liveness_face` 不能在此创建**，需走 `POST /v1/visual-validate/session` |

**限额**：每个用户在每个上游渠道（同一 AK/SK 账号）下最多 5 个素材组（`aigc` 与 `liveness_face` 合计；可通过 `BYTEPLUS_ASSET_GROUP_LIMIT` 环境变量调整）。

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
  "channel_id": 123
}
```

---

### 列出素材组

**GET** `https://api.gravitex.ai/v1/asset-groups`

```bash
curl https://api.gravitex.ai/v1/asset-groups \
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

> `space_label`（A/B/C…）用于在多上游空间场景下区分不同的渠道，无需关心内部 channel_id。

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
| `url` | string | **是** | 素材内容，支持公网 URL、Data URI、纯 Base64 字符串 |
| `group_id` | string | **是** | 素材组 ID（来自 `POST /v1/asset-groups` 或 `/v1/visual-validate/session`） |
| `asset_type` | string | 否 | `Image`（默认）/ `Video` / `Audio`，大小写不敏感 |
| `name` | string | 否 | 素材名称（用于素材库 UI 展示与模糊搜索） |

> 自 2026-05 起，**虚拟人（aigc）与真人（liveness_face）两类素材库均支持图片、视频、音频三种 `asset_type`**。网关会根据 URL 后缀做基础格式校验，最终内容审核与转码由 BytePlus 异步完成。

**`url` 字段支持的三种格式：**

| 格式 | 示例 | 备注 |
|------|------|------|
| 公网 HTTP(S) URL | `https://your-cdn.com/face.jpg` | 性能最好，推荐（视频/音频建议必走 URL） |
| Data URI（带 MIME） | `data:image/png;base64,iVBORw0KGgo...` | 自动落对象存储；不建议用于大体积视频/音频 |
| 纯 Base64 字符串 | `iVBORw0KGgo...` | 默认按图片处理 |

**素材格式与大小约束：**

| `asset_type` | 支持后缀 | 单文件大小 | 备注 |
|---|---|---|---|
| `Image` | jpg / jpeg / png / webp / bmp / tiff / gif / heic / heif | ≤ 30 MB | 长宽比 0.4–2.5；宽高 300–6000 px |
| `Video` | mp4 / mov | 网关默认 ≤ 200 MB（前端校验） | 上游异步转码，建议 < 60 秒短片段 |
| `Audio` | mp3 / wav | ≤ 30 MB | 用于音色/旁白参考 |

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

**示例 — Data URI（图片）：**

```bash
curl -X POST https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQEA...",
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

---

### 列出素材

**GET** `https://api.gravitex.ai/v1/assets`

返回当前用户所有素材，自动刷新处理中的素材状态。

**Query 参数：**

| 名称 | 说明 |
|---|---|
| `group_id` | 按素材组 ID 过滤 |
| `group_type` | `aigc` / `liveness_face` / `all`，按所属素材组的类型过滤；未指定时返回全部 |

```bash
# 列出所有素材
curl https://api.gravitex.ai/v1/assets \
  -H "Authorization: Bearer sk-your_token_key"

# 按素材组过滤
curl "https://api.gravitex.ai/v1/assets?group_id=group-20260508120000-abcde" \
  -H "Authorization: Bearer sk-your_token_key"

# 仅列出真人素材库的全部素材
curl "https://api.gravitex.ai/v1/assets?group_type=liveness_face" \
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

### 真人素材组：H5 活体核验

真人素材库不能直接通过 `POST /v1/asset-groups` 创建——必须先在 BytePlus H5 页面完成真人活体核验，由网关回调结束后落库。

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
| `name` | string | **是** | 素材组名称（用于落库） |
| `description` | string | 否 | 描述 |
| `channel_id` | integer | 否 | 指定上游渠道 ID，省略则自动选择 |

```bash
curl -X POST https://api.gravitex.ai/v1/visual-validate/session \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"name": "真人A"}'
```

响应：

```json
{
  "h5_link": "https://verify.byteplus.com/h5/?token=…",
  "state": "<base64url>.<hmac>",
  "channel_id": 123,
  "byted_token": "bp-token-xxxxxxxx",
  "expires_in": 900
}
```

> 客户端通常只需把 `h5_link` 在 popup 中打开，并监听 `window.message` 的 `gravitex-asset-validate-result`；`state` 字段已被网关内置回调页自动转发，业务侧无需自行调用 `/v1/visual-validate/result`。

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

import base64
import mimetypes


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


def create_asset_by_url(group_id: str, image_url: str, name: str = "") -> dict:
    """通过公网 URL 创建素材"""
    resp = requests.post(
        f"{BASE_URL}/v1/assets",
        headers=HEADERS,
        json={
            "url": image_url,
            "group_id": group_id,
            "asset_type": "Image",
            "name": name,
        },
    )
    resp.raise_for_status()
    return resp.json()


def create_asset_by_file(group_id: str, file_path: str) -> dict:
    """通过本地文件创建素材（自动转 Base64 Data URI）"""
    mime = mimetypes.guess_type(file_path)[0] or "image/jpeg"
    with open(file_path, "rb") as f:
        b64 = base64.b64encode(f.read()).decode("ascii")
    data_uri = f"data:{mime};base64,{b64}"

    resp = requests.post(
        f"{BASE_URL}/v1/assets",
        headers=HEADERS,
        json={
            "url": data_uri,
            "group_id": group_id,
            "asset_type": "Image",
            "name": file_path.split("/")[-1],
        },
    )
    resp.raise_for_status()
    return resp.json()


# 3a. 准备素材组（首次执行时创建，之后复用）
group_id = get_or_create_group("角色A")
print(f"素材组: {group_id}")

# 3b. 创建素材（任选其一）
#   方式 A：使用公网 URL
# asset = create_asset_by_url(group_id, "https://your-cdn.com/portrait.jpg", "portrait.jpg")
#   方式 B：使用本地文件 → 自动转 Base64
asset = create_asset_by_file(group_id, "portrait.jpg")
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
2. 通过 `POST /v1/assets` 提交人像照片（JSON 格式，`url` 字段支持公网 URL 或 Base64）
3. 轮询 `GET /v1/assets` 等待 `status` 变为 `active`
4. 在视频生成时使用 `asset://` URL 引用该素材，并设置 `role` 为 `reference_image`

### Q: 素材库支持哪些上传格式？可以直接传 Base64 吗？

`POST /v1/assets` 接口的 `url` 字段支持以下三种格式（任选其一）：

- **公网 HTTP(S) URL**（推荐）：性能最好，适合大文件，**视频/音频请优先用 URL**
- **Data URI**：`data:image/png;base64,xxx...`，适合本地图片快速上传
- **纯 Base64 字符串**：自动按图片解析

并通过 `asset_type` 字段声明素材类型：

| `asset_type` | 支持后缀 | 大小约束 |
|---|---|---|
| `Image`（默认） | jpg / jpeg / png / webp / bmp / tiff / gif / heic / heif | ≤ 30 MB |
| `Video` | mp4 / mov | 网关默认 ≤ 200 MB |
| `Audio` | mp3 / wav | ≤ 30 MB |

> 不再支持 `multipart/form-data` 文件上传。如果你之前用 `-F file=@xxx.jpg` 调用会得到 `No file-upload channel available...` 错误，请改为 JSON 格式提交。

### Q: 真人素材库（liveness_face）能传视频或音频吗？

可以。**首次创建素材组时**仍需通过 `POST /v1/visual-validate/session` 走 H5 真人活体核验（核验只看脸部图片）；**核验通过后向该组追加素材**，与虚拟人素材库完全一致：`POST /v1/assets` 同时支持 `Image` / `Video` / `Audio`，不会再触发人脸比对。

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
