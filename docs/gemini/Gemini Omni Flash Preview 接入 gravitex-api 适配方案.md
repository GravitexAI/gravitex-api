# Gemini Omni Flash Preview 接入 gravitex-api 适配方案（设计分析与实现记录）

> 本文档保留最初的架构分析和落地决策，当前接入已经完成。实际对外调用方式请以同目录的 [平台 API 接入文档](Gemini%20Omni%20Flash%20Preview%20平台%20API%20接入文档.md) 为准，代码实现以当前 `relay/channel/task/gemini/omni.go`、`middleware/native_interactions.go` 和 `router/video-router.go` 为准。
> 模型本身的调用协议（Interactions API 全部请求/响应字段）见同目录 [`Gemini Omni Flash Preview 模型与调用文档.md`](./Gemini%20Omni%20Flash%20Preview%20模型与调用文档.md)，本文只讲"怎么接"。

---

## 1. 结论先行

**不能直接复用现有 `veo-*` 视频生成的那套通道**，需要新增一条独立的请求/响应转换逻辑，但**可以复用**现有的「task 渠道 + `/v1/videos` 提交/轮询」整体骨架。

原因：

- 现有 `veo-*` 走 Google 原生 **`:predictLongRunning`**（`instances`/`parameters` 结构，逐 model 拼 URL），提交后拿到 `operations/{id}`，靠 `GET .../operations/{id}` 轮询 `done` 布尔值。
- `gemini-omni-flash-preview` 走全新的 **Interactions API**：单一扁平端点（`.../interactions`，model 写在 body 里，不再是 `models/{model}:action` 这种路径），请求体是 `{model, input, generation_config.video_config.task, response_format}`，响应体是 `steps[]` 轨迹而非 `response.videos[]`，状态枚举也不同（多了 `requires_action`/`incomplete`）。

两套协议差异大到没法在现有 `BuildRequestBody`/`ParseTaskResult` 里加一两个 `if` 就糊弄过去（会让本就承担 Veo 逻辑的文件变得又长又难读）。但两者在"提交任务 → 轮询任务 → 解析最终视频 URL/base64"这个**业务模式**上是一致的——这正是 gravitex-api 现有 `channel.TaskAdaptor` 接口本来就要抽象的东西，所以骨架可以照搬。

---

## 2. 现状代码盘点（事实依据）

### 2.1 现有 veo 视频生成的完整链路

| 环节 | 位置 |
| --- | --- |
| 渠道类型常量 | `constant/channel.go:28` `ChannelTypeGemini = 24`；`constant/channel.go:41` `ChannelTypeVertexAi = 41` |
| Task 渠道 → Adaptor 注册（**一对一**，channel type 只能映射到一个 `TaskAdaptor` 实例） | `relay/relay_adaptor.go:144-178` `GetTaskAdaptor(platform)`：`case constant.ChannelTypeGemini: return &taskGemini.TaskAdaptor{}`；`case constant.ChannelTypeVertexAi: return &taskvertex.TaskAdaptor{}` |
| Gemini（AI Studio 通道）任务适配器 | `relay/channel/task/gemini/adaptor.go`，模型清单在 `GetModelList()`（第 291-293 行）：`veo-3.0-generate-001`、`veo-3.1-generate-preview`、`veo-3.1-fast-generate-preview` |
| Vertex（企业通道）任务适配器 | `relay/channel/task/vertex/adaptor.go`，`GetModelList()`（第 339 行）：`veo-3.0-generate-001` |
| 上游协议 | `:predictLongRunning`（Gemini 侧 `adaptor.go:165-170`；Vertex 侧按鉴权模式二选一，`adaptor.go:140-181`，见 §2.2） |
| OpenAI 兼容出口 | `/v1/videos`（POST 提交）/ `/v1/videos/{id}`（GET 查询），由 `RelayModeVideoSubmit`/`RelayModeVideoFetchByID` 承接（`relay/constant/relay_mode.go`），提交/查询路径判定在 `middleware/distributor.go` |
| 轮询循环 | `relay/relay_task.go` |
| `dto.NewOpenAIVideo()` | OpenAI Sora 风格的 video 资源结构，供 `DoResponse`/`ConvertToOpenAIVideo` 使用 |

