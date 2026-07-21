# Seedance 2.0 官方 API 镜像端点 — 开发设计文档

> 状态：待评审
> 关联文档：`docs/byteplus/seedance2.0/`（官方文档扫描件）、`docs/byteplus/seedance2.0/Seedance 2.0 视频生成 API.md`（平台现有适配文档）
> 影响范围：新增为主，仅对 2 个既有文件做 flag 门控的旁路分支

## 一、背景与目标

平台现有的 `/v1/video/generations`、`/v1/asset-groups`、`/v1/assets` 等接口是**火山方舟（BytePlus Ark）Seedance 2.0** 能力的平台化简化封装（字段名、响应结构都是平台自己的形状）。

本次需求：在**不改动**上述现有接口任何行为的前提下，新增一组与火山官方 API **请求体、响应体完全一致**的镜像端点，唯一的区别是把官方 `baseURL` 换成平台自己的域名。用户可以用现成的火山官方 SDK / 官方文档里的 curl 示例，只改 `base_url`，就能直接打到平台上。

覆盖两块：

1. **视频生成任务**：提交 / 查询 / 取消，镜像 `POST|GET|DELETE https://ark.../api/v3/contents/generations/tasks[/{id}]`
2. **素材库**（虚拟 + 真人两类共用同一套接口）：CreateAssetGroup / CreateAsset / ListAssetGroups / ListAssets / GetAsset / GetAssetGroup / UpdateAsset / UpdateAssetGroup / DeleteAsset / DeleteAssetGroup，镜像 `POST https://ark.../?Action=X&Version=2024-01-01`

**明确不做**（已与用户确认排除在本期范围外）：

- 素材库官方鉴权协议（Volcengine AK/SK SigV4 签名校验）——本期仅镜像请求/响应体形状，鉴权仍走平台自己的 `Bearer sk-xxx`。
- 真人核验（`CreateVisualValidateSession`/`GetVisualValidateResult`）的官方形状镜像——现有 H5 回调机制（`/v1/visual-validate/session`、`/v1/visual-validate/result`）保持不变，不重复实现。
- 审核拦截原因查询（`GetModerationResult`）的官方形状镜像——这是平台自研的白名单能力，官方没有对应的公开 REST 端点。
- 取消任务（DELETE）不反向补充到现有 `/v1/video/generations/:task_id`，仅在新的官方镜像路由上生效。

### 新增端点一览（Base URL 暂定 `https://api.gravitex.ai`）

**视频生成任务（3 个）**

| 方法 | 完整 URL | 说明 |
|---|---|---|
| POST | `https://api.gravitex.ai/api/v3/contents/generations/tasks` | 创建视频生成任务 |
| GET | `https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}` | 查询任务 |
| DELETE | `https://api.gravitex.ai/api/v3/contents/generations/tasks/{id}` | 取消/删除任务 |

**素材库（10 个，同一路径 + 不同 `Action`）**

| 方法 | 完整 URL |
|---|---|
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=CreateAssetGroup&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=CreateAsset&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=ListAssetGroups&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=ListAssets&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=GetAsset&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=GetAssetGroup&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=UpdateAsset&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=UpdateAssetGroup&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=DeleteAsset&Version=2024-01-01` |
| POST | `https://api.gravitex.ai/ark/seedance/v3?Action=DeleteAssetGroup&Version=2024-01-01` |

> `https://api.gravitex.ai` 是先占位的域名，若正式上线时域名不同，只是替换这一处，不影响路由本身的实现。

## 二、现状梳理（设计依据）

