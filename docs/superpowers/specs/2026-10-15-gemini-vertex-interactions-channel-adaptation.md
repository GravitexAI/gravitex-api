# Gemini / Vertex Interactions 渠道适配方案

## 目标

在不引入模型白名单、不改变已支持模型既有行为的前提下，让网关支持客户端调用 `/v1beta/interactions`，并按实际命中的渠道类型转发 Google Interactions 请求。该网关入口对 Gemini 与 Vertex 渠道都必须可用：Gemini 渠道使用 Gemini Interactions API；Vertex AI 渠道将同一客户端请求在上游请求层转换为 Vertex 官方 Interactions API。另保留 Vertex 资源型客户端入口。计费、日志、后台查询和流式响应都保留 Interaction 的语义。

本文件记录通用渠道适配方案；仓库已有按模型分支的部分 Interactions 实现，但不代表本文要求的全模型通用适配及账务保证已经实现。

## 风险标记（上线前必须逐项关闭）

| 等级 | 风险 | 当前判断 | 上线门槛 |
| --- | --- | --- | --- |
| **P0｜已确认，阻断计费上线** | 同步原生 Interaction 的实际 quota 与资金来源差额未闭合：`SettleBilling` 对 BillingSession 使用 `skipFunding=true`，现有 Interaction task logger 只记用量统计；实际 usage 与预扣不同时，钱包/订阅、token、日志可能不一致。 | 由现有调用链可确认，不是推测；本次仅记录方案，代码尚未修复。 | 钱包和订阅分别验证预扣、补扣、退款、实际用量、token 与日志一致；所有资金 effect 有明确失败/重试语义。 |
| **P0｜通用能力上线阻断** | 结算与消费日志缺少持久化恢复/幂等保障：`BillingSession` 是内存状态，`RecordConsumeLog` 无持久成功回执；ClickHouse/ByteHouse 可能先进入内存 buffer。 | 当前基础设施不能证明崩溃恢复或 exactly-once。 | 持久化 settlement journal/outbox 与稳定去重键；资金调整和日志投递可分别安全重试。未完成时必须缩小支持范围并明确降低保证级别。 |
| **P1｜usage 正确性风险** | Vertex 官方 Interactions usage 与旧 Lyria parser 的字段/语义不一致；按 aggregate、modality、cache、thought/tool 处理不当会漏计或重复计费。 | 旧 parser 读取扁平字段并可能用 residual 推 thought，不能直接视作通用解析器。 | 对官方 usage 版本建立规范化规则和合同测试；可信 usage 按明确包含关系计费。缺失/无法解释/矛盾时沿用现有计费路径的估值兜底并在日志标明，不挂起账单、不按负数结算。 |
| **P1｜价格与预扣风险** | “不按模型目录拒绝”不等于无价格模型可安全请求；没有可靠价格或预扣上限会导致漏扣、过扣或无法安全结算。 | 价格策略需产品/实现明确，不能从代码自动推断。 | 逐模型/渠道明确价格来源和有上限的预扣；缺价时返回明确错误，不默认免费或把估值当最终费用。 |
| **P1｜日志读数风险** | 只写官方原始 usage 或 `usage_facts`，Java 账单明细不会自动识别；旧 Interaction 的 `is_task`/request ID 分类变化也可能让既有消费者漏读。 | Java 映射是已知字段映射；现有专用 Interaction 使用 task logger。 | 保持旧分支字段语义；新 writer 输出 Java 已识别的聚合及 flat fields。新增客户可见字段时联动 Java、Admin/CLI 并验证展示。 |
| **P1｜重复上游成本风险** | 创建 POST 响应丢失后自动重试可能重复创建 Interaction 并产生第二笔上游费用；SSE 断开/后台超时也不能证明零用量。 | 上游幂等能力和提交结果恢复机制需逐渠道核实。 | 区分明确失败与提交未知；未知提交不得无条件换渠道重发。usage 不明按现有预扣/估值兜底结算，不据此判定零费用；提交结果未知仍需保留可恢复状态。 |

其中 P0 表示现有账务正确性或恢复能力未达标时不得将相应计费能力上线；P1 表示必须在通用能力开放前完成验证或提供明确的安全降级。下文对应代码依据与应对方案均保留，风险关闭应以实现和测试证据为准，不能仅因文档完成而视为已解决。

## 官方端点与项目入口

官方 Gemini Developer API 创建端点为 `POST /v1beta/interactions`；Vertex AI 创建端点为 `POST /v1beta1/projects/{project}/locations/global/interactions`。项目已注册 Gemini 风格的 `/v1beta/interactions`，也注册了 Vertex 资源型 `/v1beta1/projects/{project}/locations/{location}/interactions`。两种入口都使用官方复数资源名 `interactions`，不新增非官方单数 `/v1beta/interaction`。