**顶层 `relay/channel/gemini/constant.go`（同步聊天渠道）里的 `veo-*` 条目是"死条目"**：那套 `ModelList` 只服务 `/v1/chat/completions` 走 `:generateContent` 的同步聊天/生图流程（`relay/channel/gemini/adaptor.go`），没有任何分支处理 `veo-` 前缀，走进去只会打到上游 `:generateContent` 报错。真正生效的模型清单是上面表格里 task 适配器各自的 `GetModelList()`。

### 2.2 Vertex 任务适配器的双模式鉴权/URL（`relay/channel/task/vertex/adaptor.go:140-211`）

Vertex 渠道支持两种凭证形态，`BuildRequestURL`/`BuildRequestHeader` 据此二选一：

| 凭证形态 | 判定 | URL host | 鉴权头 |
| --- | --- | --- | --- |
| 纯 API Key（`isAPIKey(a.apiKey)` 即首字符非 `{`） | `info.ChannelOtherSettings.VertexKeyType == dto.VertexKeyTypeAPIKey \|\| isAPIKey(a.apiKey)` | `generativelanguage.googleapis.com`（因为 API Key 模式没有 service account JSON，拿不到 `projectID`，没法拼 `aiplatform.googleapis.com` 的 `projects/{project}` 路径） | `x-goog-api-key` 头 |
| Service Account JSON | 否则 | `aiplatform.googleapis.com`（或 `{region}-aiplatform.googleapis.com`），`projectID` 从 JSON 里的 `adc.ProjectID` 取 | `Authorization: Bearer <access_token>`（`vertexcore.AcquireAccessToken`），并带 `x-goog-user-project` |

`region` 通过 `vertexcore.GetModelRegion(info.ApiVersion, modelName)` 决定，默认 `global`。

### 2.3 `channel.TaskAdaptor` 接口全貌（`relay/channel/adapter.go:34-79`）

```go
type TaskAdaptor interface {
    Init(info *relaycommon.RelayInfo)
    ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError

    EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64
    AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64
    AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int

    BuildRequestURL(info *relaycommon.RelayInfo) (string, error)
    BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error
    BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error)

    DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error)
    DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, err *dto.TaskError)

    GetModelList() []string
    GetChannelName() string

    FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error)
    ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error)
}
```

`OpenAIVideoConverter`（同文件 81-83 行）另有 `ConvertToOpenAIVideo(task *model.Task) ([]byte, error)`，用于把内部 `model.Task` 转成 OpenAI `/v1/videos` 响应格式，现有 veo 适配器也实现了它。

---

## 3. 落地方案

### 3.1 关键约束：不能新注册一个 Adaptor，只能在现有实例里按模型名分流

`GetTaskAdaptor` 是 **渠道类型 → 单个 `TaskAdaptor` 实例** 的映射（§2.1 表格）。`gemini-omni-flash-preview` 挂在 `ChannelTypeGemini` / `ChannelTypeVertexAi` 这两个已经绑定了 `taskGemini.TaskAdaptor{}` / `taskvertex.TaskAdaptor{}` 的渠道类型下，**没有第三个渠道类型可用**，所以不能像"新建一个 provider 目录"那样独立注册。正确做法是仿照现有同步渠道里 imagen / imagine 的分流方式（`relay/channel/gemini/adaptor.go:60-74`，按 `strings.HasPrefix(model, "imagen")` 等分流）——在 `task/gemini/adaptor.go` 和 `task/vertex/adaptor.go` **各自现有的** `TaskAdaptor` 方法内部，按模型名再分一次流：

```go
// task/gemini/adaptor.go 内部示意
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
    if isOmniModel(info.OriginModelName) {
        return a.buildOmniRequestURL(info) // 新文件 omni.go 里实现
    }
    // ...原有 veo :predictLongRunning 逻辑不变
}
```

`isOmniModel` 建议就判 `info.OriginModelName == "gemini-omni-flash-preview"`（未来若出现 `gemini-omni-flash` 正式版或其它 Omni 变体，再改成前缀匹配）。

Omni 专属的 URL/Header/Body/轮询/解析逻辑建议放进同包新文件（如 `relay/channel/task/gemini/omni.go`、`relay/channel/task/vertex/omni.go`），保持与 Veo 逻辑的关注点分离，但仍属于同一个 `TaskAdaptor{}` struct、同一次渠道注册——这样 `ValidateRequestAndSetAction`/`DoRequest`/`GetChannelName` 等无需按模型区分的方法可以整体复用。

