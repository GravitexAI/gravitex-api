# Seedance 2.0 官方镜像 API 用户文档

Seedance 2.0 官方镜像是与火山方舟（BytePlus Ark）官方 Seedance 2.0 API **请求体、响应体完全一致**的直连接口，只有 Base URL 和鉴权方式不同。如果已经有基于官方文档/SDK 编写的调用代码，只需要改 `base_url` 和 `Authorization` 头即可直接接入，无需改动任何请求参数或解析响应字段的代码。

如果需要平台自己简化过的接口（字段更少、响应结构更简单），请看 [Seedance 2.0 视频生成 API.md](./Seedance%202.0%20视频生成%20API.md)（`/v1/video/generations`、`/v1/assets` 等）。两套接口完全独立、互不影响，素材库部分的数据是互通的。

**Base URL**: `https://api.gravitex.ai`

---

## 目录

- [认证](#认证)
- [与官方接口的差异](#与官方接口的差异)
- [视频生成任务 API](#视频生成任务-api)
  - [创建任务](#创建任务)
  - [查询任务](#查询任务)
  - [取消/删除任务](#取消删除任务)
- [素材库 API](#素材库-api)
  - [接口总览](#接口总览)
  - [CreateAssetGroup — 创建素材组](#createassetgroup--创建素材组)
  - [CreateAsset — 创建素材](#createasset--创建素材)
  - [ListAssetGroups — 查询素材组列表](#listassetgroups--查询素材组列表)
  - [ListAssets — 查询素材列表](#listassets--查询素材列表)
  - [GetAsset — 查询单个素材](#getasset--查询单个素材)
  - [GetAssetGroup — 查询单个素材组](#getassetgroup--查询单个素材组)
  - [UpdateAsset — 更新素材](#updateasset--更新素材)
  - [UpdateAssetGroup — 更新素材组](#updateassetgroup--更新素材组)
  - [DeleteAsset — 删除素材](#deleteasset--删除素材)
  - [DeleteAssetGroup — 删除素材组](#deleteassetgroup--删除素材组)
- [真人素材库（Real-human Portrait Library）](#真人素材库real-human-portrait-library)
  - [与虚拟素材库的关系](#与虚拟素材库的关系)
  - [CreateVisualValidateSession — 发起真人核验会话](#createvisualvalidatesession--发起真人核验会话)
  - [GetVisualValidateResult — 查询核验结果并创建素材组](#getvisualvalidateresult--查询核验结果并创建素材组)
  - [真人素材的额外限制](#真人素材的额外限制)
- [参数参考](#参数参考)
  - [素材类型限制](#素材类型限制)
- [错误处理](#错误处理)
- [常见问题](#常见问题)

---

## 认证

所有接口统一使用 **Bearer Token** 认证。在 Gravitex AI 平台创建令牌后，在请求头中添加：

```
Authorization: Bearer sk-{your_token_key}
```

所有请求均使用 JSON 格式：

```
Content-Type: application/json
```

---

## 与官方接口的差异

除了下面两点，请求体的每个字段、响应体的每个字段都和官方文档一模一样：

| 项目 | 官方 | 本镜像 |
| --- | --- | --- |
| Base URL | `ark.ap-southeast.bytepluses.com` / `ark.ap-southeast-1.byteplusapi.com` | `https://api.gravitex.ai` |
| 鉴权方式 | 视频生成接口本来就是 Bearer；素材库接口是 **AK/SK + HMAC-SHA256 签名** | 统一改成平台的 `Bearer sk-xxx`（两类接口都用同一套鉴权，跟平台账号体系一致） |

> 平台目前只有 Bearer token 一套用户体系，没有给每个用户单独发 AK/SK，也没有实现火山的 HMAC-SHA256 签名校验——这是明确的范围限定：只镜像请求/响应体的字段形状，不镜像签名协议。

---

## 视频生成任务 API

### 创建任务

**POST** `https://api.gravitex.ai/api/v3/contents/generations/tasks`

与官方 [Create a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1520757) 请求体完全一致：

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | **是** | — | 模型 ID |
| `content` | array | 否* | — | 内容数组：`text` / `image_url` / `video_url` / `audio_url`，各元素带 `role`（`first_frame`/`last_frame`/`reference_image`/`reference_video`/`reference_audio`） |
| `prompt` | string | 否* | — | 简化写法，与 `content` 二选一 |
| `callback_url` | string | 否 | — | 任务状态变化时的回调地址 |
| `return_last_frame` | boolean | 否 | `false` | 是否返回最后一帧图片 |
| `service_tier` | string | 否 | `"default"` | `default` / `flex` |
| `execution_expires_after` | integer | 否 | `172800` | 任务过期时间（秒），范围 `[3600, 259200]` |
| `generate_audio` | boolean | 否 | `true` | 是否生成音频 |
| `safety_identifier` | string | 否 | — | 终端用户唯一标识（建议传用户 ID 的哈希） |
| `resolution` | string | 否 | `"720p"` | `480p` / `720p` / `1080p`（`seedance-2-0-fast` 不支持 `1080p`） |
| `ratio` | string | 否 | `"adaptive"` | `16:9` / `4:3` / `1:1` / `3:4` / `9:16` / `21:9` / `adaptive` |
| `duration` | integer | 否 | `5` | 秒数，`[4,15]` 或 `-1`（自动） |
| `seed` | integer | 否 | `-1` | 随机种子 |
| `watermark` | boolean | 否 | `false` | 是否加水印 |

> `content` 和 `prompt` 至少提供一个。`content[].image_url.url` / `video_url.url` / `audio_url.url` 支持三种值：公网 URL、Base64（`data:image/png;base64,...`）、`asset://<ASSET_ID>` 素材库引用（见[素材库 API](#素材库-api)）。

> **目前可用的 `model` 值**：`seedance-2-0`（标准版）、`seedance-2-0-fast`（快速版，不支持 `1080p`）、`seedance-2-0-NSFW`（放开内容安全限制，允许生成敏感/成人向内容，仅限已获授权的场景使用）。

> 请求体里没有任何字段被平台丢弃或改写——包括官方支持、但平台简化版接口（`/v1/video/generations`）不支持的 `execution_expires_after`/`service_tier`/`safety_identifier` 等字段，都会原样转发给火山。

**curl 示例**：

```bash
curl -X POST https://api.gravitex.ai/api/v3/contents/generations/tasks \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0",
    "content": [
      {"type": "text", "text": "黄金时刻，无人机航拍连绵山脉"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "16:9",
    "generate_audio": true
  }'
```

**响应**（创建成功）：

```json
{
  "id": "cgt-20260708094649-mxfjc"
}
```

---

### 查询任务

**GET** `https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}`

与官方 [Retrieve a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1521309) 响应体完全一致：

```json
{
  "id": "cgt-20260708094649-mxfjc",
  "model": "seedance-2-0",
  "status": "succeeded",
  "error": null,
  "created_at": 1783475210,
  "updated_at": 1783475364,
  "content": {
    "video_url": "https://...",
    "last_frame_url": "https://..."
  },
  "seed": 11,
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "generate_audio": true,
  "safety_identifier": "user-hash-abc",
  "service_tier": "default",
  "execution_expires_after": 172800,
  "usage": {
    "completion_tokens": 40594,
    "total_tokens": 40594
  }
}
```

| 字段 | 说明 |
| --- | --- |
| `status` | `queued` / `running` / `cancelled` / `succeeded` / `failed` / `expired`——注意这是官方原样枚举，不是平台简化接口的 `queued`/`in_progress`/`completed`/`failed` |
| `error` | 失败时非 `null`：`{"code": "...", "message": "..."}` |

**curl 示例**：

```bash
curl https://api.gravitex.ai/api/v3/contents/generations/tasks/cgt-20260708094649-mxfjc \
  -H "Authorization: Bearer sk-your_token_key"
```

---

### 取消/删除任务

**DELETE** `https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}`

按官方 [Cancel or delete a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1521720) 语义，`DELETE` 的行为取决于任务当前状态：

| 任务状态 | 能否删除 | 效果 | 删除后状态 |
| --- | --- | --- | --- |
| `queued` | 能 | 从队列移除，状态改为 `cancelled` | `cancelled` |
| `running` | **不能** | 上游拒绝 | — |
| `succeeded` | 能 | 任务记录被删除，之后查不到 | — |
| `failed` | 能 | 同上 | — |
| `cancelled` | **不能** | 上游拒绝 | — |
| `expired` | 能 | 同上 | — |

成功时响应体为空（`{}`），失败时原样透传上游的错误状态码和错误体。

> 这是平台从未有过的新能力，仅在这个官方镜像端点上提供，平台自己的 `/v1/video/generations/:task_id` 不受影响、也没有新增 `DELETE`。

**curl 示例**：

```bash
curl -X DELETE https://api.gravitex.ai/api/v3/contents/generations/tasks/cgt-20260708094649-mxfjc \
  -H "Authorization: Bearer sk-your_token_key"
```

---

## 素材库 API

所有素材库操作都走**同一个端点**，用 `Action` 查询参数区分具体操作（与官方完全一致的调用形状）：

```
POST https://api.gravitex.ai/api/v3/seedance?Action=<Action名>&Version=2024-01-01
Authorization: Bearer sk-your_token_key
Content-Type: application/json
```

**响应统一是官方原始信封形状**（不是平台自己拼的）：

```json
{
  "ResponseMetadata": {
    "RequestId": "...",
    "Action": "<Action名>",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "..."
  },
  "Result": { ... }
}
```

错误响应同理，走 `ResponseMetadata.Error.{Code,Message}` 官方形状。

支持的 10 个信封形状 `Action`：`CreateAssetGroup`、`CreateAsset`、`ListAssetGroups`、`ListAssets`、`GetAsset`、`GetAssetGroup`、`UpdateAsset`、`UpdateAssetGroup`、`DeleteAsset`、`DeleteAssetGroup`。另外还有 2 个真人核验专用 Action（`CreateVisualValidateSession`、`GetVisualValidateResult`，见[真人素材库](#真人素材库real-human-portrait-library)）——注意这两个的响应是**扁平结构**，不是上面的信封形状。传其他 Action 值会返回 `400 InvalidAction`。

> **素材组配额限制**：`CreateAssetGroup` 和 `CreateVisualValidateSession`（发起真人核验会话之前，因为核验通过的那一刻火山侧就已经创建了真人素材组，不能等到 `GetVisualValidateResult` 才拦截）都会做每用户素材组数量上限检查，超限会返回 `403`，`ResponseMetadata.Error.Code = "QuotaExceeded"`，且**不会调用火山**（本地硬拦截）。`GetVisualValidateResult` 也会做同样的检查作为第二道闸（两次调用之间配额可能被占满），但这只是兜底，不是主要防线。具体额度以平台后台配置为准。

### 接口总览

| Action | 官方文档 | 说明 |
| --- | --- | --- |
| `CreateAssetGroup` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318270) | 创建素材组（强制为虚拟 `AIGC` 类型，有配额检查） |
| `CreateAsset` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318271) | 创建素材 |
| `ListAssetGroups` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318272) | 查询素材组列表 |
| `ListAssets` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318273) | 查询素材列表 |
| `GetAsset` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318274) | 查询单个素材 |
| `GetAssetGroup` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318275) | 查询单个素材组 |
| `UpdateAsset` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318277) | 更新素材（目前仅支持 `Name`） |
| `UpdateAssetGroup` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318276) | 更新素材组（目前仅支持 `Name`/`Description`） |
| `DeleteAsset` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2318278) | 删除素材 |
| `DeleteAssetGroup` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2341606) | 删除素材组（级联删除组内所有素材） |
| `CreateVisualValidateSession` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2333587) | 真人素材组：发起 H5 真人核验会话（有配额检查） |
| `GetVisualValidateResult` | [文档](https://docs.byteplus.com/en/docs/ModelArk/2333588) | 真人素材组：查询核验结果并创建素材组（有配额检查，兜底） |

---

### CreateAssetGroup — 创建素材组

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=CreateAssetGroup&Version=2024-01-01`

与官方 [CreateAssetGroup](https://docs.byteplus.com/en/docs/ModelArk/2318270) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Name` | string | **是** | 素材组名称 |
| `Description` | string | 否 | 描述 |
| `GroupType` | string | 否 | **无论传什么，平台都会强制改写为 `AIGC`**——真人素材组（`LivenessFace`）只能走专属的真人核验流程（见[真人素材库](#真人素材库real-human-portrait-library)），本接口不支持创建 |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

> **配额限制**：超出每用户素材组数量上限会返回 `403`，`ResponseMetadata.Error.Code = "QuotaExceeded"`，不会调用火山。

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=CreateAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"Name": "角色A", "Description": "测试角色"}'
```

响应：

```json
{
  "ResponseMetadata": {"Action": "CreateAssetGroup", "Version": "2024-01-01", "Service": "ark"},
  "Result": {"Id": "group-20260710-abcde"}
}
```

---

### CreateAsset — 创建素材

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=CreateAsset&Version=2024-01-01`

与官方 [CreateAsset](https://docs.byteplus.com/en/docs/ModelArk/2318271) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `GroupId` | string | **是** | 素材组 ID |
| `URL` | string | **是** | 素材的公网可访问 URL（不支持 Base64/本地文件直传） |
| `AssetType` | string | **是** | `Image` / `Video` / `Audio` |
| `Name` | string | 否 | 素材名称，仅用于 `ListAssets` 模糊搜索，**不参与模型推理** |
| `Moderation` | object | 否 | 内容预审核策略，`{"Strategy": "Default"｜"Skip"}`；`Default`（默认）= 预审核开启，`Skip` = 跳过大部分非基线内容安全审核策略（需先在火山控制台关闭 Secure Mode） |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

> **不支持 Base64/本地文件**：`URL` 必须是公网可访问地址，图片/视频/音频素材只支持 URL 上传。

> **`Moderation.Strategy = "Skip"` 只是跳过大部分非基线审核策略，不是完全关闭审核**：素材仍可能因为基线安全策略被判定失败（`Status` 变成 `Failed`）。如果确认素材本身就是敏感/成人向内容、且跳过审核后依然被拦截，建议改用 `seedance-2-0-NSFW` 模型发起生成任务（见[创建任务](#创建任务)），而不是反复调整 `Moderation` 策略。

素材类型限制见[参数参考](#素材类型限制)。

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=CreateAsset&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "GroupId": "group-20260710-abcde",
    "URL": "https://your-cdn.com/portrait.jpg",
    "AssetType": "Image",
    "Name": "portrait.jpg"
  }'
```

响应：

```json
{
  "ResponseMetadata": {"Action": "CreateAsset"},
  "Result": {"Id": "asset-20260710-xyz"}
}
```

> 创建成功后，这个素材同时会被记录到平台本地表，之后可以在[创建任务](#创建任务)里用 `asset://asset-20260710-xyz` 引用，也能在平台自己的 `/v1/assets` 简化接口里查到——两套接口的数据是互通的。

---

### ListAssetGroups — 查询素材组列表

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=ListAssetGroups&Version=2024-01-01`

与官方 [ListAssetGroups](https://docs.byteplus.com/en/docs/ModelArk/2318272) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Filter.GroupIds` | array | 否 | 按素材组 ID 过滤 |
| `Filter.GroupType` | string | **是** | `AIGC` / `LivenessFace` |
| `Filter.Name` | string | 否 | 按名称模糊搜索 |
| `PageNumber` | integer | 否 | 页码，从 1 开始 |
| `PageSize` | integer | 否 | 每页数量，最多 100 |
| `SortBy` | string | 否 | 默认 `CreateTime`：`CreateTime` / `UpdateTime` |
| `SortOrder` | string | 否 | 默认 `Desc`：`Asc` / `Desc` |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=ListAssetGroups&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"Filter": {"GroupType": "AIGC"}, "PageNumber": 1, "PageSize": 10}'
```

响应：

```json
{
  "ResponseMetadata": {"Action": "ListAssetGroups"},
  "Result": {
    "TotalCount": 1,
    "Items": [
      {
        "Id": "group-20260710-abcde",
        "Name": "角色A",
        "Title": "角色A",
        "Description": "测试角色",
        "GroupType": "AIGC",
        "ProjectName": "default",
        "CreateTime": "2026-07-10T00:00:00Z",
        "UpdateTime": "2026-07-10T00:00:00Z"
      }
    ],
    "PageNumber": 1,
    "PageSize": 10
  }
}
```

---

### ListAssets — 查询素材列表

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=ListAssets&Version=2024-01-01`

与官方 [ListAssets](https://docs.byteplus.com/en/docs/ModelArk/2318273) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Filter.GroupIds` | array | 否 | 按素材组 ID 过滤 |
| `Filter.GroupType` | string | **是** | `AIGC` / `LivenessFace` |
| `Filter.Statuses` | array | 否 | `Active` / `Processing` / `Failed` |
| `Filter.Name` | string | 否 | 按名称模糊搜索 |
| `PageNumber` | integer | **是** | 页码，从 1 开始 |
| `PageSize` | integer | **是** | 每页数量，最多 100 |
| `SortBy` | string | 否 | 默认 `CreateTime`：`CreateTime` / `UpdateTime` / `GroupId` |
| `SortOrder` | string | 否 | 默认 `Desc`：`Asc` / `Desc` |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=ListAssets&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "Filter": {"GroupIds": ["group-20260710-abcde"], "Statuses": ["Active"]},
    "PageNumber": 1,
    "PageSize": 10
  }'
```

响应：

```json
{
  "ResponseMetadata": {"Action": "ListAssets"},
  "Result": {
    "Items": [
      {
        "Id": "asset-20260710-xyz",
        "Name": "portrait.jpg",
        "URL": "https://...(12小时有效签名URL)",
        "GroupId": "group-20260710-abcde",
        "AssetType": "Image",
        "Status": "Active",
        "Moderation": {"Strategy": "Default"},
        "ProjectName": "default",
        "CreateTime": "2026-07-10T00:00:00Z",
        "UpdateTime": "2026-07-10T00:00:00Z"
      }
    ],
    "TotalCount": 1,
    "PageNumber": 1,
    "PageSize": 10
  }
}
```

> `SortBy`/`SortOrder`（以及其他任何官方支持但没在上面列出的字段）都会原样转发给火山，不会被平台过滤掉。`Status` 为 `Failed` 的 Item 上还会多带一个 `Error: {Code, Message}` 对象。

---

### GetAsset — 查询单个素材

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=GetAsset&Version=2024-01-01`

与官方 [GetAsset](https://docs.byteplus.com/en/docs/ModelArk/2318274) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材 ID |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应：

```json
{
  "ResponseMetadata": {"Action": "GetAsset"},
  "Result": {
    "Id": "asset-20260710-xyz",
    "Name": "portrait.jpg",
    "URL": "https://...",
    "AssetType": "Image",
    "GroupId": "group-20260710-abcde",
    "Status": "Active",
    "Moderation": {"Strategy": "Default"},
    "CreateTime": "2026-07-10T00:00:00Z",
    "UpdateTime": "2026-07-10T00:00:00Z",
    "ProjectName": "default"
  }
}
```

`Status` 为 `Failed` 时，`Result` 里还会带 `Error: {Code, Message}`。

---

### GetAssetGroup — 查询单个素材组

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=GetAssetGroup&Version=2024-01-01`

与官方 [GetAssetGroup](https://docs.byteplus.com/en/docs/ModelArk/2318275) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材组 ID |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应：

```json
{
  "ResponseMetadata": {"Action": "GetAssetGroup"},
  "Result": {
    "Id": "group-20260710-abcde",
    "Name": "角色A",
    "Description": "测试角色",
    "GroupType": "AIGC",
    "ProjectName": "default",
    "CreateTime": "2026-07-10T00:00:00Z",
    "UpdateTime": "2026-07-10T00:00:00Z"
  }
}
```

---

### UpdateAsset — 更新素材

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=UpdateAsset&Version=2024-01-01`

与官方 [UpdateAsset](https://docs.byteplus.com/en/docs/ModelArk/2318277) 一致，**官方目前只支持更新 `Name`。**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材 ID |
| `Name` | string | 否 | 新名称 |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应：

```json
{
  "ResponseMetadata": {"Action": "UpdateAsset"},
  "Result": {"Id": "asset-20260710-xyz"}
}
```

---

### UpdateAssetGroup — 更新素材组

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=UpdateAssetGroup&Version=2024-01-01`

与官方 [UpdateAssetGroup](https://docs.byteplus.com/en/docs/ModelArk/2318276) 一致，**官方目前只支持更新 `Name` 和 `Description`。**

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材组 ID |
| `Name` | string | 否 | 新名称 |
| `Description` | string | 否 | 新描述 |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应：

```json
{
  "ResponseMetadata": {"Action": "UpdateAssetGroup"},
  "Result": {"Id": "group-20260710-abcde"}
}
```

---

### DeleteAsset — 删除素材

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=DeleteAsset&Version=2024-01-01`

与官方 [DeleteAsset](https://docs.byteplus.com/en/docs/ModelArk/2318278) 请求体完全一致：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材 ID |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应（无业务返回参数）：

```json
{
  "ResponseMetadata": {"Action": "DeleteAsset"},
  "Result": {}
}
```

---

### DeleteAssetGroup — 删除素材组

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=DeleteAssetGroup&Version=2024-01-01`

与官方 [DeleteAssetGroup](https://docs.byteplus.com/en/docs/ModelArk/2341606) 请求体完全一致：

⚠️ **删除素材组会级联删除组内所有素材，不可撤销。**

⚠️ **真人素材组（`LivenessFace`）额外限制**：只有授权已过期、或授权被拒绝的素材组才能删除；授权仍在有效期内、授权期尚未开始、或已通过审核的素材组无法删除（上游会拒绝）。虚拟素材组（`AIGC`）没有这个限制。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Id` | string | **是** | 素材组 ID |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

响应（无业务返回参数）：

```json
{
  "ResponseMetadata": {"Action": "DeleteAssetGroup"},
  "Result": {}
}
```

---

## 真人素材库（Real-human Portrait Library）

### 与虚拟素材库的关系

真人素材库（`GroupType = LivenessFace`）跟前面的虚拟素材库（`GroupType = AIGC`）**共用同一套 Asset/AssetGroup Action**（`CreateAsset`、`ListAssetGroups`、`ListAssets`、`GetAsset`、`GetAssetGroup`、`UpdateAsset`、`UpdateAssetGroup`、`DeleteAsset`、`DeleteAssetGroup`），调用方式、请求体、响应体完全一样，只是 `Filter.GroupType`/查询结果里的 `GroupType` 变成 `LivenessFace`。

**唯一的区别**：真人素材组不能通过 `Action=CreateAssetGroup` 创建——这个 Action 在本平台被**强制改写成 `AIGC`**（见 [CreateAssetGroup](#createassetgroup--创建素材组)），所以真人素材组只能走「真人核验」流程来创建，核验通过后拿到的 `GroupId`，后续增删改查跟虚拟素材组一模一样地使用。

> 平台还提供一套**平台专属便捷版**真人核验流程（`/v1/visual-validate/session`、`/v1/visual-validate/result`），请求/响应体不是官方原始形状，但可以提前传 `name`/`description`，且核验完成后走浏览器自动重定向回调完成绑定，无需调用方主动轮询结果——面向普通前端用户时更合适。两套流程效果等价、二选一，创建出来的真人素材组在后续查询/使用上没有任何区别。

### CreateVisualValidateSession — 发起真人核验会话

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=CreateVisualValidateSession&Version=2024-01-01`

与官方 [CreateVisualValidateSession](https://docs.byteplus.com/en/docs/ModelArk/2333587) 请求体完全一致。

> ⚠️ **响应形状跟其他 Action 不一样**：这是官方原始的**扁平结构**，不是 `{ResponseMetadata, Result}` 信封。
>
> ⚠️ **配额检查发生在这一步**：火山官方文档写明 `GetVisualValidateResult` 返回的 `GroupId` 是 "newly created"——也就是说真人素材组是在用户 H5 核验通过的那一刻由火山创建的，`GetVisualValidateResult` 只是事后查询。所以配额上限检查放在这里（发起核验会话之前），超限会返回 `403`，`ResponseMetadata.Error.Code = "QuotaExceeded"`，不会调用火山、不会生成 H5 核验链接。如果放到 `GetVisualValidateResult` 才检查，配额满的用户仍会被放行去做真人核验，核验通过后却因为超配额永远查不到 `GroupId`——核验白做。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `CallbackURL` | string | 否 | **无论传什么，平台都会强制改写为本平台自己的回调页地址**——防止跳转到调用方指定的任意地址。`GetVisualValidateResult` 走的是带 Bearer 鉴权的同一个 API，不依赖回调页传递任何上下文，所以强制改写不影响核验换 `GroupId` 的能力 |
| `ProjectName` | string | 否 | 默认 `default`，火山项目名 |

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=CreateVisualValidateSession&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{}'
```

响应：

```json
{
  "BytedToken": "202607...",
  "H5Link": "https://www.byteplus.com/en/liveness-face-manage/authorization?pl=...",
  "CallbackURL": "https://api.gravitex.ai/asset-validate-callback.html"
}
```

把 `H5Link` 展示给终端用户（建议用 WebView/浏览器打开）完成真人核验。`BytedToken` 需要调用方自己保存，30 分钟内有效、只能核验一次——核验完成后用它调用下面的接口换 `GroupId`。回调页只是核验完成后的兜底展示页，不影响用 `BytedToken` 主动轮询/查询结果。

### GetVisualValidateResult — 查询核验结果并创建素材组

**POST** `https://api.gravitex.ai/api/v3/seedance?Action=GetVisualValidateResult&Version=2024-01-01`

与官方 [GetVisualValidateResult](https://docs.byteplus.com/en/docs/ModelArk/2333588) 请求体完全一致。

> ⚠️ 响应同样是**扁平结构**，不是 `{ResponseMetadata, Result}` 信封。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `BytedToken` | string | **是** | `CreateVisualValidateSession` 响应里的 `BytedToken` |

```bash
curl -X POST "https://api.gravitex.ai/api/v3/seedance?Action=GetVisualValidateResult&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"BytedToken": "202607..."}'
```

响应（核验通过）：

```json
{
  "GroupId": "group-20260710-face01"
}
```

调用成功即代表真人核验已通过，本地会同步创建一条 `GroupType = LivenessFace` 的素材组记录。这一步也会做同样的配额检查（超限返回 `403 QuotaExceeded`，不调用火山）——但这只是第二道闸，主要拦截点在上一步的 `CreateVisualValidateSession`，因为素材组在火山侧其实已经创建，这里检查失败只影响本地能否同步到 `GroupId`。拿到 `GroupId` 之后，就可以像操作 `AIGC` 素材组一样，用 `CreateAsset`/`ListAssets`/`GetAsset`/`UpdateAsset`/`DeleteAsset`/`DeleteAssetGroup` 等 Action 往这个组里传素材、查询、更新、删除。

核验尚未完成、或核验被拒绝时，上游会返回相应的错误状态码和错误体（原样透传）。

### 真人素材的额外限制

- **配额独立**：每个用户在每个渠道下的真人素材组数量单独限额（跟 `AIGC` 素材组配额分开计算），具体额度以平台后台配置为准。
- **人脸匹配**：每个真人素材组对应唯一一个真实人物；往组里上传素材时，官方会跟核验时留存的参考人脸比对，人物不匹配或图片中出现多张人脸都会导致素材上传失败（`Status` 变成 `Failed`，`Error` 里带具体原因）。
- **删除限制**：见 [DeleteAssetGroup](#deleteassetgroup--删除素材组)——只有授权过期或被拒绝的真人素材组才能删除。
- 素材的引用方式（`asset://<ID>`）、素材类型限制跟虚拟素材库完全一致，不再重复。

---

## 参数参考

### 素材类型限制

`CreateAsset` 的 `AssetType` 对应的文件要求（虚拟 `AIGC` 与真人 `LivenessFace` 两类素材库均一致），超出限制上游会报错：

| 类型 | 格式 | 时长 | 分辨率/尺寸 | 宽高比 (W/H) | 大小 | 帧率 |
| --- | --- | --- | --- | --- | --- | --- |
| `Image` | jpeg / png / webp / bmp / tiff / gif / heic / heif | — | 宽高 300–6000px | 0.4–2.5 | < 30 MB | — |
| `Video` | mp4 / mov | 2–15 秒 | 480p/720p/1080p，宽高 300–6000px，总像素 (W×H) 409600–2086876 | 0.4–2.5 | ≤ 50 MB | 24–60 fps |
| `Audio` | wav / mp3 | 2–15 秒 | — | — | ≤ 15 MB | — |

---

## 错误处理

素材库接口的错误响应走官方原始信封形状（`CreateVisualValidateSession`/`GetVisualValidateResult` 除外，两者是扁平结构透传）：

```json
{
  "ResponseMetadata": {
    "Action": "<Action名>",
    "Version": "2024-01-01",
    "Service": "ark",
    "Error": {"Code": "InvalidParameter", "Message": "..."}
  }
}
```

| 场景 | HTTP 状态码 | `Error.Code` |
| --- | --- | --- |
| `Action` 不在支持列表 | `400` | `InvalidAction` |
| 请求体不是合法 JSON | `400` | `InvalidParameter` |
| 无可用 BytePlus 渠道 | `503` | `NoChannel` |
| 素材组配额检查失败（DB 查询出错） | `500` | `QuotaCheckFailed` |
| 超出素材组数量上限 | `403` | `QuotaExceeded` |
| 上游调用失败 | `502` | `UpstreamError` |

视频生成任务 API（[创建任务](#创建任务)/[查询任务](#查询任务)/[取消删除任务](#取消删除任务)）的错误响应原样透传上游状态码和错误体，格式跟官方文档一致。

---

## 常见问题

### Q: 这套接口跟平台自己的 `/v1/video/generations`、`/v1/assets` 是什么关系？

完全独立、互不影响的两套接口。素材库这块数据是互通的——用官方形状 `CreateAsset` 建的素材，能在 `/v1/assets` 里查到，反之亦然。视频生成这块目前是分开的两个路由，互不干扰。

### Q: 为什么素材库鉴权用 Bearer token，官方文档写的是 AK/SK 签名？

平台目前只有 Bearer token 一套用户体系，没有给每个用户单独发 AK/SK、也没有实现火山的 HMAC-SHA256 签名校验。这是明确的范围限定——只镜像请求/响应体的字段形状，不镜像签名协议。

### Q: `model` 字段能不能直接抄官方文档示例里的模型 ID（比如 `dreamina-seedance-2-0-260128`）？

不一定。平台渠道注册的模型名可能跟官方示例不完全一致。目前平台实际可用的模型只有三个：`seedance-2-0`、`seedance-2-0-fast`、`seedance-2-0-NSFW`（见[创建任务](#创建任务)）。如果传的 `model` 字符串没有被任何渠道注册，会报“无可用渠道”错误。

### Q: `seedance-2-0`、`seedance-2-0-fast`、`seedance-2-0-NSFW` 该怎么选？

- `seedance-2-0`：标准版，功能最全，支持 `1080p`，走完整内容安全审核。
- `seedance-2-0-fast`：出图/出视频更快，但不支持 `1080p`（`resolution` 传 `1080p` 会被上游拒绝），内容安全审核策略和标准版一致，适合对分辨率要求不高、追求速度的场景。
- `seedance-2-0-NSFW`：内容安全限制放开，允许生成敏感/成人向内容，仅限已获授权的场景使用，未授权账号调用会被上游拒绝。三个模型的请求/响应结构完全一致，只是 `model` 字段值不同。

### Q: 素材已经用 `Moderation.Strategy = "Skip"` 跳过审核了，为什么生成任务还是报内容安全错误？

`Skip` 只是跳过大部分非基线内容安全审核策略，不是完全关闭审核——素材本身仍可能因为基线安全策略被判定失败，生成任务也会对 `prompt`/素材内容单独做安全检查。如果确认这段内容本来就是敏感/成人向的、且业务场景已获授权，建议直接改用 `seedance-2-0-NSFW` 模型发起生成任务，而不是反复调整 `Moderation` 策略去绕过标准模型的审核。

### Q: 取消一个 `running` 状态的任务会怎样？

上游会拒绝，返回错误状态码和错误体（原样透传，不做二次包装）。具体的错误码格式以实际请求为准，官方文档没有详细说明这个错误的具体格式。

### Q: 素材库的 `SortBy`/`SortOrder` 等边缘字段能用吗？

能。这套接口的请求体是原样转发给火山的，不会因为平台内部结构体没有对应字段就被丢弃。

### Q: 真人素材组能不能像虚拟素材组一样直接用 `CreateAssetGroup` 创建？

不能。`CreateAssetGroup` 在本平台被强制改写成 `AIGC`（见 [CreateAssetGroup](#createassetgroup--创建素材组)），真人素材组只能走 `CreateVisualValidateSession`/`GetVisualValidateResult`（或平台专属便捷版 `/v1/visual-validate/session`、`/v1/visual-validate/result`）的专属核验流程创建，创建之后的增删改查复用同一套 Action。

### Q: 官方镜像版真人核验和平台专属便捷版应该用哪个？

效果等价，二选一。已经有官方 SDK 调用代码、想最小改动接入的场景用官方镜像版（`CreateVisualValidateSession`/`GetVisualValidateResult`，字段跟官方基本一致，只有 `CallbackURL` 会被强制改写）；面向普通前端用户、想提前传素材组名称且核验完成后自动跳转回调完成绑定的场景用平台专属便捷版（`/v1/visual-validate/session`、`/v1/visual-validate/result`）。