- 视频生成的真实上游适配器是 `relay/channel/task/doubao/adaptor.go`（`ChannelTypeDoubaoVideo = 54` / `ChannelTypeVolcEngine = 45` 共用），已经在直连官方端点 `{baseURL}/api/v3/contents/generations/tasks`，Bearer 鉴权。`relay/channel/task/seedancegateway/`（`ChannelTypeSeedanceGateway = 62`）是另一个第三方聚合渠道，跟本次镜像无关，不涉及。
- 平台已有"官方形状请求 + 复用内部转发管线"的先例：`middleware/kling_adapter.go`、`middleware/jimeng_adapter.go` 把 Kling / 即梦官方请求体改写为平台内部形状、把 URL path 改写成 `/v1/video/generations[...]`，让既有的 `middleware.Distribute()`（渠道选型/限流/配额预检）原样生效，再交给 `controller.RelayTask` / `RelayTaskFetch`。即梦渠道还展示了"单端点 + `?Action=` 查询参数"的路由方式。
- 但已验证：Kling/即梦这条路径最终返回给客户端的仍是**平台归一化的 `OpenAIVideo` 结构**（`dto.NewOpenAIVideo()`），不是对应厂商的官方响应形状——这条先例只解决了"官方请求 → 内部路由"，没有解决"响应体与官方一致"。
- 更关键的一点：内部请求结构体 `relay/common/relay_info.go:715 TaskSubmitReq` 对未知顶层字段是**静默丢弃**的（`json.Unmarshal` 到具名字段的标准行为），只有客户端显式把内容包在 `"metadata"` 键里才会被保留。而官方请求体的 `execution_expires_after` / `service_tier` / `safety_identifier` / `frames` 等字段都是顶层字段，不是包在 `metadata` 里——如果照搬 Kling/即梦的路数（转换成 `TaskSubmitReq` 再转发），这些字段会被平台悄悄吞掉，达不到"请求体一模一样"的要求。
- 计费维度目前靠 `c.Set("has_video_input", ...)` / `c.Set("video_resolution", ...)`（`relay/channel/task/doubao/adaptor.go:212,216`）传递给下游计费逻辑（`relay/relay_task.go`、`controller/task_video.go`、`service/task_billing.go` 都读这两个 key）。
- 素材库当前是纯 AK/SK 签名对接火山（`service/byteplus_asset.go`，`byteplus-go-sdk-v2` 的 `universal.DoCall`），跟视频生成的 Bearer 鉴权是两套完全独立的协议；底层 `byteplusCall(cfg, action, body)`（package-private）本身就是"任意 Action + 任意 map body"的通用调用，10 个现有 `ByteplusXxx` 包装函数只是收窄过的具名参数版本（详见四、Part B 的 `ByteplusRawAction` 设计）。
- `model/user_asset.go` / `user_asset_group.go` 里落库的 `VirtualId` / `GroupId` 就是火山返回的真实 ID（`asset-xxx` / `group-xxx`），没有内部 ID 转换层；视频任务同理，`doubao/adaptor.go:260` 明确把上游真实任务 ID 当作平台对外的 `PublicTaskID`（`info.PublicTaskID = dResp.ID`）。这意味着官方镜像端点不需要额外的 ID 映射逻辑。
- 任务取消（DELETE）在平台目前**完全没有实现**——`TaskAdaptor` 接口（`relay/channel/adapter.go`）没有 Cancel/Delete 方法，所有现役渠道适配器都没实现。

## 三、Part A：视频生成官方镜像

### 3.1 新增路由

```
POST   /api/v3/contents/generations/tasks        创建任务
GET    /api/v3/contents/generations/tasks/:id     查询任务
DELETE /api/v3/contents/generations/tasks/:id     取消/删除任务
```

独立注册一个新的 `router.Group("/api/v3/contents/generations")`（不复用 `router/api-router.go` 里那个面向 dashboard 的 `/api` group，避免继承 gzip/CORS/GlobalAPIRateLimit 等不相关中间件），中间件链：

```go
seedanceOfficialRouter := router.Group("/api/v3/contents/generations")
seedanceOfficialRouter.Use(middleware.RouteTag("relay"))
seedanceOfficialRouter.Use(middleware.SeedanceOfficialMirror(), middleware.TokenAuth(), middleware.AssetResolveChannel(), middleware.Distribute())
{
    seedanceOfficialRouter.POST("/tasks", controller.RelayTask)
    seedanceOfficialRouter.GET("/tasks/:id", controller.RelayTaskFetch)
    seedanceOfficialRouter.DELETE("/tasks/:id", controller.RelayTaskCancel) // 新增
}
```

### 3.2 新中间件 `middleware.SeedanceOfficialMirror()`（新文件）

职责类似 `KlingRequestConvert`/`JimengRequestConvert`，但**不改写 body 内容**（这是与 Kling/即梦最大的不同点）：