### 3.2 各方法需要做什么（按接口顺序）

| 方法 | Veo 现状 | Omni 需要的改动 |
| --- | --- | --- |
| `Init` | 记录 `ChannelType`/`baseURL`/`apiKey` | 不变，直接复用 |
| `ValidateRequestAndSetAction` | 从原始 body 提取 `durationSeconds`/`aspectRatio`/... 塞进 `Metadata`，供计费用 | Omni 分支需要额外提取"是否有参考图/首帧图/源视频"以推断 `video_config.task`（见 §3.3），仍写入同一个 `task_request.Metadata` |
| `EstimateBilling` | 现有实现固定返回 `nil`（`AdjustBillingOnComplete`/`EstimateBilling` 在 gemini task 适配器里都是空实现，第 678-688 行），真正的预估在 `BuildRequestBody` 里通过 `info.PriceData.OtherRatios` 设置（Vertex 侧示例见 `task/vertex/adaptor.go:273-276`） | Omni 按秒计费思路一致，可复用 `video_seconds` 机制（见 §3.6），在 Omni 分支的 `BuildRequestBody` 里设置 `OtherRatios` |
| `BuildRequestURL` | 拼 `{base}/{version}/models/{model}:predictLongRunning` | 拼 Interactions API 的固定端点：Gemini 侧 `{base}/v1beta/interactions`；Vertex 侧用 `relay/channel/vertex/url_builder.go` 的 `BuildAPIBaseURL(baseURL, version, projectID, region)` 拼出 `{base}/projects/{project}/locations/global/interactions`（**Interactions API 目前只支持 `global`**，region 强制写死，不要复用 Veo 那套 `GetModelRegion` 按 region 分流的逻辑） |
| `BuildRequestHeader` | Gemini 侧 `x-goog-api-key`；Vertex 侧按 API Key / Service Account 二选一（§2.2） | 完全复用现有两套鉴权代码，Interactions API 的鉴权方式和 `:predictLongRunning` 一致（同样是 `x-goog-api-key` 或 `Authorization: Bearer`） |
| `BuildRequestBody` | 组装 `{instances:[{prompt, image?, lastFrame?}], parameters:{...}}` | 组装 `{model, input, generation_config:{video_config:{task}}, response_format:{aspect_ratio, duration}, background: true}`（`input` 数组里塞 text/image 内容块，字段命名与 Veo 完全不同，需要新写，不能复用 `GeminiVideoPayload`） |
| `DoRequest` | 走公共 `channel.DoTaskApiRequest` | 不变，直接复用 |
| `DoResponse` | 解析 `{name: "projects/.../operations/{id}"}`，base64 编码 operation name 作为对外 `taskID` | Interactions API 提交响应直接带 `id` 字段（已经是扁平字符串），**不需要**再 base64 编码；按提交响应的 `status` 分流（`completed` 直接取结果、其余进轮询），设计见 §3.5 |
| `GetModelList` | 返回 veo 型号列表 | 追加 `"gemini-omni-flash-preview"` |
| `GetChannelName` | 返回 `"gemini"` / `"vertex"` | 不变，同一个渠道名 |
| `FetchTask` | `GET {base}/{version}/{operation_name}` | `GET {同 BuildRequestURL 的 base}/interactions/{id}` |
| `ParseTaskResult` | 解析 `done` 布尔 + `response.videos[]`/`generateVideoResponse` | 解析 `status` 枚举（`in_progress`/`requires_action`/`completed`/`failed`/`cancelled`/`incomplete`，比 Veo 多两种状态，需要新的映射表，见 §3.4），从 `steps[]` 里筛 `type=="model_output"` 且 content `type=="video"` 的块，取 `data`（base64）或 `uri` |
| `ConvertToOpenAIVideo` | 从 `task.Data`/`task.GetResultURL()` 拼 OpenAI video 资源 | 逻辑基本可复用（下游存储结构一致），只是数据来源换成 Omni 的解析结果 |

### 3.3 `video_config.task` 的推断规则

`/v1/videos` 的 OpenAI 兼容提交请求本身没有"text_to_video / image_to_video / reference_to_video / edit"这个概念，需要在 `ValidateRequestAndSetAction` 或 `BuildRequestBody` 里按已有字段推断（仿照现有 veo 逻辑里"有没有 `image`/`lastFrame`"的判断方式）：

