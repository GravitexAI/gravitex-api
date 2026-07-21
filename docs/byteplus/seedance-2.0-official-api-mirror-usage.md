# Seedance 2.0 官方镜像接口 — 调用文档

> 状态：已实现（14 个任务全部完成并通过 review）
> 关联文档：`docs/byteplus/seedance-2.0-official-api-mirror-design.md`（设计文档）
> Base URL（暂定）：`https://api.gravitex.ai`

## 一、概述

这套接口与火山方舟（BytePlus Ark）官方 Seedance 2.0 API **请求体、响应体完全一致**，唯一的区别是：

- **Base URL** 换成了平台自己的域名（`https://api.gravitex.ai`），不是火山的 `ark.ap-southeast.bytepluses.com` / `ark.ap-southeast-1.byteplusapi.com`。
- **鉴权方式统一用平台的** `Bearer sk-xxx` **token**，不是官方文档里 AK/SK 签名（视频生成接口官方本来就是 Bearer，素材库接口官方是 AK/SK+HMAC 签名，这里为了跟平台账号体系统一，两类接口都改成了 Bearer）。

除了这两点，请求体的每个字段、响应体的每个字段都和官方文档一模一样，可以直接照抄火山官方文档/SDK 的调用代码，只改 `base_url` 和 `Authorization`。

如果需要平台自己简化过的接口（字段更少、响应结构不同），请看 `docs/byteplus/seedance2.0/Seedance 2.0 视频生成 API.md`（`/v1/video/generations`、`/v1/assets` 等），跟本文档说的是两套完全独立、互不影响的接口。

素材库部分（第三、四节）覆盖虚拟素材（`AIGC`）和真人素材（`LivenessFace`）两种素材组：素材的增删改查是同一套镜像接口，唯一的区别在于真人素材组的创建流程——第四节单独说明。

---



## 二、视频生成任务（3 个接口）



### 2.1 创建任务

```
POST https://api.gravitex.ai/api/v3/contents/generations/tasks
Authorization: Bearer sk-your_token_key
Content-Type: application/json
```

**请求体**（与官方 [Create a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1520757) 完全一致）：


| 字段                        | 类型      | 必填              | 说明                                                                                                                                          |
| ------------------------- | ------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `model`                   | string  | 是               | 模型 ID                                                                                                                                       |
| `content`                 | array   | 否*              | 内容数组：text / image_url / video_url / audio_url，各元素带 `role`（`first_frame`/`last_frame`/`reference_image`/`reference_video`/`reference_audio`） |
| `prompt`                  | string  | 否*              | 简化写法，与 `content` 二选一                                                                                                                        |
| `callback_url`            | string  | 否               | 任务状态变化时的回调地址                                                                                                                                |
| `return_last_frame`       | boolean | 否，默认 `false`    | 是否返回最后一帧图片                                                                                                                                  |
| `service_tier`            | string  | 否，默认 `default`  | `default` / `flex`                                                                                                                          |
| `execution_expires_after` | integer | 否，默认 `172800`   | 任务过期时间（秒），范围 `[3600, 259200]`                                                                                                               |
| `generate_audio`          | boolean | 否，默认 `true`     | 是否生成音频                                                                                                                                      |
| `safety_identifier`       | string  | 否               | 终端用户唯一标识（建议传用户 ID 的哈希）                                                                                                                      |
| `resolution`              | string  | 否，默认 `720p`     | `480p` / `720p` / `1080p`（seedance-2-0-fast 不支持 1080p）                                                                                      |
| `ratio`                   | string  | 否，默认 `adaptive` | `16:9` / `4:3` / `1:1` / `3:4` / `9:16` / `21:9` / `adaptive`                                                                               |
| `duration`                | integer | 否，默认 `5`        | 秒数，`[4,15]` 或 `-1`（自动）                                                                                                                      |
| `seed`                    | integer | 否，默认 `-1`       | 随机种子                                                                                                                                        |
| `watermark`               | boolean | 否，默认 `false`    | 是否加水印                                                                                                                                       |


请求体里的 `content[].image_url.url` / `video_url.url` / `audio_url.url` 支持三种值：公网 URL、Base64（`data:image/png;base64,...`）、`asset://<ASSET_ID>` 素材库引用（见第三节）。

**响应**（创建成功）：