- POST：把 `c.Request.URL.Path` 改写为 `/v1/video/generations`，让 `Distribute()` 的既有模型解析逻辑原样生效（零改动 `distributor.go`）；同时 `c.Set("seedance_raw_mirror", true)`。请求体本身**不做任何转换**，原始字节直接透传下去（body 本身官方请求就带 `model` 顶层字段，`Distribute()` 已有的通用模型抽取逻辑可以直接读到）。
- GET：`c.Set("task_id", c.Param("id"))`（沿用即梦那种"塞进 context 供 fallback 读取"的方式，因为官方路径参数名是 `id` 不是 `task_id`），路径改写为 `/v1/video/generations/` 拼上 id；`c.Set("seedance_raw_mirror", true)`。
- DELETE：`c.Set("seedance_raw_mirror", true)`，直接放行给专门的取消 handler（不需要路径改写，因为这是全新路由，不复用 RelayTaskFetch/RelayTask）。

### 3.3 请求体保真：`doubao/adaptor.go` 的旁路分支

`BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo)` 本身就带 `*gin.Context` 参数，门控分支直接读 `c.GetBool("seedance_raw_mirror")` 判断，不新增 `RelayInfo` 字段、不改 `relay/common/relay_info.go`：

- 已核实确切机制：调用 `common.GetBodyStorage(c)` 拿到缓存的 `BodyStorage`（`controller/relay.go:585` 现有代码已经这么用），再 `.Bytes()` 取出原始字节——这是平台既有的"读一次、缓存、到处复用"机制，**不是**待定方案。前提是新的镜像中间件（3.2 节）不像 Kling/即梦中间件那样重写 body（`KlingRequestConvert`/`JimengRequestConvert` 会把转换后的 JSON 写回 `KeyRequestBody`，导致后续读到的是转换后而非原始字节）——镜像中间件按设计只改 `URL.Path`，不碰 body，所以 `BodyStorage` 里缓存的就是客户端原始字节。**不经过 `relaycommon.GetTaskRequest`/`TaskSubmitReq` 重建**，原样作为上游请求体转发。
- 仍然轻量解析一次原始 JSON（不反序列化进具名结构体，只探测两个字段）：`resolution` 字符串、`content[]` 里是否存在 `type == "video_url"` 的项，分别 `c.Set("video_resolution", ...)` / `c.Set("has_video_input", ...)`——这两个 key 是计费逻辑已经在读的，保证走镜像端点的任务计费口径与走 `/v1/video/generations` 一致。
- 资产归属校验（`extractAssetVirtualIds` + `model.CheckUserOwnsAssets`）逻辑保留，因为镜像端点同样可能引用 `asset://` 素材。
- 非镜像路径（现有 `/v1/video/generations` 等）完全走原逻辑，不受影响。

### 3.4 响应体保真

- **创建（POST）成功路径**：`DoResponse` 里加一个分支，`seedance_raw_mirror` 为真时直接 `c.Data(http.StatusOK, "application/json", responseBody)` 把上游原始响应体透传给客户端，而不是构造 `dto.NewOpenAIVideo()`。本地任务落库（`model.Task` 记录，供后续 GET/DELETE 使用）逻辑不变——`taskData`（即 `responseBody`）照常存。
- **创建（POST）非 200 错误路径 —— 这是一个已核实的真实缺口，原方案没覆盖**：上游返回非 200 时，`doubao.TaskAdaptor.DoResponse` 根本不会被调用——`relay/relay_task.go` 里 `RelayTaskSubmit`（约第 271-274 行）会在调用 `adaptor.DoResponse` **之前**先检查 `resp.StatusCode != http.StatusOK`，直接把上游原始响应体包进平台自己的 `service.TaskErrorWrapper(...)`（`dto.TaskError{Code:"fail_to_fetch_task",...}`），随后 `controller/relay.go` 的 `respondTaskError`（约第 689-694 行）用 `c.JSON(taskErr.StatusCode, taskErr)` 写出的是**平台的错误壳**，不是上游原始错误体——如果不管这一段，镜像端点在官方报错（内容审核拦截、参数错误、限流等）时返回给客户端的就不是"官方一模一样"的错误体，与本次需求的核心目标直接冲突。
  修复需要三处配合（都是 flag 门控的旁路分支，不改变非镜像路由的行为）：
  1. `relay/relay_task.go` 第 271-274 行：`seedance_raw_mirror` 为真时，不调用 `TaskErrorWrapper`，而是把 `resp.StatusCode` 和已读出的 `responseBody` 原样存到 `dto.TaskError` 的一个新增字段（如 `RawBody []byte`），或者用 `c.Set(...)` 挂到 gin context 上。
  2. `controller/relay.go` 的 `respondTaskError`：`seedance_raw_mirror` 为真且该 `TaskError` 带有原始 body 时，`c.Data(taskErr.StatusCode, "application/json", rawBody)` 而不是 `c.JSON(taskErr.StatusCode, taskErr)`。
  3. 需要确认 `shouldRetryTaskRelay`（同文件约第 696 行）对这类错误的重试判断逻辑不受影响——重试到另一个渠道是既有行为，镜像端点不应该改变"要不要重试"，只应该改变"最终报错时返回给客户端的 body 形状"。