| 输入情况 | 推断的 `task` |
| --- | --- |
| 只有 `prompt`，无图/无源视频 | `text_to_video` |
| 有一张图作为起始帧（对应现有 `metadata["image"]` 语义） | `image_to_video` |
| 有多张图作为风格/主体参考（现有协议没有对应字段，需要新增，见 §5 开放问题） | `reference_to_video` |
| 有源视频（现有协议没有对应字段，需要新增） | `edit` |

### 3.4 状态映射（新增两种状态）

| Interactions API `status` | 建议映射到项目内部 `model.TaskStatus` |
| --- | --- |
| `in_progress` | `TaskStatusInProgress` |
| `completed` | `TaskStatusSuccess` |
| `failed` | `TaskStatusFailure` |
| `cancelled` | `TaskStatusFailure`（或新增 `TaskStatusCancelled`，取决于项目是否已有该枚举，需确认） |
| `requires_action` | 项目现有状态机里没有对应语义（Interactions API 通用于 Agent 场景，纯视频生成预期不会出现），**建议**先按 `TaskStatusInProgress` 处理并打日志观察，不要贸然当失败处理 |
| `incomplete` | 同上，建议先按 `TaskStatusInProgress` 处理，需要产品/业务确认最终语义 |

### 3.5 同步与异步不是二选一——统一走 `background: true`，按首次响应 `status` 分流即可

Interactions API 只有"是否设置 `background: true`"一个开关，并不存在两套独立协议需要二选一。`/v1/videos` 对外协议本身就是"提交拿 `task_id` → 客户端另行查询"的异步契约，Omni Flash 生成得快慢对客户端完全透明。因此建议：

- **提交时始终传 `background: true`**，不区分"这次会不会生成得很快"。
- 在 `DoResponse` 里检查提交响应的 `status` 字段，一次分流即可覆盖"同步"和"异步"两种表现：
  - `status == "completed"`：说明生成很快、上游已经同步做完了，直接从 `steps[]` 取视频、把内部任务标记为成功，**不进入轮询**（这一条路径本质上就是"同步返回"的效果，只是走的是异步提交的响应体）。
  - `status == "in_progress"`：走现有 veo 同款的轮询骨架（`FetchTask` + `ParseTaskResult` + `relay/relay_task.go` 轮询循环），等下一次轮询拿到 `completed` 再取视频。
- 这是**同一份 `DoResponse`/`ParseTaskResult` 代码**里的一个 if 分支，不需要配置开关，也不需要客户端感知区别，因此不需要产品预先"选择"同步还是异步——两种情况天然都覆盖到了。
- 唯一**架构上确实不同**、需要额外工作量的是 `stream: true` 的 SSE 模式：它要求网关常驻一个协程持续消费 upstream 事件直到 `done`，是"上游推事件"而非现有框架"网关主动轮询拉状态"的模式，与 `FetchTask`/`ParseTaskResult` 的假设（离散的请求/响应）不吻合。**建议第一版不支持 SSE**，`background: true` + 轮询（含"第一次轮询就已完成"的快速情形）已经能覆盖同步和异步两种表现。

### 3.6 计费口径：优先用上游返回的真实 token 用量结算，按秒只作预扣估算

Google 官方定价本质上就是 **token 制**：输入（文本/图片/视频/音频统一）$1.50/1M token，文本输出 $9/1M token，视频输出 $17.50/1M token（$0.10/秒只是"固定 720p、固定 5792 token/秒"换算出来的等价报价，不是另一套独立定价，详见模型文档 §11）。

**关键差异（对比 veo）**：veo 走 `:predictLongRunning`，上游 `operationResponse`（`task/gemini/dto.go:65-87`）里**没有**任何 usage/token 字段，只有 `response.videos[]`/`generateVideoResponse` 之类的内容，所以 veo 只能靠"客户端请求时声明的 duration/resolution"去估算费用，做不到按真实用量结算，`video_seconds`/`video_resolution` 走 `OtherRatios` 是不得已的选择。但 Omni Flash 的 Interactions API 响应里，`Interaction.usage` 字段本身就带 `input_tokens_by_modality[]`/`output_tokens_by_modality[]`（模型文档 §8.2），`modality` 枚举里明确包含 `video`——也就是说**任务完成时上游会直接告诉我们这次视频输出真实花了多少 token**，不需要自己拿"时长 × 每秒 token 数"去猜测。