```json
{
  "id": "cgt-20260708094649-mxfjc"
}
```

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

> 请求体里没有任何字段被平台丢弃或改写——包括官方支持、但平台自己简化版接口（`/v1/video/generations`）不支持的 `execution_expires_after`/`service_tier`/`safety_identifier` 等字段，这里都会原样转发给火山。

---



### 2.2 查询任务

```
GET https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}
Authorization: Bearer sk-your_token_key
```

**响应**（与官方 [Retrieve a video generation task](https://docs.byteplus.com/en/docs/ModelArk/1521309) 完全一致）：

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

`status` 取值：`queued` / `running` / `cancelled` / `succeeded` / `failed` / `expired`（注意：不是平台简化接口的 `queued`/`in_progress`/`completed`/`failed`，是官方原样枚举）。

`error`（失败时非 null）：`{"code": "...", "message": "..."}`。

**curl 示例**：

```bash
curl https://api.gravitex.ai/api/v3/contents/generations/tasks/cgt-20260708094649-mxfjc \
  -H "Authorization: Bearer sk-your_t/api/v3/contents/generations/tasksoken_key"
```

---



### 2.3 取消 / 删除任务

```
DELETE https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}
Authorization: Bearer sk-your_token_key
```

按官方语义，`DELETE` 的行为取决于任务当前状态：


| 任务状态        | 能否删除   | 效果                     | 删除后状态       |
| ----------- | ------ | ---------------------- | ----------- |
| `queued`    | 能      | 从队列移除，状态改为 `cancelled` | `cancelled` |
| `running`   | **不能** | 上游拒绝                   | —           |
| `succeeded` | 能      | 任务记录被删除，之后查不到          | —           |
| `failed`    | 能      | 同上                     | —           |
| `cancelled` | **不能** | 上游拒绝                   | —           |
| `expired`   | 能      | 同上                     | —           |


成功时响应体为空（`{}`），失败时原样透传上游的错误状态码和错误体。

**curl 示例**：

```bash
curl -X DELETE https://api.gravitex.ai/api/v3/contents/generations/tasks/cgt-20260708094649-mxfjc \
  -H "Authorization: Bearer sk-your_token_key"
```

> 这是平台从未有过的新能力，仅在这个官方镜像端点上提供，平台自己的 `/v1/video/generations/:task_id` 不受影响、也没有新增 DELETE。

---



## 三、素材库（10 个接口，单端点 + Action 区分）



### 3.1 通用说明

所有素材库操作都走**同一个端点**，用 `Action` 查询参数区分具体操作（与官方完全一致的调用形状）：

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=<Action名>&Version=2024-01-01
Authorization: Bearer sk-your_token_key
Content-Type: application/json
```

> **鉴权差异（唯一跟官方不一样的地方）**：官方素材库接口要求 AK/SK + HMAC-SHA256 签名（`Authorization: HMAC-SHA256 Credential=...`），这里改成了平台的 `Bearer sk-xxx`，其余请求头（`Content-Type: application/json`）、请求体、响应体都不变。

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

支持的 10 个 `Action`：`CreateAssetGroup`、`CreateAsset`、`ListAssetGroups`、`ListAssets`、`GetAsset`、`GetAssetGroup`、`UpdateAsset`、`UpdateAssetGroup`、`DeleteAsset`、`DeleteAssetGroup`。传其他值会返回 `400 InvalidAction`。

---



### 3.2 CreateAssetGroup — 创建素材组

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=CreateAssetGroup&Version=2024-01-01
```


| 字段            | 类型     | 必填             | 说明                                                                                                             |
| ------------- | ------ | -------------- | -------------------------------------------------------------------------------------------------------------- |
| `Name`        | string | 是              | 素材组名称                                                                                                          |
| `Description` | string | 否              | 描述                                                                                                             |
| `GroupType`   | string | 否              | **无论传什么，平台都会强制改写为** `AIGC`——真人素材组（`LivenessFace`）只能走专属的 H5 真人核验流程（`/v1/visual-validate/session`，见第四节），本接口不支持创建 |
| `ProjectName` | string | 否，默认 `default` | 火山项目名                                                                                                          |


**请求示例**：

```bash
curl -X POST "https://api.gravitex.ai/ark/seedance/v3?Action=CreateAssetGroup&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"Name": "角色A", "Description": "测试角色"}'
```

**响应**：

```json
{
  "ResponseMetadata": {"Action": "CreateAssetGroup", "Version": "2024-01-01", "Service": "ark", "..."},
  "Result": {"Id": "group-20260710-abcde"}
}
```

---



### 3.3 CreateAsset — 创建素材

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=CreateAsset&Version=2024-01-01
```


| 字段            | 类型     | 必填             | 说明                                                                                                                        |
| ------------- | ------ | -------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `GroupId`     | string | 是              | 素材组 ID                                                                                                                    |
| `URL`         | string | 是              | 素材的公网可访问 URL（不支持 Base64/本地文件）                                                                                             |
| `AssetType`   | string | 是              | `Image` / `Video` / `Audio`                                                                                               |
| `Name`        | string | 否              | 素材名称，仅用于 `ListAssets` 模糊搜索，**不会**参与模型推理（引用素材时用 `asset://<ID>` 或 prompt 里的"图片 N"/"视频 N"，不要用 Name）                          |
| `Moderation`  | object | 否              | 内容预审核策略，`{"Strategy": "Default"｜"Skip"}`；`Default`（不传本字段时的默认行为）= 预审核开启，`Skip` = 跳过大部分非基线内容安全审核策略（需要先在火山控制台关闭 Secure Mode） |
| `ProjectName` | string | 否，默认 `default` | 火山项目名                                                                                                                     |


> **不支持 Base64/本地文件**：`URL` 必须是公网可访问地址，图片/视频/音频素材只支持 URL 上传。

**素材类型限制**（`AssetType` 对应的文件要求，超出限制上游会报错）：


| 类型      | 格式                                               | 时长     | 分辨率/尺寸                                                 | 宽高比 (W/H) | 大小      | 帧率        |
| ------- | ------------------------------------------------ | ------ | ------------------------------------------------------ | --------- | ------- | --------- |
| `Image` | jpeg / png / webp / bmp / tiff / gif / heic/heif | —      | 宽高 300–6000px                                          | 0.4–2.5   | < 30 MB | —         |
| `Video` | mp4 / mov                                        | 2–15 秒 | 480p/720p/1080p，宽高 300–6000px，总像素 (W×H) 409600–2086876 | 0.4–2.5   | ≤ 50 MB | 24–60 fps |
| `Audio` | wav / mp3                                        | 2–15 秒 | —                                                      | —         | ≤ 15 MB | —         |


**请求示例**：

```bash
curl -X POST "https://api.gravitex.ai/ark/seedance/v3?Action=CreateAsset&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "GroupId": "group-20260710-abcde",
    "URL": "https://your-cdn.com/portrait.jpg",
    "AssetType": "Image",
    "Name": "portrait.jpg"
  }'
```

**响应**：

```json
{
  "ResponseMetadata": {"Action": "CreateAsset", "...": "..."},
  "Result": {"Id": "asset-20260710-xyz"}
}
```

> 创建成功后，这个素材同时会被记录到平台本地表，之后可以在视频生成请求（第二节）里用 `asset://asset-20260710-xyz` 引用，也能在平台自己的 `/v1/assets` 简化接口里查到——两套接口的数据是互通的。

---



### 3.4 ListAssetGroups — 查询素材组列表

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=ListAssetGroups&Version=2024-01-01
```


| 字段                 | 类型      | 必填                | 说明                          |
| ------------------ | ------- | ----------------- | --------------------------- |
| `Filter.GroupIds`  | array   | 否                 | 按素材组 ID 过滤                  |
| `Filter.GroupType` | string  | 是                 | `AIGC` / `LivenessFace`     |
| `Filter.Name`      | string  | 否                 | 按名称模糊搜索                     |
| `PageNumber`       | integer | 否                 | 页码，从 1 开始                   |
| `PageSize`         | integer | 否                 | 每页数量，最多 100                 |
| `SortBy`           | string  | 否，默认 `CreateTime` | `CreateTime` / `UpdateTime` |
| `SortOrder`        | string  | 否，默认 `Desc`       | `Asc` / `Desc`              |
| `ProjectName`      | string  | 否，默认 `default`    | 火山项目名                       |


**请求示例**：

```bash
curl -X POST "https://api.gravitex.ai/ark/seedance/v3?Action=ListAssetGroups&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{"Filter": {"GroupType": "AIGC"}, "PageNumber": 1, "PageSize": 10}'
```

**响应**：

```json
{
  "ResponseMetadata": {"Action": "ListAssetGroups", "...": "..."},
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



### 3.5 ListAssets — 查询素材列表

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=ListAssets&Version=2024-01-01
```


| 字段                 | 类型      | 必填                | 说明                                      |
| ------------------ | ------- | ----------------- | --------------------------------------- |
| `Filter.GroupIds`  | array   | 否                 | 按素材组 ID 过滤                              |
| `Filter.GroupType` | string  | 是                 | `AIGC` / `LivenessFace`                 |
| `Filter.Statuses`  | array   | 否                 | `Active` / `Processing` / `Failed`      |
| `Filter.Name`      | string  | 否                 | 按名称模糊搜索                                 |
| `PageNumber`       | integer | 是                 | 页码，从 1 开始                               |
| `PageSize`         | integer | 是                 | 每页数量，最多 100                             |
| `SortBy`           | string  | 否，默认 `CreateTime` | `CreateTime` / `UpdateTime` / `GroupId` |
| `SortOrder`        | string  | 否，默认 `Desc`       | `Asc` / `Desc`                          |
| `ProjectName`      | string  | 否，默认 `default`    | 火山项目名                                   |


**请求示例**：

```bash
curl -X POST "https://api.gravitex.ai/ark/seedance/v3?Action=ListAssets&Version=2024-01-01" \
  -H "Authorization: Bearer sk-your_token_key" \
  -H "Content-Type: application/json" \
  -d '{
    "Filter": {"GroupIds": ["group-20260710-abcde"], "Statuses": ["Active"]},
    "PageNumber": 1,
    "PageSize": 10
  }'
```

**响应**：

```json
{
  "ResponseMetadata": {"Action": "ListAssets", "...": "..."},
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



### 3.6 GetAsset — 查询单个素材

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=GetAsset&Version=2024-01-01
```


| 字段            | 类型     | 必填             | 说明    |
| ------------- | ------ | -------------- | ----- |
| `Id`          | string | 是              | 素材 ID |
| `ProjectName` | string | 否，默认 `default` | 火山项目名 |


**响应**：

```json
{
  "ResponseMetadata": {"Action": "GetAsset", "...": "..."},
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



### 3.7 GetAssetGroup — 查询单个素材组

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=GetAssetGroup&Version=2024-01-01
```


| 字段            | 类型     | 必填             | 说明     |
| ------------- | ------ | -------------- | ------ |
| `Id`          | string | 是              | 素材组 ID |
| `ProjectName` | string | 否，默认 `default` | 火山项目名  |


**响应**：

```json
{
  "ResponseMetadata": {"Action": "GetAssetGroup", "...": "..."},
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



### 3.8 UpdateAsset — 更新素材

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=UpdateAsset&Version=2024-01-01
```

**官方目前只支持更新** `Name`**。**


| 字段            | 类型     | 必填             | 说明    |
| ------------- | ------ | -------------- | ----- |
| `Id`          | string | 是              | 素材 ID |
| `Name`        | string | 否              | 新名称   |
| `ProjectName` | string | 否，默认 `default` | 火山项目名 |


**响应**：

```json
{
  "ResponseMetadata": {"Action": "UpdateAsset", "...": "..."},
  "Result": {"Id": "asset-20260710-xyz"}
}
```

---



### 3.9 UpdateAssetGroup — 更新素材组

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=UpdateAssetGroup&Version=2024-01-01
```

**官方目前只支持更新** `Name` **和** `Description`**。**


| 字段            | 类型     | 必填             | 说明     |
| ------------- | ------ | -------------- | ------ |
| `Id`          | string | 是              | 素材组 ID |
| `Name`        | string | 否              | 新名称    |
| `Description` | string | 否              | 新描述    |
| `ProjectName` | string | 否，默认 `default` | 火山项目名  |


**响应**：

```json
{
  "ResponseMetadata": {"Action": "UpdateAssetGroup", "...": "..."},
  "Result": {"Id": "group-20260710-abcde"}
}
```

---



### 3.10 DeleteAsset — 删除素材

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=DeleteAsset&Version=2024-01-01
```


| 字段            | 类型     | 必填             | 说明    |
| ------------- | ------ | -------------- | ----- |
| `Id`          | string | 是              | 素材 ID |
| `ProjectName` | string | 否，默认 `default` | 火山项目名 |


**响应**（无业务返回参数）：

```json
{
  "ResponseMetadata": {"Action": "DeleteAsset", "...": "..."},
  "Result": {}
}
```

---



### 3.11 DeleteAssetGroup — 删除素材组

```
POST https://api.gravitex.ai/ark/seedance/v3?Action=DeleteAssetGroup&Version=2024-01-01
```

⚠️ **删除素材组会级联删除组内所有素材，不可撤销。**

⚠️ **真人素材组（**`LivenessFace`**）额外限制**：只有授权已过期、或授权被拒绝的素材组才能删除；授权仍在有效期内、授权期尚未开始、或已通过审核的素材组无法删除（上游会拒绝）。虚拟素材组（`AIGC`）没有这个限制。


| 字段            | 类型     | 必填             | 说明     |
| ------------- | ------ | -------------- | ------ |
| `Id`          | string | 是              | 素材组 ID |
| `ProjectName` | string | 否，默认 `default` | 火山项目名  |


**响应**（无业务返回参数）：

```json
{
  "ResponseMetadata": {"Action": "DeleteAssetGroup", "...": "..."},
  "Result": {}
}
```

---



## 四、真人素材库（Real-human Portrait Library）



### 4.1 与虚拟素材库的关系

真人素材库（`GroupType = LivenessFace`）跟第三节的虚拟素材库（`GroupType = AIGC`）**共用同一套 Asset/AssetGroup Action**（`CreateAsset`、`ListAssetGroups`、`ListAssets`、`GetAsset`、`GetAssetGroup`、`UpdateAsset`、`UpdateAssetGroup`、`DeleteAsset`、`DeleteAssetGroup`），调用方式、请求体、响应体跟第三节完全一样，只是 `Filter.GroupType`/查询结果里的 `GroupType` 变成 `LivenessFace`。

**唯一的区别**：真人素材组不能通过 `/ark/seedance/v3?Action=CreateAssetGroup` 创建——这个接口在本平台被**强制改写成** `AIGC`（见 3.2 节），所以真人素材组只能走下面这套专门的「真人核验」两步流程来创建。核验通过后拿到的 `GroupId`，后续增删改查（3.3–3.11 节里的所有 Action）跟虚拟素材组一模一样地使用。

> 官方原始的 `CreateVisualValidateSession`/`GetVisualValidateResult` 两个 Action（AK/SK 签名）本平台**没有**直接镜像，而是包装成了下面两个平台专属接口（Bearer 鉴权，请求/响应体也不是官方原始形状）。如果你熟悉官方 SDK 的调用方式，这里请注意形状不一样，不能直接照抄官方示例代码。



### 4.2 CreateVisualValidateSession（平台专属，非官方镜像）

```
POST https://api.gravitex.ai/v1/visual-validate/session
Authorization: Bearer sk-your_token_key
Content-Type: application/json
```


| 字段            | 类型      | 必填  | 说明                   |
| ------------- | ------- | --- | -------------------- |
| `channel_id`  | integer | 否   | 指定使用哪个渠道发起核验，不传则自动选择 |
| `name`        | string  | 是   | 核验通过后创建的素材组名称        |
| `description` | string  | 否   | 素材组描述                |


**响应**：

```json
{
  "h5_link": "https://www.byteplus.com/en/liveness-face-manage/authorization?pl=...",
  "state": "eyJhbGciOi...(平台签名的不透明 token，回调时原样带回)",
  "channel_id": 12,
  "byted_token": "202607...",
  "expires_in": 1800
}
```

把 `h5_link` 展示给终端用户（建议用 WebView/浏览器打开），用户完成真人核验后会跳转到本平台的回调页 `GET /asset-validate-callback.html`，由这个页面解析官方 H5 回调参数、拿着 `state`/`byted_token` 调用下面的 4.3 接口。

> `state` 是平台自己签名的一次性凭证，携带了发起核验时的用户 ID、渠道 ID、素材组名称/描述等上下文，**不是**官方文档里的 `bytedToken`，两者不能混用；`byted_token`（下划线）才是需要透传给官方接口的官方凭证，30 分钟内有效，只能核验一次。



### 4.3 SubmitVisualValidateResult（平台专属，非官方镜像）

```
POST https://api.gravitex.ai/v1/visual-validate/result
Content-Type: application/json
```

> 这个接口是**匿名**的（不需要 `Authorization`），因为它是从官方 H5 回调页触发调用的，此时终端用户手上没有平台的 Bearer token——身份信息全部由 `state` 里签名的内容来恢复。


| 字段            | 类型     | 必填  | 说明                     |
| ------------- | ------ | --- | ---------------------- |
| `state`       | string | 是   | 4.2 响应里的 `state`       |
| `byted_token` | string | 是   | 4.2 响应里的 `byted_token` |


**响应**：

```json
{
  "group_id": "group-20260710-face01",
  "name": "用户A的真人形象",
  "channel_id": 12,
  "group_type": "LivenessFace"
}
```

拿到 `group_id` 之后，就可以像操作 `AIGC` 素材组一样，用第三节的 `CreateAsset`/`ListAssets`/`GetAsset`/... 往这个组里传素材、查询、更新、删除，唯一区别是 `GroupId` 传的是这里返回的真人素材组 ID。

**幂等与冲突处理**：

- 如果同一次官方核验对应的 `GroupId` 本地已经存在（比如回调重复触发），会直接返回已存在的记录，响应里多带一个 `"reused": true` 字段。
- 如果这个 `GroupId` 已经绑定给了另一个用户，会返回 `409 Conflict`。



### 4.4 真人素材的额外限制

- **配额独立**：每个用户在每个渠道下的真人素材组数量单独限额（跟 `AIGC` 素材组配额分开计算），具体额度以平台后台配置为准。
- **人脸匹配**：每个真人素材组对应唯一一个真实人物；往组里上传素材时，官方会跟核验时留存的参考人脸比对，人物不匹配或图片中出现多张人脸都会导致素材上传失败（`Status` 变成 `Failed`，`Error` 里带具体原因）。
- **删除限制**：见 3.11 节——只有授权过期或被拒绝的真人素材组才能删除。
- 素材的引用方式（`asset://<ID>`、prompt 里用"图片 N"/"视频 N"）、素材类型限制（3.3 节的图片/视频/音频格式与尺寸要求）跟虚拟素材库完全一致，不再重复。

---



## 五、常见问题

**Q: 这套接口跟平台自己的** `/v1/video/generations`**、**`/v1/assets` **是什么关系？**
A: 完全独立、互不影响的两套接口。素材库这块数据是互通的——用官方形状 `CreateAsset` 建的素材，能在 `/v1/assets` 里查到，反之亦然。视频生成这块目前是分开的两个路由，互不干扰。

**Q: 为什么素材库鉴权用 Bearer token，官方文档写的是 AK/SK 签名？**
A: 平台目前只有 Bearer token 一套用户体系，没有给每个用户单独发 AK/SK、也没有实现火山的 HMAC-SHA256 签名校验。这是本期明确的范围限定——只镜像请求/响应体的字段形状，不镜像签名协议。

**Q:** `model` **字段能不能直接抄官方文档示例里的模型 ID（比如** `dreamina-seedance-2-0-260128`**）？**
A: 不一定。平台渠道注册的模型名可能跟官方示例不完全一致（渠道用的是 `seedance-2-0`、`doubao-seedance-2-0-260128` 等）。如果传的 `model` 字符串没有被任何渠道注册，会报"无可用渠道"错误。建议先确认平台后台已配置的模型名。

**Q: 取消一个** `running` **状态的任务会怎样？**
A: 上游会拒绝，返回错误状态码和错误体（原样透传，不做二次包装）。具体的错误码格式以实际请求为准，官方文档没有详细说明这个错误的具体格式。

**Q: 素材库的** `SortBy`**/**`SortOrder` **等边缘字段能用吗？**
A: 能。这套接口的请求体是原样转发给火山的，不会因为平台内部结构体没有对应字段就被丢弃。

**Q: 真人素材组能不能像虚拟素材组一样直接用** `CreateAssetGroup` **创建？**
A: 不能。`CreateAssetGroup` 在本平台被强制改写成 `AIGC`（见 3.2 节），真人素材组只能走 4.2/4.3 节的专属核验流程创建，创建之后的增删改查才复用第三节的接口。