- **查询（GET）**：`relay/relay_task.go` 加门控分支，具体改法：
  - `tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte`（目前只有一处调用，`videoFetchByIDRespBodyBuilder` 第 442 行）新增第三个参数 `rawMirror bool`，在现有第 552-557 行、604-614 行"`OpenAIVideoConverter` 渠道返回 nil、交给外层 `ConvertToOpenAIVideo`"的分支之前插入：`if rawMirror { return body }`（`body` 是本函数已经读到的原始上游响应字节），跳过后续归一化逻辑。这是一个新增形参，唯一调用点同步更新，不影响其它渠道的调用行为（因为 `rawMirror` 默认传 `false`）。
  - `videoFetchByIDRespBodyBuilder` 里任务已是终态（缓存命中，不需要实时刷新）的分支，`seedance_raw_mirror` 为真时直接 `respBody = originTask.Data`（合并保存的原始上游 JSON），跳过 `converter.ConvertToOpenAIVideo(originTask)` 调用。
  - 两处改动都严格 flag 门控，对 Kling/即梦/doubao 现有调用路径零影响。
- **取消（DELETE）**：全新 handler `controller.RelayTaskCancel`（新文件或加到 `controller/relay.go` 旁边的新文件），流程：
  1. 按 `user_id + task_id` 查本地 `model.Task`（复用现有查询函数），校验归属。
  2. 取该任务绑定的 channel，取适配器实例，`type assert` 到新增的可选接口：
     ```go
     type TaskCancelAdaptor interface {
         CancelTask(baseUrl, key, taskID, proxy string) (*http.Response, error)
     }
     ```
     只有 `doubao.TaskAdaptor` 实现它（`DELETE {baseURL}/api/v3/contents/generations/tasks/{id}`，Bearer 头），不改动共享的 `TaskAdaptor` 接口定义，不影响其它渠道适配器。
  3. 上游返回后按官方文档语义更新本地任务状态（`queued`→`cancelled`；`succeeded`/`failed`/`expired`→本地记录标记删除/不可再查；`running` 时上游会返回错误，原样透传错误体）。
  4. 响应体：官方文档写明"无响应参数"，原样透传上游返回的空/或 `{}` 响应体。

### 3.5 影响范围小结（Part A）

| 文件 | 改动类型 | 说明 |
|---|---|---|
| `middleware/seedance_official_mirror.go` | 新增 | 路径/参数改写 + flag 设置，不碰请求体内容 |
| `router/video-router.go` | 新增路由注册 | 只加新 group，不改现有 group |
| `relay/channel/task/doubao/adaptor.go` | 旁路分支 | `BuildRequestBody`/`DoResponse` 各加一个 flag 门控 if 分支；新增 `CancelTask` 方法（新能力，不影响现有方法） |
| `relay/relay_task.go` | 旁路分支 | `videoFetchByIDRespBodyBuilder`/`tryRealtimeFetch` 各加一个 flag 门控分支；`RelayTaskSubmit` 里非 200 状态码判断处新增 flag 门控分支（保留原始 body/status，见 3.4 节） |
| `controller/relay.go` | 旁路分支 | `respondTaskError` 加 flag 门控分支，命中时原样透传原始错误体而不是 `dto.TaskError` 壳 |
| `controller/seedance_official_video.go`（新文件） | 新增 | `RelayTaskCancel` handler |
| `relay/common/relay_info.go` | **不改动** | flag 直接通过 `c.GetBool("seedance_raw_mirror")` 读取，不新增字段 |