**推荐做法**（比继续套用"按秒估算"更贴合现有 token 计价体系，实现量也不大）：

1. **提交时（`EstimateBilling`）**：按请求声明的 `duration` × 已知的 720p 固定换算率（5792 token/秒）粗略估一个预扣金额，走现有 `OtherRatios` 预扣机制——这一步和 veo 做法一致，只是"先扣一笔押金"，不追求精确。
2. **任务完成时（`ParseTaskResult` + `AdjustBillingOnComplete`）**：`status == "completed"` 时，从最终 `Interaction.usage` 里取真实 token 数，按 $1.50/1M（输入，无需分模态，官方输入统一定价）、$9/1M（文本输出）、$17.50/1M（视频输出）算出真实费用，通过 `AdjustBillingOnComplete` 补扣/退还差额——**这正是 `TaskAdaptor` 接口专门为"预估与真实用量有出入"设计的 hook**（接口注释原文：`Return a positive value to trigger delta settlement (supplement / refund)`），不需要额外造轮子，也不需要在 `setting/ratio` 里维护"分辨率 × 时长"组合费率表（比 veo 现状更简单，因为官方输入价格不分模态，只有文本输出/视频输出两档单价）。

⚠️ **待验证风险（联调阶段必须确认，不能想当然）**：`output_tokens_by_modality` 带 `modality: "video"` 是通用 Interactions API schema 里的枚举，官方参考页给出的 usage 示例实际是 Lyria 音乐模型的 `modality: "audio"`，**没有拿到一份 Omni Flash 视频生成的真实响应样例**去确认 preview 阶段这个字段是否已经真的下发、数值是否准确。第一次联调必须打一个真实请求，看 `completed` 状态下 `usage.output_tokens_by_modality` 里到底有没有 `video` 这一项——如果 preview 阶段这块缺失或不可信，退回到"按秒估算"（`duration × 5792 token/秒`）作为兜底，且这个兜底本身也已经是 §3.6 第 1 步在做的事，切换成本很低。

---

## 4. Veo vs Omni Flash 关键差异一览

| 维度 | Veo（`predictLongRunning`） | Omni Flash（Interactions API） |
| --- | --- | --- |
| 端点形态 | `models/{model}:predictLongRunning`（逐模型拼路径） | 固定扁平端点 `.../interactions`（model 写 body 里） |
| 请求体顶层结构 | `{instances:[...], parameters:{...}}` | `{model, input, generation_config, response_format, background/stream}` |
| 任务提交后的 ID | `operations/{id}`（需 base64 编码后对外暴露） | `id` 直接是扁平字符串，无需二次编码 |
| 轮询端点 | `GET {base}/{operation_name}` | `GET {base}/interactions/{id}` |
| 状态字段 | 布尔 `done` + 错误对象 | 枚举 `status`（6 种取值，比 Veo 多 `requires_action`/`incomplete`） |
| 结果定位 | `response.videos[]` / `response.generateVideoResponse.generatedSamples[]` | `steps[]` 中 `type=="model_output"` 且内容块 `type=="video"` |
| 多轮编辑 | 不支持（每次都是独立请求） | 支持，靠 `previous_interaction_id` 或整段回传 `steps` |
| 参考图 | 仅首帧/尾帧（`image`/`lastFrame`） | 首帧、风格参考、编辑源视频，语义更丰富（`video_config.task` 区分） |
| 区域支持 | 支持按 region 调用（`GetModelRegion`） | 目前仅 `global` |
| Vertex 双模式鉴权（API Key / Service Account） | 已实现 | 同一套鉴权机制可直接复用（协议层没有差异） |

---

## 5. 需要业务/产品确认的开放问题（不建议工程侧自行拍板）

> 以下两点**不在此列**，属于工程侧可以自行决定的实现细节，不需要等产品拍板：
> - 同步/异步调用——见 §3.5，统一 `background: true` + 按响应 `status` 分流。
> - 计价维度——见 §3.6，优先用上游真实 token 用量结算（`AdjustBillingOnComplete`），按秒估算只作预扣兜底。**明确不支持 `stream: true` SSE 模式**，第一版统一走 `background: true` + 轮询。