- Gemini 官方参考：[Interactions API](https://ai.google.dev/api/interactions-api)
- Vertex 官方参考：[Interactions API](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/interactions-api)

端点按官方渠道固定，不作为可选设计项：网关公共入口 `/v1beta/interactions` 必须支持 Gemini 与 Vertex 两种渠道；Gemini 上游使用 `https://generativelanguage.googleapis.com/v1beta/interactions`，Vertex 上游使用 `https://aiplatform.googleapis.com/v1beta1/projects/{project}/locations/global/interactions`。所以 Vertex 转换不是拒绝 `/v1beta/interactions` 或要求调用方更换路径，而是同一网关入口在 Vertex channel selection 后转换到 Vertex 官方 endpoint。Google 文档另有 CMEK 多区域说明；只有后续明确实现 CMEK 时，才按官方文档规定的 `us`/`eu` location 与 `aiplatform.{LOCATION}.rep.googleapis.com` host 增加对应分支。

客户端入口和上游端点是两个层次：网关必须支持 `POST /v1beta/interactions`，并继续支持 Vertex 资源型入口 `POST /v1beta1/projects/{project}/locations/{location}/interactions`；命中 Gemini 渠道时 `/v1beta/interactions` 按 Gemini 官方 endpoint 转发，命中 Vertex 渠道时同一个 `/v1beta/interactions` 请求转换为 Vertex 官方 global endpoint。项目当前已注册这两个复数入口，但都会改写到 `/v1/video/generations` 并复用视频任务管线（见 `router/video-router.go`、`middleware/native_interactions.go`）。新能力应扩展现有入口，不要求 Vertex 客户端必须改用资源型入口，也不新增单数路径。

## 路由与渠道适配

1. 客户请求保持官方 Interactions 请求格式，网关完成常规鉴权和模型渠道分配。
2. 模型名只用于现有渠道分配及计价配置；不得用模型目录决定该端点是否可用，也不得在适配层因模型不在目录内而拒绝。
3. 在渠道分配完成后，依据最终 `ChannelType` 选择上游适配：
   - `ChannelTypeGemini`：客户端可调用 `/v1beta/interactions`，网关将请求发往 Gemini Developer API 的 `/v1beta/interactions`，保持请求体、模型名及 Interaction 响应语义，不做 Vertex 转换。
   - `ChannelTypeVertexAi`：客户端同样可调用 `/v1beta/interactions`；也可使用已注册的 Vertex 资源型入口。两种入口都保留 Interaction 请求/响应结构，并在上游适配层构造 Vertex 官方资源路径（首期固定 `locations/global`）、加入 Vertex 项目并使用渠道配置的 OAuth 凭证。若客户端使用 Vertex 资源型入口，项目必须与渠道配置一致；不得把客户端传入的任意 location 直接拼入上游地址。
   - 其他渠道：不做模型目录拒绝；按渠道适配能力转发。尚未具备 Interactions 协议能力的渠道返回其上游/渠道错误，不伪装成视频请求。
4. Gemini/Vertex 的模型能力和参数合法性由各自上游校验。网关仅执行通用 JSON、鉴权和账务安全校验。
5. 对现有 Lyria、Omni、Veo 等已接入任务行为保持兼容；增加通用 Interaction 转发时不得改变它们目前的对外状态、查询和结果交付契约。适配器应按渠道类型承担协议差异，既有专用任务适配仍可保留其内部任务生命周期。

## 响应与状态

- 对外始终返回 Google Interaction 响应格式；Vertex 的资源 URL、认证信息和内部任务字段不得泄漏给客户端。
- 同步请求透传完整终态 Interaction。
- `stream: true` 按上游 SSE 事件逐事件转发，并保留终态事件及其 usage；网关解析流只用于用量/计时采集，不重写 Gemini 响应事件。
- `background`/`store` 请求须保留可供后续 GET、取消、续写使用的 Interaction ID 与最终渠道绑定。后续操作必须回到创建请求命中的同一渠道，不能重新按当前负载均衡选渠道。
- 后台查询、取消和流续传沿用官方复数资源路径；Vertex 上游请求使用对应 Vertex 资源路径。

## 计费方案

现有专用 Interactions 分支必须保留当前 `LogTaskConsumptionWithTaskInfo` 日志契约（包括 `is_task`、request ID/upstream request ID 分工及 Java 已读取的 flat fields），不得为通用实现改写旧记录语义。通用 Interaction 可复用 `PostTextConsumeQuota` / `GenerateTextOtherInfo` 的 usage 与日志字段协议，但不能直接调用整个 text handler（它包含 chat-specific usage normalization 和资金结算流程）；也不能把任意 Interactions 都交给 `LogTaskConsumption`，因为它固定 `is_task=true`、调用 `RecordConsumeLog` 后另行累计用量，且不是后台恢复单元。新路径需使用独立的 Interaction settlement + log builder，并逐字段兼容 Java billing reader。

统一采用“预扣 → 获取终态 usage → 按资金来源结算差额 → 持久化结算结果/日志投递任务”的账务生命周期；成功消费日志由 outbox 投递，不能假设余额更新和日志库写入处于同一事务：

- 同步：先按现有价格配置预扣，解析最终 usage 后结算。
- SSE：按流中的终态 usage 结算；客户端断开不能直接视为上游零消耗。
- 后台：提交时先预扣，并持久化 user/token/channel/model、interaction ID、预扣金额和账务状态；GET/取消取得终态 usage 后幂等结算。部分生成后取消仍按已产生的 usage 计费。
- 明确上游拒绝且能证明未产生计费时按现有规则退款；若有可信部分 usage 按实际用量结算。成功响应明确报告 usage 为 0 时按该计费路径的零用量规则处理；usage 缺失不能当作零，应回退到既有配置估值/预扣。
- usage 缺失、格式未知或关键字段无法解释时，**不得新增“待对账”计费状态**。Interaction task 沿用现有任务行为：保留 `ModelPriceHelperPerCall` 算出的初始/预扣额度作为最终配置估值，日志明确标记 `billing_process`/usage 来源为估值，不能伪装成上游实际 usage；只有可信、可归一化的终态 usage 才覆盖该估值。无法安全估值或预扣时，在发往上游前按价格配置错误拒绝。负数、溢出或互相矛盾的 usage 不得直接用于 quota 计算，也不得因此产生负扣费。
- 所有 token、模态和价格倍率经过项目现有边界校验与安全 quota 转换；饱和事件写入 `other.admin_info.quota_saturation`。
- interaction ID 是业务关联键，不自动提供幂等扣费。重复轮询、重复终态事件和服务重试必须通过持久化结算状态/唯一 claim 防止重复资金调整；日志写入需单独具备可恢复的去重策略。

现有模型价格配置仍用于计价；“不按模型目录限制端点”不等于“无价格模型免费”。上线前须明确没有模型价格时的渠道级兜底策略，确保预扣上限与最终结算不会造成负扣费或漏扣费。

### 按当前代码核对的计费边界

当前实现存在几条不能直接当作通用 Interaction 账务实现的路径：

| 当前代码                                                                 | 已有行为                                                                                              | 对新能力的影响                                                                                                                             |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `relay/relay_task.go` 的 `ModelPriceHelperPerCall` 与 pre-consume      | 视频/任务请求按次计算初始额度；按量视频模型走专用分支。普通模型未配置价格时还要经过倍率配置与 `AcceptUnsetRatioModel` 检查。                       | 任务预扣不是通用 token usage 结算器。模型未配置价格时仍可能在发往上游前被价格校验拦截；这与模型端点白名单是不同的门槛，必须在产品策略中明示。                                                       |
| `nativeGenerateContentUsageQuota` / `isNativeGenerateContentBilling` | 最终 usage 结算依赖 `IsNativeInteractionGenerateContentModel` 的模型集合；目前按 token、缓存、thought，以及图像输出的配置倍率计算。 | 直接移除模型名单但仍复用任务结算，会让名单外模型拿不到这段实际 usage 结算，可能按初始估值收费。通用方案必须按 Interaction 响应中的 usage 结构结算，模型名不得作为是否结算的开关。                              |
| `relay/common/relay_info.go` 的 `TaskInfo`                            | 结构字段含总输入/输出、文本/图片/视频分项、缓存和 thought；其他 usage 可塞入 `UsageFacts`。                                     | 现有强类型字段尚未覆盖全部 Interactions 模态和 usage 版本。新增解析应保留原始 usage 并生成规范化账务 usage；聚合数与分项数、缓存子集、thought 子集不能重复累加。                               |
| `service/billing_session.go` / `service/billing.go`                  | `BillingSession` 是内存状态；`SettleBilling` 调用 `Billing.Settle(actualQuota, true)`，约定调用方已自行结算资金来源。     | 后台请求不能只把 `BillingSession` 放进 goroutine 或假定它能从 Task 恢复。钱包/订阅差额、token 额度和账务状态必须有持久化恢复路径；使用 `SettleBilling` 前也必须明确由哪个事务/函数更新资金来源。      |
| `model/task.go` / `service/task_billing.go`                          | Task 私有数据已有 `BillingSource`、订阅/令牌 ID、`BillingContext` 价格快照；异步任务结算通过任务配额和状态认领实现退款/差额。              | 可先评估扩展现有 `tasks.private_data` 承载 Interaction 账务快照，避免新表；但现有快照不等于可序列化 BillingSession，需补齐预扣额、实际 usage、结算/退款状态和幂等标识。若另建表或加列，需按三数据库规则验证。 |

实现必须明确以下规则：

- **必须先处理的现存资金差额风险**：原生同步 Interaction 当前由 `RelayTaskSubmit` 预扣，并可能通过 `nativeGenerateContentUsageQuota` 把最终 quota 改为实际 usage；`controller/relay.go` 随后调 `SettleBilling`，而该 helper 对 `BillingSession` 固定调用 `Settle(actualQuota, true)`。此路径没有先调用 `model.ConsumeUserQuotaSettle`；`LogTaskConsumptionWithTaskInfo` 只累计 `used_quota`/request count 和 channel usage，不改用户钱包余额，也不提交订阅的实际差额。因此当实际额度与预扣不同，钱包/订阅仍停留在预扣状态，日志使用实际额度，token quota 又可能按差额调整，三者不一致。`PostTextConsumeQuota` 虽是 usage 和日志字段的参考，但它也在结尾调用同一个 `SettleBilling(..., true)`；订阅分支只更新 used stats，没有在本函数中提交 subscription delta。不得把该路径的资金结算盲目照搬。实现必须显式分资金来源：钱包路径原子更新钱包差额与 actual used/request count，再让 BillingSession 只处理 token delta；订阅路径调用真正的 subscription delta settle（等价于 `SubscriptionFunding.Settle(delta)`，不能 skip funding），并仅更新一次 token 和统计。需决定是否一并修复现有同步 Interaction/标准文本订阅结算；差额场景必须覆盖钱包、订阅、token 三方。
- **官方 usage 结构不能由旧 parser 代替**：`relay/channel/task/lyria/adaptor.go:parseInteractionResult` 当前读取扁平 `cached_tokens`、`thought_tokens`、`text_input_tokens` 等字段，thought 缺失时还会用 `total_tokens - input - output` 推导。Vertex 官方 Interactions `usage` schema 则包含 `total_cached_tokens`、`total_thought_tokens`、`input_tokens_by_modality`、`cached_tokens_by_modality`、`output_tokens_by_modality`、`total_tool_use_tokens` / `tool_use_tokens_by_modality` 等。通用 parser 必须按官方字段归一化；显式 thought 优先，不得把总量残差全部当 thought；cached/modality 明细必须保持子集/拆分语义而不重复收费；tool-use tokens 先记录，不得未确认包含关系就再加到账单。
- **上游 usage 也是外部输入**：解析到负数、超出可安全表示范围或 total 与明细矛盾时，不得把值直接转成 quota/token 整数或产生负差额。可信数值的 quota 转换使用项目 checked helpers，并把 clamp 放入 `other.admin_info.quota_saturation`；usage 本身无法可信归一化时回退到该请求已建立的安全配置估值，不把异常 usage 当实际用量，也不进入待对账计费状态。输入/输出模态明细缺失时，仅使用能从官方定义确认的 aggregate 计价；不从不明 residual 推导 text、thought 或 tool usage。
- **当前日志 writer 不是可靠账务 outbox**：`model.RecordConsumeLog` 在 `LogConsumeEnabled=false` 时跳过，返回 `void`；`CreateLog` 在 ClickHouse/ByteHouse 下先把记录放入内存 `GlobalLogBuffer`，flush 才写库，flush 失败只告警并 dump 到本地 `failed_logs.jsonl`。因此不能保证资金结算与唯一消费日志原子提交，不能以函数返回判定日志落库，也不能只靠 log 做后台恢复。若要求计费/用量/日志可恢复，需持久化结算状态和待写日志载荷（outbox/journal），由唯一 settlement key 防止重复资金变更、由日志去重键支持安全重放；但 outbox 不能在日志仅进入内存 buffer 后就标记完成，必须确认持久写入/持久队列接收，并覆盖主库与 log DB 分离时的故障恢复。钱包/订阅的余额调整本身也没有 settlement key；需要用主库事务/幂等 ledger 保护“资金已调整但 journal 状态未更新”的崩溃窗口。若暂不做持久 outbox/幂等资金操作，文档与验收不得承诺 exactly-once。

- 通用预扣应以原始 Interaction `input` 和经校验的最大输出预算估算；不能用被改写后的占位 `prompt` 代替真实输入。用户可控的 `max_output_tokens` 等数量必须复用现有 token 上限校验。若 agent 循环可多轮生成，预扣策略需限定总预算或按阶段追加安全 reserve。
- “任何上游支持的模型都不因模型目录被网关拒绝”不能隐含成“任何未配置价格的模型都能安全结算”。无价格时应明确渠道级保守价格/额度上限，或返回可识别的价格未配置错误；不得落到 0 元，也不得用固定默认估值作为最终实际费用。
- 通用 usage 计费优先使用上游 terminal usage，并明确总量和分项包含关系。usage 缺失、未知版本或关键分项无法解释时，使用当前计费路径已有的估值/预扣兜底并记录来源；这不同于上游明确报告零 usage。明确零 usage 按现有计费路径的零用量规则处理；不得把未收到 usage 解释为零，也不得引入待对账计费状态。
- 调用模型工具产生的模型 token 可按上游 usage 计费。外部工具自身的执行成本不在 Google token usage 内；只有已有独立工具计费器能提供经验证的次数/金额时才叠加，且不得再把同一成本混入 token 费用。
- Interaction 创建 POST 具有副作用。若上游可能已创建成功但网关未收到响应，自动换渠道重试可能创建第二个 interaction 并产生双重成本。新路径需定义请求幂等键或“提交结果未知”状态；未知提交不得按普通可重试 5xx 无条件重发。
- SSE 断开、客户端取消、background 查询超时都不是上游零用量证明。usage 缺失时按已建立的预扣/配置估值结算，不按零退款；如果创建 POST 的结果本身未知，则保留提交状态以避免重复创建，这与 usage 缺失的计费兜底分开处理。
- 结算状态与消费日志载荷需关联同一 settlement key；资金操作和日志投递各有独立可重试状态。资金调整失败不得标记已结算；日志投递失败只能重放日志，不得重放资金扣款。现有 `RecordConsumeLog` 无 dedup key/持久成功回执，必须补齐 outbox/journal 或调整保证级别。可借鉴 `controller/task_video.go` 的条件 claim 思想，但它不覆盖所有资金副作用的崩溃窗口，不能当作完整 exactly-once 方案，也不能复用视频专用计价公式。

## 日志方案

最终完成的 Interaction 在消费日志启用时写入一条消费日志；轮询过程和 SSE 分片不逐条写消费日志。失败/处理中状态可保留请求或任务状态日志，但不得冒充消费记录。`Log` 是展示/审计投影，不是计费账务真相源；若要求在进程崩溃、资金结算失败或 log DB 暂时不可用后恢复，必须有持久化结算状态和待写日志载荷，不能只依赖 `RecordConsumeLog`。

消费日志继续使用现有 `Log` 与 `other` 扩展字段，不优先增加数据库列。应记录：

- 客户端 `request_path`（官方路径）、系统 request ID、上游 interaction ID；三个标识分别保存并能互相追踪。
- 最终渠道 ID、渠道类型、原始模型名、模型映射后的上游名（如有）。
- `interaction_status`、执行模式（同步/SSE/background/store）、请求耗时和结算来源。
- 输入/输出 token 总数及可得的文本、图像、音频、视频、缓存、thought/tool 等 usage 分项。
- 账务 journal 保存预扣、最终 quota、结算状态和价格/倍率快照；消费日志只投影需要用于查询/展示的字段，不作为恢复状态的唯一存储。
- Vertex 的转换链路和上游状态码放在管理员可见诊断字段；不得记录 API key、OAuth token 或原始鉴权头。

字段命名应复用 Java 日志服务与 Admin/CLI 当前消费日志协议；新增展示字段前先检查读取方，未知字段对旧版本应可忽略。不能依赖请求结束后的 `gin.Context` 作为后台结算信息来源，后台必需数据需随任务/Interaction 状态持久化。

### 日志读取侧的代码边界

- Go `model/log.go` 的 `RecordConsumeLog` 是消费日志写入入口；`service/task_billing.go` 会将任务日志标记 `is_task=true` 并写用户/渠道累计量。既有 native Interaction 分支已使用该 task logger，旧模型必须保持当前日志分类；新增通用 Interaction 使用独立 builder，不能把它误记为视频任务，但 `is_task` 是否保留需以现有 `/v1beta/interactions` 兼容日志契约为准，不能因实现分支不同导致旧消费者漏读。
- Java `LogsServiceImpl.parseOtherJsonToMap` 会解析 `other`，但只把已知字段映射到日志 VO 顶层；`usage_facts` 中任意新增 key 不会自动成为列表字段。`BillingDetailService.computeBillableTokens` 依据 `prompt_tokens`/`completion_tokens` 聚合数，再从 `other` 的扁平 `input_image_tokens`、`video_input_tokens`、`audio_input`、`image_output_tokens`、`text_output_tokens`、`reasoning_tokens`、`cache_tokens` 等拆分；计费明细也读取 `model_ratio`、`completion_ratio`、`image_completion_ratio`、`cache_ratio`、模态价格和 billing type。Interactions writer 必须沿用相同的总量/分项包含关系；不能只写 `usage_facts` 或原始 usage JSON，否则 Java 明细会折叠模态、漏思考 token或把缓存重复计入。使用新字段时需扩展 Java VO/计算/导出和需要展示它们的前端类型。
- Admin 的账单列配置使用固定字段；CLI `ExpenseLog` 类型已有文本/图像/音频/视频等有限字段，费用过程文案来自 Java 返回的 `billingProcessText`。若目标仅是审计，可先写入受控 `other`；若要用户可见展示，必须走完整的 Go writer → Java VO/service/export → Admin/CLI contract，不可只改 Go JSON。
- `other.admin_info` 只允许管理员查看。渠道转换诊断、价格来源与内部 retry 状态放在管理员字段；客户消费详情只能出现必要的公开 usage 和费用数据。原始 key、OAuth token、鉴权头和含敏感信息的上游原文禁止写入日志。

## 兼容与验收条件

- 既有 `/v1beta/interactions` 已支持请求的响应形状、任务查询、用量与账单结果保持不变。
- 一个模型请求分别命中 Gemini 与 Vertex 渠道时，只发生对应渠道的上游适配；非白名单模型不被网关提前拒绝。
- Gemini 请求与响应协议保持原样；Vertex 上游收到 Vertex 官方路径与认证，对外仍是 Google Interaction 格式。
- 同步、SSE、background/store、失败/取消都能在重试和重复查询时只结算一次；消费日志启用时经 outbox 可安全重放且按稳定去重键只呈现一条。没有 outbox/dedup 前不得宣称这一日志保证。
- usage 分项在日志及 Java/Admin 消费详情中不被错误合并；系统 request ID 与上游 interaction ID 均可检索。
- 价格缺失、usage 缺失/零、重复终态、日志写失败、结算部分失败、POST 上游结果不确定、SSE 断连等状态均有明确且符合现有渠道语义的账务结果；usage 缺失按估值兜底，不进入待对账计费状态。
- 不引入数据库 schema 变更优先；如实现需要新增字段/表，必须验证 SQLite、MySQL、PostgreSQL，并覆盖新库与代表性升级库。

## 影响范围与兼容边界

### 当前请求链路（代码确认）

两个复数路由都注册在 `router/video-router.go`，中间件 `middleware/native_interactions.go` 将 POST body 改写成视频任务请求，并把 URL 改写为 `/v1/video/generations`；GET 也映射到视频任务查询。该中间件对部分已知 Gemini 请求生成任务占位 prompt，对其余请求会按视频字段解析并可能拒绝音频输入。Vertex 的专用 Interactions TaskAdaptor 则在 `relay/relay_adaptor.go:GetTaskAdaptorForRequest` 中按 `NativeInteractions + ChannelTypeVertexAi + model capability list` 选择。

这意味着仅修改 Lyria TaskAdaptor URL 不足以支持通用 Interactions：多数请求会先经过视频请求转换、Task RelayMode、按次预扣和任务结果解析；同一通用接口的异步 GET、SSE 也由任务控制器驱动。实现需要先在现有官方复数路由上增加/拆出 Interaction relay mode，使原始请求体进入普通 Interaction handler，再在已选渠道后适配上游；已运行的 Lyria/Omni/Veo 特殊任务继续由旧任务分支承接。

### 文件和模块责任边界


| 层          | 代码位置                                                                                                                                              | 本方案触及点                                                                                          | 明确不应顺带改变                                                                               |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| 路由/协议入口    | `router/video-router.go`、`middleware/native_interactions.go`、`controller/native_interactions.go`                                                  | 复数入口保留；区分通用 Interaction 和既有专用任务流程；保留客户端原始 path、raw body、stream/background/store 语义。             | `/v1/video/generations`、`/v1/videos` 的请求体、计价和返回结构。                                     |
| 渠道选择/适配    | `controller/relay.go`、`relay/relay_adaptor.go`、`relay/channel/task/lyria/adaptor.go` 及 Gemini/Vertex API 适配层                                      | channel selection 完成后按 `ChannelType` 选择 Vertex URL/认证转换或 Gemini 原样转发；模型能力交由上游校验。                | model mapping、channel affinity、普通 Gemini generateContent、Vertex generateContent 的既有协议。 |
| 价格/结算      | `relay/helper/price.go`、`relay/relay_task.go`、`service/billing.go`、`service/billing_session.go`、`service/text_quota.go`、`service/task_billing.go` | 定义 Interaction 估算、终态 usage 规范化、资金来源与 token 差额更新、重试/恢复幂等。                                        | 按次任务、Veo 秒计费、Omni 视频 token 计费等专用公式，除非其既有分支需要明确保留。                                      |
| 持久化/后台生命周期 | `model/task.go`、`controller/task_video.go`、`controller/lyria_async.go`                                                                            | 如果复用 Task，写入 interaction/channel/账务快照及状态；GET/cancel/resume 必须按原 task/channel 查找；增加无人查询时的过期对账策略。 | 不能只依赖创建请求的 `gin.Context`、goroutine 或内存 `BillingSession`。                               |
| 消费/错误日志    | `model/log.go`、`service/log_info_generate.go`、`service/task_billing.go`                                                                           | 一次 Interaction 最终一条消费日志；记录计费来源为上游 usage 或配置估值；保留 request ID、upstream ID、原始 path 和可用终态 usage。 | 不把通用 Interaction 标成 `is_task`，不逐 SSE chunk 或每次 GET 记消费。                                |
| 管理端账单读取    | Java `LogsServiceImpl`、`BillingDetailService`、`LogsVo`、导出服务；Admin `bill-beta-columns.ts`；CLI `expenseService.ts` / Expenses 页面                    | 只有在新 usage 或账单字段需要显示时扩展映射与显示；核对公开/管理员权限及旧记录兼容。                                                  | Java 不成为账务写入方；不把 `admin_info` 暴露给客户。                                                   |

### 第一阶段边界

- 支持 Google Gemini 与 Vertex 渠道的 Interactions 协议转发；不承诺其它供应商原生支持该协议。其它渠道只有显式实现对应协议适配后才能接入，不由模型目录决定端点准入。
- 首期只结算上游可核实的模型 usage 和已存在、可审计的附加计费项；Interactions 的 `grounding_tool_count` 仅在能映射到现有工具计价与明确单价时才计入。不推算工具服务器、外部 API、agent 执行环境的隐性成本，不把 `total_tool_use_tokens` 未经确认地叠加计费。
- `store/background` 的 interaction ID、渠道绑定和账务状态由网关可靠保存。客户按官方路径查询；不允许查询请求重新负载均衡到另一个渠道。
- 现有专用模型的旧分支在行为上冻结为兼容基线；本功能不得重算旧账、改写已落库日志，也不得改变历史价格快照的含义。
- 优先评估现有 Task/日志存储能否承载持久账务 journal/outbox；若现有结构不能满足 crash recovery、唯一 claim 和日志重放去重，新增表/唯一键是正确性要求，不应为了避免 schema 变更牺牲计费一致性。任何 schema 变化必须按 Go `AGENTS.md` 验证 SQLite、MySQL、PostgreSQL，并覆盖新库、代表性升级库与幂等启动。

## 实施顺序

1. 将 `/v1beta/interactions` 的通用请求适配从视频请求体转换中解耦；明确现有专用任务分支的兼容边界。
2. 在渠道分配之后按渠道类型实现 Gemini 原样转发与 Vertex URL/认证适配。
3. 规范同步、SSE、后台 Interaction 的 usage 归一化与 BillingSession 结算幂等。
4. 扩展消费日志 `other` 字段和 Java/Admin 读取展示，保持旧字段兼容。
5. 按验收条件验证渠道路由、上游请求/响应、预扣退款、重复结算和日志关联；若有 schema 变化，执行三数据库矩阵。

## 当前状态

当前仓库已有按模型分支的 Interactions→Task 部分实现；本文所述“所有模型不按目录限制、按渠道类型适配的通用 Interactions”尚未实现。代码复核发现：现有同步原生 Interaction 的资金差额结算存在余额/日志不一致风险；官方 Vertex usage 新字段还未被当前 task parser 完整读取；消费日志 API 不是持久化 outbox，不能保障结算与日志 exactly-once。以上是实现前必须解决/明确的账务与用量问题，不可仅列作一般风险。另需定下无模型价格时的安全策略、后台账务 journal/outbox 载体、创建结果不确定时的恢复办法、既有特殊分支兼容方式及是否扩展客户端 usage 展示。

## 按代码确定的应对方法与改动边界

以下按当前实现给出建议落点。目标是把通用 Interactions 从“视频请求转换 + task adaptor”中分离，但沿用已有的渠道分配、模型映射、价格配置、配额保护和日志基础设施；不全局改造普通视频或 Gemini/Vertex GenerateContent。


### 主要风险与对应处理

| 风险（代码依据）                                                                                                                                                                                                           | 应对方法                                                                                                                                                                                     | 改动边界                                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 请求语义被视频转换破坏：`middleware.NativeInteractions` 调用 `convertNativeInteractionRequest`，对未知模型抽取 `input`、拒绝 audio，并转换成 `{model,prompt,metadata}`；之后 POST 路径改写为 `/v1/video/generations`。                                    | 在相同的官方 POST 路由上加入通用 Interaction 分流标记；通用分支保存原始 body/path 并直接进入 Interaction handler，不调用视频字段转换。                                                                                             | 限定 `middleware/native_interactions.go` 与 `controller/native_interactions.go` 的 native Interaction 分支；保留已支持 Omni/Lyria/Veo 既有转换行为，不能修改 `/v1/video/generations` 与 `/v1/videos`。分流不得再用模型目录判定准入；只可用已存在专用适配所需的兼容条件保留旧行为。 |
| 上游适配早于最终渠道决策，或错误复用 Vertex TaskAdaptor：`relay.GetTaskAdaptorForRequest` 在 `NativeInteractions + Vertex + Lyria/已知 GenerateContent 模型` 条件下选择专用 adaptor；Gemini 目前不走此 Vertex adaptor。                                  | 将通用 Interaction 上游适配放到渠道分配和 model mapping 之后，以 `info.ChannelType` 决定 Gemini 原样转发或 Vertex 资源 URL/认证转换；原始客户端 model 与映射后的上游 model 分别保留。                                                     | 不扩大 `isNativeInteractionTaskRequest` 的模型名单，也不把 Vertex URL 逻辑塞进普通 Gemini GenerateContent adaptor。优先新增独立 relay/adaptor 分支并在 `controller/native_interactions.go` 接入；`relay/relay_adaptor.go` 只保留既有专用任务选择。              |
| 现有计费的准入与结算仍依赖模型能力名单：`isNativeGenerateContentBilling` 调用 `IsNativeInteractionGenerateContentModel`；`nativeGenerateContentUsageQuota` 只对命中条件且有成功 `TaskInfo` 的请求覆盖最终 quota。                                           | 将“是否进入 Interactions 计费”改为协议/relay-mode 属性；实现独立的 Interaction usage parser/normalizer，将可信终态 usage 转成通用账务用量并计算最终 quota；usage 缺失/无法解释时保留现有预扣估值并标明来源，不按 0 计费或挂起。 | 修改范围仅限新增通用 Interaction 账务路径和现有特殊分支的明确分流；不要删除专用模型校验/既有公式，不改变 `/v1/video` 计费公式。usage 解析不能假设所有模型都有相同模态字段；字段缺失与字段显式为零必须区分。                                                                                              |
| 预扣费依赖已有价格：`RelayTaskSubmit` 先调用 `ModelPriceHelperPerCall`，随后 `PreConsumeBilling`；未配置模型可能按 `AcceptUnsetRatioModel` 策略报错，且任务预扣默认不由上游实际 token usage 推导。                                                               | 不绕过配额安全：新增 Interaction 之前明确渠道级/模型级保守预扣价格策略，按输入与受限的最大输出预算预扣；用 `BillingSession.Reserve` 支持同步过程中发现更高预算时追加 reserve；终态按规范化 usage 结算。无安全价格/上限时返回可识别的 pricing configuration error，不向上游发送不可收费请求。 | 不修改 `ModelPriceHelperPerCall` 的全局未知模型语义，也不把未配置价格默认为免费/固定最终收费。需要新增价格配置时限定 Interaction 计价策略，并遵守 `PriceData.AddOtherRatio`、`common.QuotaFromDecimalChecked` 等既有边界。                                                     |
| `SettleBilling` 的资金源约定容易误用：有 `BillingSession` 时它调用 `Billing.Settle(actualQuota, true)`；`skipFunding=true` 表示调用前资金来源已由 handler 结算。现有 `service/quota.go` 示例只可参考钱包路径，`PostTextConsumeQuota` 的订阅路径也未在本函数中提交 subscription delta，不能作为订阅结算正确性的证据。 | 新同步 handler 复用 `PreConsumeBilling` 与 `BillingSession` 的 token 结算能力，但必须按资金来源选择并验证真实资金 delta API：钱包余额、订阅余额、用户/渠道用量统计、token quota 各恰好更新一次，之后生成账单日志。不要把某个 handler 的组合调用直接视作通用标准结算链。 | 限定 Interaction handler/账务服务，不改 `BillingSession` 公共语义。钱包/订阅差额都必须有可验证的原子性与重试语义；未实现幂等资金操作和持久 journal 前，不承诺进程崩溃后的 exactly-once。 |
| 后台会话不能跨进程恢复：`BillingSession` 在 `RelayInfo` 中是内存对象；`buildSubmittedTask` 保存了来源、token、价格上下文，但没有完整的预扣/settlement journal。任务轮询有基于状态/额度 claim 的结算防重模式（如 `controller/task_video.go` 按 `quota=0` 原子 claim），但它是视频结算路径。      | background/store 要将预扣金额、资金来源 ID、token ID、价格/映射快照、上游 interaction ID、终态 usage、结算状态与幂等键持久化；用事务/条件更新 claim 保证只有一个结算者。服务重启后按持久状态恢复，不把内存 `BillingSession` 当恢复机制。                               | 首选评估 `tasks.private_data`/任务行是否可承载；若其状态模型无法表达“提交未知、待 usage、结算中、结算完成、退款完成”，新增专用状态存储优于复用视频 `quota` sentinel。避免为了通用 Interactions 改写视频 task polling/billing。若新增 schema，执行三数据库与升级验证。                                     |
| 创建 POST 的失败重试可能重复上游副作用：`controller/relay.go` 有 task channel retry/retryable error 流程；上游已创建但响应丢失时换渠道再发会新建 Interaction。                                                                                              | 对 Interaction 创建结果增加“明确失败 / 已确认创建 / 提交结果未知”区分。只有明确未创建的可重试错误允许换渠道；可能已创建的网络断开/响应解析失败保留 uncertain 状态并用原渠道、原幂等键查询/恢复。                                                                        | 在 Interaction 专用 retry policy 中短路普通 task retry；不改变其他 task 的重试行为。Vertex/Gemini 上游若不提供可用幂等键，则网关生成请求关联键并记录不确定状态，不能声称 exactly-once。                                                                                     |
| SSE 分片、客户端取消与最终 usage 生命周期不一致：当前 controller 有 `waitNativeInteraction`/`writeNativeSSE` 的响应路径，但它建立在任务提交/查询结果上，不代表通用原生 SSE 的计费完整。                                                                                    | SSE 解析器只旁路收集终态及 usage，事件原样写客户端；客户端断开或 usage 缺失时仍按已建立的配置估值/预扣兜底，不按零退款。                                                                                                         | 修改只在新的 Interaction stream handler；不让通用 SSE 被现有 Omni stream 特判误识别，不按每个 event/chunk 记消费日志。                                                                                                                            |
| 日志协议有不同 writer：普通同步消费可经 `service/quota.go` → `model.RecordConsumeLog`；`service.LogTaskConsumption` 固定 `is_task=true` 并更新任务统计。Java/Admin/CLI 只映射已知字段。                                                               | 复用 `RecordConsumeLog` 的底层消费日志协议，新增明确的 Interaction log builder/字段契约；同步扩展 Java 映射和 UI/export 仅限新增字段确需客户可见时。后台一次终态最多一条消费日志，和账务结算使用同一 idempotency key。                                       | 通用 Interaction 不调用 `LogTaskConsumption`，除非它确属当前既有专用任务兼容分支；不修改旧日志读取语义，不把诊断信息放到客户可见字段。                                                                                                                                |

### 建议的实施切面

1. 入口只改 Interactions：`router/video-router.go` 保留现有复数 route；`middleware/native_interactions.go` 将“专用兼容请求”与“通用 raw Interaction”分开。通用分支不可改写 `Request.URL.Path` 到视频接口，也不可改写 request body；把客户端 path、原始 body 和 Interaction 执行模式存入 request context/relay info。
2. 新增独立 Interaction relay mode/handler：在 `controller/native_interactions.go` 处理同步、SSE、background/store；用现有 `middleware.AssetResolveChannel`/`Distribute` 选渠道。渠道选定后再调用 Gemini/Vertex transport adapter。Gemini 不做 Vertex 转换；Vertex 构造官方资源路径，并明确项目/location 来源和允许值。不要通过“模型是否在支持列表”选择 adapter。
3. 复用可靠通用基础设施：沿用 `helper.ModelMappedHelper`、`PreConsumeBilling`、`BillingSession.Reserve` 和 quota saturation helper；**不要直接照搬 `SettleBilling` 的 `skipFunding=true`**。按资金来源实现并测试结算：钱包应以原子操作更新余额差额与实际用量统计，再让 BillingSession 只处理 token delta；订阅必须调用已核实的真实资金差额接口（可能是 `BillingSession.Settle(actual, false)`，但须确认当前 Funding 实现和调用契约），并保证统计只更新一次。新增 Interaction usage 归一化与价格计算，不复用视频秒数/分辨率公式。同步和异步共用一套资金源语义，但异步必须持久化/幂等。
4. 后台先做状态机，不直接借用视频结算：创建本地 pending record 与预扣状态后发送 upstream；确认返回 interaction ID 后落定渠道绑定。查询/取消按本地记录命中固定 channel。对创建结果不确定的情况存 uncertain，而不是释放预扣后允许无条件重试。结算 journal 用条件 claim；每类资金 effect 有幂等执行记录；消费日志由持久 outbox 投递，日志失败恢复不能再次执行资金扣款。
5. 日志先定义 contract 再接展示：写入 Java 已识别的聚合/flat usage 和管理员诊断字段，并保持既有 native Interaction 的 `is_task`、request ID/upstream ID 契约；`RecordConsumeLog` 只作现有 writer，不作为 outbox。确定 Java DTO/导出、Admin 与 CLI 是否必须展示新 modality。若不要求客户可见，可在 outbox payload 持久化原始官方 usage/规范化 usage，公开 `other` 只写兼容字段；若要求展示，则 Go/Java/Admin/CLI 一起扩展，不只改 Go JSON。

### 明确不在本次改动内

- 不改 `/v1/video/generations`、`/v1/videos` 的 handler、模型规则、价格公式、轮询和响应。
- 不删除或泛化当前 Lyria/Omni/Veo 的专用 adaptor、已支持模型行为、历史 task 查询格式和账单公式；只为路由分流增加必要条件。
- 不改 `ModelPriceHelperPerCall` 对所有 API 的未知价格行为，不把“无 endpoint 模型白名单”扩大解释成“无价格也必须调用上游”。Interactions 如需承接尚未配置的模型，必须提供明确、可审计且可预扣的 Interaction 级价格策略。
- 不把 Vertex 的 path/认证转换应用到 Gemini channel 或普通 Vertex GenerateContent。
- 不将通用 Interaction 假设为视频 task，不使用视频用量字段作计费主数据；新增日志的 `is_task` 分类要兼容现有 Interactions 消费方，不能擅自改变已支持模型分支。
- 不在日志中保存原始鉴权信息、完整敏感 request/response；不把 `admin_info` 暴露给客户。
- 第一阶段不扩展其他供应商的 Interaction 兼容协议，不推算未接入计费器的外部工具执行成本。

### 需产品/实现决策，而非代码推断

- 对没有模型价格记录但被渠道明确支持的模型，采用渠道级保守价格、需管理员配置后才能启用，还是允许免费模型标记；代码不能安全推断业务选择。
- Vertex 项目 ID 的具体渠道配置字段/存量字段映射，需实现前核对现有 Vertex channel schema；端点规范本身不待产品选择，首期固定使用官方 `aiplatform.googleapis.com/v1beta1/.../locations/global/interactions`。CMEK 多区域不属于首期范围。
- Gemini/Vertex 对创建请求是否支持可用的幂等键，以及 uncertain 状态的恢复查询机制；没有上游保证时只能保证网关账务防重，不能保证上游 exactly-once。
- 客户端是否需要 Java/Admin/CLI 展示完整模态 usage，还是仅保留管理员审计数据。