## 四、Part B：素材库官方镜像

### 4.1 路由

单端点 + `Action` 查询参数，与官方完全一致的调用形状（对齐即梦渠道已用过的模式）：

```
POST /ark/seedance/v3?Action=CreateAssetGroup&Version=2024-01-01
POST /ark/seedance/v3?Action=CreateAsset&Version=2024-01-01
POST /ark/seedance/v3?Action=ListAssetGroups&Version=2024-01-01
POST /ark/seedance/v3?Action=ListAssets&Version=2024-01-01
POST /ark/seedance/v3?Action=GetAsset&Version=2024-01-01
POST /ark/seedance/v3?Action=GetAssetGroup&Version=2024-01-01
POST /ark/seedance/v3?Action=UpdateAsset&Version=2024-01-01
POST /ark/seedance/v3?Action=UpdateAssetGroup&Version=2024-01-01
POST /ark/seedance/v3?Action=DeleteAsset&Version=2024-01-01
POST /ark/seedance/v3?Action=DeleteAssetGroup&Version=2024-01-01
```

鉴权：`Authorization: Bearer sk-xxx`（平台 token，`TokenAuth()` 中间件），**不是**真实 AK/SK，也不做 Volcengine SigV4 签名校验——已与用户确认此为本期范围。

```go
assetOfficialRouter := router.Group("/ark/seedance/v3")
assetOfficialRouter.Use(middleware.RouteTag("relay"))
assetOfficialRouter.Use(middleware.TokenAuth())
{
    assetOfficialRouter.POST("", controller.SeedanceOfficialAssetDispatch)
}
```

### 4.2 新 controller（新文件 `controller/seedance_official_asset.go`）

单一入口函数按 `c.Query("Action")` 分发到内部小函数。**已与用户确认采用高保真方案**：新增一个导出函数

```go
// ByteplusRawAction 透传任意 Action + 请求体到火山 Ark，返回未加工的原始响应
// map（含真实 ResponseMetadata），供需要官方原始响应形状的调用方使用。
func ByteplusRawAction(cfg ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
    resp, err := byteplusCall(cfg, action, body) // 复用现有私有函数，不改动其行为
    if err != nil {
        return nil, err
    }
    if resp == nil {
        return nil, fmt.Errorf("nil response")
    }
    return *resp, nil
}
```

放在 `service/byteplus_asset.go` 里（唯一对该文件的改动，仅新增，不修改任何现有函数）。10 个 Action 里的**读操作**（`ListAssets`/`ListAssetGroups`/`GetAsset`/`GetAssetGroup`）和**部分写操作的官方调用部分**统一走：解析客户端官方形状 JSON body → 转成 `map[string]interface{}` 原样透传 → `ByteplusRawAction(cfg, action, body)` → 响应 map 原样透传给客户端（响应本身已经是火山原始的 `{ResponseMetadata, Result}` 形状，不需要手工拼装）。

**⚠️ 关键约束（已核实，不是可选项）：镜像 controller 不能是"哑代理"，必须同步写本地表。**

`controller/asset.go:484` 的 `CreateAsset`、`controller/asset_group.go:176` 的 `CreateAssetGroup` 在调用完 `ByteplusCreateAsset`/`ByteplusCreateAssetGroup` 拿到火山真实 ID 后，都会 `model.InsertUserAsset(...)` / `model.InsertUserAssetGroup(...)` 落一条本地记录。这条本地记录是后续这些能力的唯一依据：

- `model.CheckUserOwnsAssets`（`relay/channel/task/doubao/adaptor.go:195`，视频生成时校验 `asset://` 归属）
- 现有 `/v1/assets`、`/v1/asset-groups` 的列表/查询/删除
- 素材状态轮询（`active`/`pending`/`failed`）