1. **`requires_action` / `incomplete` 状态语义**：项目现有 `model.TaskStatus` 枚举没有直接对应项，需要产品/后端一起定义映射策略（§3.4 只是工程侧的保守建议，不代表最终方案）。
2. **最终售价**：§3.6 只解决了"怎么算真实成本"的技术方案，官方成本价（输入 $1.50/1M、文本输出 $9/1M、视频输出 $17.50/1M）转换成对用户的售价是否要加价、加多少，需要业务确认，与现有 veo 定价做横向对比。
3. **是否要支持多轮对话式编辑（`previous_interaction_id`）**：现有 OpenAI `/v1/videos` 协议本身没有"基于上一个视频继续编辑"的字段概念，如果要支持，需要设计新的透传字段（类似现有 veo 走 `metadata.image`/`metadata.lastFrame` 的方式，比如 `metadata.previous_interaction_id` 或 `metadata.reference_images`），这是一个协议层扩展，需要先确认客户端（gravitex-api-cli / gravitex-api-admin）是否有对应交互需求。
4. **参考图（`reference_to_video`）与视频编辑（`edit`）在管理后台的模型清单如何呈现**：是否需要在 `abilities` 表 / 渠道模型清单里让运营可视化配置 `gemini-omni-flash-preview` 的启用与否（类比现有 `model_setting.IsGeminiModelSupportImagine` 的做法，为 imagine 模型清单开放了后台可配置项）。

---

## 6. 原设计开发步骤 Checklist

> 本节保留用于回顾设计与实现差异，不代表当前仍有待办项。当前对外 API 的能力边界以平台 API 文档为准。

1. [ ] 与产品确认 §5 中的开放问题（尤其是状态映射与最终售价），避免返工。
2. [ ] 在 `relay/channel/task/gemini/` 新增 `omni.go`：实现 Omni 专属的 URL/Header/Body 构造与 `ParseTaskResult`。
3. [ ] 在 `relay/channel/task/vertex/` 新增 `omni.go`：复用现有双模式鉴权，实现 Vertex 侧 Interactions API 端点拼接（注意 region 强制 `global`）。
4. [ ] 在两个包各自的 `TaskAdaptor` 方法入口加 `isOmniModel` 分流，接入 §3.2 表格里列的改动点。
5. [ ] `GetModelList()` 追加 `"gemini-omni-flash-preview"`；确认是否需要同步更新渠道模型清单/后台可配置项（管理端 `abilities` 表）。
6. [ ] 在 `setting/ratio`（或等价计价模块）新增 Omni Flash 的三档单价（输入 $1.50/1M、文本输出 $9/1M、视频输出 $17.50/1M，售价按业务确认后的加价系数），并实现 `EstimateBilling`（按 duration 预扣）+ `AdjustBillingOnComplete`（按 `usage` 真实 token 结算差额），见 §3.6。
7. [ ] 补充单元测试：至少覆盖「文生视频提交」「异步轮询到 completed」「上游返回 failed/cancelled」「视频块从 `steps[]` 正确提取」「`usage` 真实 token 与预扣金额有出入时的补扣/退款」五类场景（参考现有 `relay/channel/task/gemini/adaptor_test.go` 的测试风格，若存在的话；未发现该测试文件则需要新建）。
8. [ ] 联调阶段重点验证（不能想当然）：`background:true` 提交后的初始 `status` 到底是什么；`requires_action`/`incomplete` 是否会在实际视频生成场景出现；**`completed` 状态下 `usage.output_tokens_by_modality` 是否真的带 `video` 模态且数值可信**（§3.6 的兜底方案是否需要启用）。

---

## 参考

- 模型协议细节：同目录 [`Gemini Omni Flash Preview 模型与调用文档.md`](./Gemini%20Omni%20Flash%20Preview%20模型与调用文档.md)
- 现有同类实现参考：`relay/channel/task/gemini/adaptor.go`、`relay/channel/task/vertex/adaptor.go`、`relay/channel/vertex/url_builder.go`
- 现有"按模型名分流"的项目内先例：`relay/channel/gemini/adaptor.go`（imagen / imagine 分流）、`docs/gemini/imagine-images-generations.md`