如果镜像端点的 `CreateAsset`/`CreateAssetGroup` 只是转发请求、拿到响应就直接回给客户端而不写本地表，那么**通过镜像端点创建的素材，无法在视频生成时以 `asset://` 引用**（会被 `CheckUserOwnsAssets` 判定为不属于当前用户而拒绝），也不会出现在 `/v1/assets` 里——这会直接违背"素材库镜像出来的东西要和现有功能打通"的预期。所以镜像 controller 里 `CreateAsset`/`CreateAssetGroup`/`DeleteAsset`/`DeleteAssetGroup`/`UpdateAsset`/`UpdateAssetGroup` 六个写操作，除了转发官方形状的请求/响应，还要各自复刻一份对应的本地表写入/更新/删除逻辑（读操作 `ListAssets`/`ListAssetGroups`/`GetAsset`/`GetAssetGroup` 不需要，纯查询转发即可）。

**响应形状**：由于 `ByteplusRawAction` 直接返回火山 SDK 的原始响应 map，正常情况下**不需要**再手工拼装 `{ResponseMetadata, Result}` 信封——它本来就是这个形状（真实的 `RequestId`/`Region`/`Action`/`Version` 都是火山自己回填的，不是我们伪造的），控制器只需把这个 map 原样 `c.JSON(200, resp)` 出去。错误响应同理：`byteplusCall` 失败时返回的 Go `error` 目前是拼接过的字符串（`fmt.Errorf("byteplus %s: %w", action, err)`），如果需要在错误路径也保留官方原始的 `{ResponseMetadata:{Error:{Code,Message}}}` 形状，控制器这层需要在调用 `ByteplusRawAction` 出错时，尝试从 SDK 返回的原始 error（`bytepluserr` 包，`service/byteplus_asset.go` 已引入）里取出结构化的 Code/Message 再拼一层官方形状的错误信封，而不是直接把 Go error 字符串包出去——这是新 controller 内部的实现细节，不影响平台其它接口的错误格式。

| Action | 调用方式 | 本地表同步（写操作才需要） |
|---|---|---|
| `CreateAssetGroup` | `ByteplusRawAction`，`GroupType` 固定视作 `aigc`（与现有 `/v1/asset-groups` 一致，`liveness_face` 仍只能走 H5 流程） | 从响应 `Result.Id` 取 `group_id` → `model.InsertUserAssetGroup(...)` |
| `CreateAsset` | `ByteplusRawAction`，body 原样透传（`GroupId`/`URL`/`AssetType`/`Name`/`Moderation`/`ProjectName` 等） | 从响应 `Result.Id` 取 `virtual_id` → `model.InsertUserAsset(...)` |
| `ListAssetGroups` | `ByteplusRawAction`，body（含 `Filter`/`PageNumber`/`PageSize`/`SortBy`/`SortOrder`）原样透传 | 无 |
| `ListAssets` | `ByteplusRawAction`，body 原样透传 | 无 |
| `GetAsset` | `ByteplusRawAction` | 无 |
| `GetAssetGroup` | `ByteplusRawAction`（火山确实有 `GetAssetGroup` 这个 Action，直接透传即可，不需要再用 `ListAssetGroups` 模拟——之前版本以为没有底层调用是看漏了，`ByteplusRawAction` 是通用调用，任何 Action 字符串都能传） | 无 |
| `UpdateAsset` | `ByteplusRawAction` | 按 `Id` 更新本地 `UserAsset` 的对应字段（如 `Name`） |
| `UpdateAssetGroup` | `ByteplusRawAction` | 按 `Id` 更新本地 `UserAssetGroup` 的对应字段 |
| `DeleteAsset` | `ByteplusRawAction` | 按 `Id` 删除本地 `UserAsset` 记录 |
| `DeleteAssetGroup` | `ByteplusRawAction` | 按 `Id` 删除本地 `UserAssetGroup` 记录（含级联，与现有 `/v1/asset-groups` DELETE 行为一致） |

ID 直接透传（`group-xxx`/`asset-xxx` 本来就是火山真实 ID，本地表 `UserAssetGroup.GroupId`/`UserAsset.VirtualId` 存的也是同一个值，无需转换层）。

用户/渠道解析：与现有 `/v1/asset-groups`、`/v1/assets` 一致的方式——从 token 关联的用户下已配置 AK/SK 的 BytePlus 渠道里选取（不引入新的渠道选择逻辑）。

### 4.3 影响范围小结（Part B）

| 文件 | 改动类型 | 说明 |
|---|---|---|
| `controller/seedance_official_asset.go` | 新增 | Action 分发 + body 原样透传给 `ByteplusRawAction` + 6 个写操作各自的本地表同步（`InsertUserAsset`/`InsertUserAssetGroup`/删除/更新，见上文关键约束） |
| `router/video-router.go`（或新 router 文件） | 新增路由注册 | 一个新 POST 路由 |
| `service/byteplus_asset.go` | 新增一个导出函数 | **已确认采用**：新增 `ByteplusRawAction`（包一层现有私有 `byteplusCall`），换取完整字段保真度（`SortBy`/`SortOrder`/按次 `ProjectName` 等边缘字段不再被忽略）；不修改任何现有函数 |
| `controller/asset.go` / `asset_group.go` | **不改动** | 现有 `/v1/assets`、`/v1/asset-groups` 行为不变，本地表结构/字段也不变 |

## 五、测试计划

- **视频生成镜像**：
  - 用官方文档里的原始 curl 示例（文生视频/图生视频/首尾帧/多模态参考）直接打新端点，只替换 `Authorization` 和 host，校验请求体不用做任何调整。
  - 对比：同一个请求分别打 `/v1/video/generations`（现有）和 `/api/v3/contents/generations/tasks`（新），确认两者请求上游的实际 body 一致（新端点是原样转发，现有端点走 `TaskSubmitReq` 重建），下游计费维度（`has_video_input`/`video_resolution`）一致。
  - 查询：验证 `queued`/`running`/`succeeded`/`failed` 各状态下响应体字段与官方文档一致（`content.video_url` 嵌套、无 `progress` 字段等）。
  - 取消：`queued` 状态下可取消；`running` 状态下按官方语义应报错（不可取消）；`succeeded`/`failed`/`expired` 状态下删除记录。
  - 回归：确认 `/v1/video/generations`、`/kling/v1/...`、`jimeng/...` 现有路由行为完全不变（跑现有测试 + 手工抽查）。
- **素材库镜像**：
  - 十个 Action 各跑一次正常路径 + 一次错误路径（如 `GetAsset` 传不存在的 `Id`），校验响应包裹结构、字段大小写、错误体形状。
  - 确认新端点产生的数据（`UserAssetGroup`/`UserAsset` 记录）与走 `/v1/assets` 产生的数据完全互通——同一个 `group_id` 可以一边用官方形状加素材，一边用平台 `/v1/assets` 查看，反之亦然。
  - 回归：`/v1/asset-groups`、`/v1/assets`、`/v1/visual-validate/*` 现有行为不变。

## 六、计费与日志：镜像端点不影响现有链路

`tryRealtimeFetch`（`relay/relay_task.go`）里，上游状态变为 `succeeded`/`failed` 时会调用 `CompleteVideoTaskOnUpstreamSuccessFn`（实现在 `controller/task_video.go:1050` 的 `CompleteVideoTaskOnUpstreamSuccess`），quota 扣费（`handleVideoPerSecondBilling`/`handleVideoTokenRatioBilling`）和消费记录（日志）都在这一步落地，且**先于**任何响应体格式判断。

第 3.4 节里 `rawMirror` 的早退分支（`if rawMirror { return body }`）必须插在 `CompleteVideoTaskOnUpstreamSuccessFn` 调用**之后**（即现有第 552-557 行、604-614 行"交给 `ConvertToOpenAIVideo`"的位置，而不是更早的billing 逻辑之前）。这样一来，镜像端点的轮询只改变返回给客户端的字节，计费扣费和消费日志走的是与 `/v1/video/generations/:task_id` 完全相同的一条路径，互不影响。实现时需要在这两处分支旁加注释强调这个顺序约束，避免后续维护时误挪。

## 七、风险与待确认事项

- ~~`BuildRequestBody` 里"原始字节透传"需要确认框架里请求体是否已被消费~~ **已核实解决**：`common.GetBodyStorage(c)` 是平台既有机制（`controller/relay.go:585` 已在用），镜像中间件只要不像 Kling/即梦那样重写 body，`BodyStorage` 里就是客户端原始字节，无需新增缓存逻辑（详见 3.3 节）。
- `DELETE` 取消动作里，官方对 `running` 状态任务返回错误（不可取消）——已尝试查证火山对这种情况返回的具体 HTTP 状态码/错误体，但官方文档站是 JS 渲染的 SPA，本 session 里可用的抓取工具拿不到实际渲染内容，本地已扫描的文档也没有这部分细节，暂未查实。不阻塞设计：实现时原样透传上游返回的状态码/错误体即可，不需要平台自己判断拦截（上游本身就是 gatekeeper），留到联调阶段用真实请求验证具体形状。
- **视频生成非 200 错误响应的原样透传是本轮复核新发现的缺口**（详见 3.4 节新增内容）：原方案只处理了成功响应，遗漏了 `relay/relay_task.go` 里"上游非 200 直接被包成平台 `TaskError`，根本不会走到 adaptor 的 `DoResponse`"这一层拦截。已补充到 3.4/3.5 节的设计里，需要 `relay/relay_task.go` + `controller/relay.go` 两处配合，而不是最初以为的只改 `doubao/adaptor.go` 就够。
- ~~素材库镜像请求/响应保真度取舍~~ **已确认解决**：采用新增 `ByteplusRawAction` 方案（详见 4.2/4.3 节），`SortBy`/`SortOrder`/按次 `ProjectName` 等字段不再被忽略；`GetAssetGroup` 也直接透传真实的官方 `Action=GetAssetGroup`，不再需要用 `ListAssetGroups` 模拟。
- **素材库镜像端点"必须同步写本地表"是本轮复核新发现的硬约束**（详见 4.2 节）：如果只做请求/响应转发而不复刻 `InsertUserAsset`/`InsertUserAssetGroup` 等本地持久化，镜像端点创建的素材会在视频生成的 `asset://` 归属校验（`CheckUserOwnsAssets`）中被判定为不存在，也不会出现在现有 `/v1/assets` 列表里——破坏"两套端点数据互通"的预期。这不是一个可以往后拖的优化项，是六个写操作（Create/Delete/Update × Asset/AssetGroup）都必须做的事。
- **本地 OSS 暂存行为会保留、不算新增风险**：`controller/asset.go` 现有的 `CreateAsset` 在调用 `ByteplusCreateAsset` 之前，若 `service.IsAssetOSSStagingEnabled()` 开启，会先把客户端传入的 `url` 转存到平台自己的 OSS，再把 OSS 地址传给火山（`service.UploadAssetURLToOSS`）——也就是说实际打到火山的 `URL` 字段值不一定和客户端传入的完全一样。这与"素材库仅镜像形状、不镜像鉴权协议"的既定范围一致（`URL` 字段仍是合法的公网 URL，形状不变），镜像 controller 按现有行为原样复用即可，不用特殊处理，只是提前说明避免实现时误以为是 bug。
- **本地任务记录的保留期可能短于官方 7 天窗口**：官方文档说明视频生成任务 ID 只保留 7 天，超时自动删除；如果平台自己的 `model.Task` 表有更短的清理/归档周期，镜像端点的 GET/DELETE 在官方窗口内、但平台本地记录已被清理的情况下会查不到本地任务而报错——这是一个现有系统的边界行为，不是镜像功能引入的新问题，但需要在测试计划里补一条"本地记录已过期但官方任务仍存在"的用例，实现前确认平台当前的任务表保留策略。
- **模型 ID 命名差异只是文档说明问题，不是代码缺口**：官方示例里出现的模型 ID（如 `seedance-2-0-260128`、`dreamina-seedance-2-0-260128`）与平台渠道当前注册的模型名（`doubao-seedance-2-0-260128`、`seedance-2-0` 等，见 `relay/channel/task/doubao/constants.go`）不完全一致。用户如果直接照抄官方文档示例里的 `model` 字段值，若该字符串没有被任何渠道注册，会被 `Distribute()` 判定为"无可用渠道"而报错——这是预期内的正确行为，只需要在最终面向用户的文档里注明"请使用平台已注册的模型名"，避免联调时被误判为 bug。
