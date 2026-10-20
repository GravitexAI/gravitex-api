# Mock AI 渠道类型 — 设计文档

- 日期：2026-10-20
- 状态：已定稿，待实现
- 涉及仓库：`gravitex-api`（Go 后端 + `web/classic` + `web/default`）、`gravitex-api-admin`（Vue 管理端）

---

## 1. 意图与背景

我们需要一个**不依赖任何真实上游**、但在「用量数字」和「响应时延」上可以被精确编排的渠道类型。

动机：

- 计费与日志链路的验证目前只能靠真实流量。要构造一个特定形状的 usage（大额缓存读取、超长输出、开启思考 token）成本极高，且完全受制于上游恰好返回什么。
- 联调、压测、演示场景下，真实上游会烧额度、会抖动、会限流，噪音掩盖了被测对象本身。
- 需要构造特定 token 量级或特定耗时的历史日志数据（验证报表分档、导出账单字段、超时告警）时，没有可控的生产手段。

Mock AI 渠道用来填这个洞：**请求进来不发往任何地方，而是就地合成一份与真实上游逐字节同构的响应，再让它流过完整的、未经改动的解析 → 聚合 → 计费 → 写日志链路。**

---

## 2. 目标

1. 新增渠道类型 **66 = `Mock AI`**，在渠道管理中常规展示，无开关门禁。
2. 支持三个端点：`/v1/chat/completions`、`/v1/responses`、`/v1/messages`。
3. 按渠道、按模型可配置用量 token 范围与响应时延范围，请求时在范围内随机取值。
4. 合成的响应**与真实上游逐字节同构**，因此能流过现有的全部 relay 代码而无需任何特判分支。
5. 完整走现有的预扣费 → 结算 → 写 `logs` 链路，真实扣减用户额度。

---

## 3. 非目标

明确不做，不要"顺手"加上：

- 其他端点：图片生成、embedding、rerank、audio、video、Gemini 原生格式、各类异步 task 平台、`/v1/messages/count_tokens`、realtime websocket。
- key / base_url 的内容校验。两者只要**非空**即可，不比对、不返回 401/403。后端不写死任何默认 key。
- 环境变量开关。渠道类型直接展示。
- 错误注入（按概率返回 429/500/断流）。
- 多模态 token（audio / image / video tokens）。
- 把 usage 数字与实际回答文本长度对齐。两者**有意**脱钩。
- **响应正文不随请求特性变化**：永远是一段纯文本。即使配置开启了思考 token，正文里也不会出现 thinking / reasoning 块；即使请求带了 `tools`，也不会产出 `tool_calls` / `tool_use`；`finish_reason` 固定 `stop`，`stop_reason` 固定 `end_turn`。
- `message_start` 与 `message_delta` 的 usage 不支持分别配置，两处数字恒自洽。

## 3.1 反目标（即使功能"能跑"，出现下列任一情况也算这次做失败）

- **回答文案里出现 mock / 测试 / 占位 / AI 自我介绍类字眼。** 文案必须读起来就是一条正常的、独立成立的回答。
- **合成的字节与真实上游不同构**，导致 relay / 计费侧需要为 Mock 渠道加 `if channelType == MockAI` 之类的分支。
- **自行构造最终的 `dto.Usage` 或自行写 SSE 下发给客户端**，绕过真实的响应处理器。那样 Mock 就失去了"让流量流过真实链路"的全部意义。
- **Mock 渠道的模型混入真实用户的负载均衡池**（见 §7）。

---

## 4. 约束

来自 `AGENTS.md` 和项目既有约定，实现时必须遵守：

- JSON 的序列化/反序列化一律走 `common.Marshal` / `common.Unmarshal` 等包装函数，不直接调用 `encoding/json`。
- `relaykit/` 必须保持独立可构建。Mock 的配置结构放进 `relaykit/dto/channel_settings.go`（与 `AdvancedCustomConfig` 同处），**不得引用 root module 的任何包**。改动后用 `cd relaykit && GOWORK=off go build ./...` 验证。
- 本设计**不新增数据库列**：配置整体序列化进已有的 `channels.channel_other_settings` JSON 字段。因此不涉及迁移，但仍需在三种数据库上跑一次启动，确认 `AutoMigrate` 没有被意外触发。
- 日志 content、SysLog、error message 一律英文。
- 渠道类型号取 66（65 已被 `ChannelTypeTaskPlugin` 占用），`ChannelTypeDummy` 顺延到 67。遵循"吸收官方新渠道一律往 63+ 顺延"的既有规则。
- 不得修改 `new-api` / `QuantumNous` 相关的任何标识。
- 新增/重写的 Go 测试使用 `testify` 的 `require`（致命断言）与 `assert`（非致命值检查）。

---

## 5. 后端设计

### 5.1 注册新渠道类型

全部是既有的扩展位，照 `ChannelTypeNewAPI` 的样子加即可：

| 位置 | 改动 |
|---|---|
| `constant/channel.go` | `ChannelTypeMockAI = 66`；`ChannelTypeDummy` 顺延；`ChannelBaseURLs` 补第 66 项空串；`ChannelTypeNames` 补 `"Mock AI"` |
| `constant/api_type.go` | 新增 `APITypeMockAI`，插在 `APITypeNewAPI` 之后、`APITypeDummy` 之前 |
| `common/api_type.go` | `ChannelType2APIType` 加 case |
| `relay/relay_adaptor.go` | `GetAdaptor` 加 case，返回 `&mockai.Adaptor{}` |
| `common/endpoint_type.go` | `GetEndpointTypesByChannelType` 加 case，返回三个端点的**全集**作为无配置时的兜底 |
| `model/pricing.go` | `getPricingEndpointTypesForAbility` 加 Mock 分支：读该渠道配置里实际勾选的端点，照 `ChannelTypeAdvancedCustom` 的写法（含按 channelId 预加载配置的模式，注意其中记录的 `updatePricingLock -> channelSyncLock` 锁序约定） |

### 5.2 配置结构

挂到 `ChannelOtherSettings` 上，与 `AdvancedCustom` 并列：

```go
// relaykit/dto/channel_settings.go
type MockAIConfig struct {
    Endpoints MockAIEndpoints               `json:"endpoints"`
    Default   MockAIModelProfile            `json:"default"`
    Models    map[string]MockAIModelProfile `json:"models,omitempty"`
}

type MockAIEndpoints struct {
    ChatCompletions bool `json:"chat_completions"`
    Responses       bool `json:"responses"`
    Messages        bool `json:"messages"`
}

type MockAIRange struct {
    Min int `json:"min"`
    Max int `json:"max"`
}

type MockAIModelProfile struct {
    TextInput     MockAIRange  `json:"text_input"`
    TextOutput    MockAIRange  `json:"text_output"`
    CacheRead     MockAIRange  `json:"cache_read"`
    CacheCreation MockAIRange  `json:"cache_creation"`           // 仅 /v1/messages 生效
    CacheCreation1hRatio float64 `json:"cache_creation_1h_ratio"` // 0~1，默认 0 即全部算 5m
    Reasoning     *MockAIRange `json:"reasoning,omitempty"`      // nil = 关闭
    LatencyMs     MockAIRange  `json:"latency_ms"`               // 端到端总用时
    FirstTokenMs  MockAIRange  `json:"first_token_ms"`           // 流式首字
}
```

**模型查找键用「重定向之前的原始模型名」**，即用户在请求里写的那个名字、也就是渠道「模型」字段里列出的那个名字。先查 `Models[原始模型名]`，未命中回落到 `Default`。

> 这是设计者的决定，需要说明理由：更"像真实渠道"的做法是用重定向后的上游模型名，但 Mock 并不存在真实上游，而前端配置表格的下拉框只能列出渠道「模型」字段里的原始名。若键用上游名，一旦管理员配了 `model_mapping`，查找就永远落空、静默回落到默认档位——配置看着生效了其实没生效。用原始名可以杜绝这类静默失效。

**保存时校验**（`Validate()`，后端与两个前端各自实现一遍）：

- 每个区间 `Min >= 0` 且 `Min <= Max`。
- `CacheCreation1hRatio` 在 `[0, 1]` 内。
- `LatencyMs.Min > FirstTokenMs.Max` —— 取严格大于、且比的是"下限 vs 上限"，这样任意一次随机抽样都必然满足"总用时 > 首字时间"。
- 三个端点至少勾选一个。
- 区间上界设防呆硬上限：token 取 `maxTokensLimit`（`relay/helper/valid_request.go`）同量级，时延取 600000ms。这是防止有人填出一个天文数字 token 把某个用户额度一次清零，也防止填出半小时的 sleep 把连接占死。

### 5.3 适配器 `relay/channel/mockai/`

**核心手法：合成"上游"字节，然后把真实的响应处理器放在它后面。**

不要自己写 SSE、不要自己构造 `dto.Usage`。正确做法是：

1. `DoRequest` 返回一个**真的 `*http.Response`**（`StatusCode: 200`，`Header` 带正确的 `Content-Type`——流式 `text/event-stream`、非流式 `application/json`），它的 `Body` 是一个自定义的 `io.ReadCloser`，按时间节奏吐出合成的上游字节。

   `Content-Type` 必须设对：`relay/compatible_handler.go` 和 `relay/claude_handler.go` 都会用它来判定 `info.IsStream`。

2. `DoResponse` **委托给对应格式的真实处理器**——Claude 路走 `claude` 包的响应处理，OpenAI 路走 `openai` 包的，Responses 路走 responses 的。Mock 适配器自己不解析、不聚合、不计算用量。

这样做的收益：

- `StreamScannerHandler` 正常运行，`info.SetFirstResponseTime()` 被正常调用，日志里的 `frt`（首字时间）字段是**真实测量**出来的，而不是编的。
- usage 的解析与跨事件聚合走的是生产代码本身。
- 下发给客户端的字节由真实处理器产出，天然与真实渠道一致。
- 不需要碰 `BillingUsage`：同格式直连时真实适配器怎么做，Mock 就怎么被对待。

> 注：同格式直连时，上游 SSE 与下发给客户端的 SSE 本就是基本同一批字节，所以这个方案相对"自己写 SSE"并没有增加多少字节构造的工作量，却白赚整条解析链路。

**节奏化 Body reader 的行为**：

- 非流式：阻塞 `总用时` 后，一次性返回完整 JSON 响应体。
- 流式：阻塞 `首字时间` 后吐出起始事件，随后把正文按字符切片，在 `总用时 − 首字时间` 的窗口内**均匀铺开**依次吐出，最后吐出收尾事件。切片数量与 usage 里的 token 数字无关。
- 所有阻塞必须响应 `c.Request.Context()` 的取消——客户端断开时要立刻停，不能把 goroutine 挂满整个 latency 窗口。
- **实现时需确认** `StreamScannerHandler` 是否存在空闲超时看门狗，以及其阈值与 600000ms 的 latency 上限是否冲突。若冲突，以看门狗阈值为准收紧硬上限。

**渠道测试与健康检查路径必须跳过 latency sleep。** `testChannelForHealthCheck` 按墙钟耗时判定并自动禁用渠道，Mock 若配了较长 latency 会被定时任务直接杀掉，同时每轮为每条渠道烧一个纯睡眠的 goroutine。通过 context key 识别测试路径并跳过等待。这是设计者的决定——另一条路（要求运维关掉自动禁用）把一个代码能解决的问题推给了人。

其余方法：`Init` / `SetupRequestHeader` 空实现；`GetRequestURL` 返回空串；`Convert*Request` 原样返回；`GetModelList` 返回空（模型由管理员自己填）；`GetChannelName` 返回 `"mock_ai"`。

**端点未勾选时的拒绝形态**：返回 **404 `endpoint_not_found` 且不记错误日志**，照 `relay/responses_handler.go:30-42` 的先例（那里把这种情况定性为"预期内的端点能力缺失，不是供应商故障"）。不要走 `Convert*Request` 返回 error 那条路——那会变成一条 500 并污染错误日志，对一条专门用来压测的渠道来说噪音会迅速堆积。

### 5.4 合成字节里的用量字段

配置语义统一为：**`TextInput` 表示「不含任何缓存部分的纯文本输入 token」。** 两家 API 对缓存 token 的归属定义不同，由各端点在合成字节时各自换算：

**`/v1/chat/completions`**

```
prompt_tokens                              = TextInput + CacheRead
prompt_tokens_details.cached_tokens        = CacheRead
completion_tokens                          = TextOutput
completion_tokens_details.reasoning_tokens = Reasoning（开启时）
total_tokens                               = prompt_tokens + completion_tokens
```

**`/v1/responses`**

```
input_tokens                               = TextInput + CacheRead
input_tokens_details.cached_tokens         = CacheRead
output_tokens                              = TextOutput
output_tokens_details.reasoning_tokens     = Reasoning（开启时）
total_tokens                               = input_tokens + output_tokens
```

**`/v1/messages`**

```
input_tokens                               = TextInput        ← 并列关系，不含缓存
cache_read_input_tokens                    = CacheRead
cache_creation_input_tokens                = CacheCreation
cache_creation.ephemeral_1h_input_tokens   = round(CacheCreation × CacheCreation1hRatio)
cache_creation.ephemeral_5m_input_tokens   = CacheCreation − 上面那个
output_tokens                              = TextOutput
```

保留 1h 占比这个配置位，是因为 1h 缓存创建的计费倍率与 5m 不同，是一条真实的计费维度，少了它就测不到。默认 0（全 5m）。

**流式下 usage 的位置**：

- OpenAI / Responses 侧：**无条件在流中下发 usage**，不要求客户端传 `stream_options.include_usage`。这是与真实 OpenAI 行为**有意的一处偏离**，目的是让任何客户端在流式下都能拿到用量。合成字节时视作该参数恒为开启。
- Claude 侧：按真实协议在 `message_start` 与 `message_delta` 中分别下发，两处数字自洽。

### 5.5 固定回答文案池

- 位置：`relay/channel/mockai/` 下一个独立的 Go 源文件，一个包级 `[]string` 常量切片。不引入 txt / json 资源文件，不用 `go:embed`。
- 数量：100 条，每条 ≤ 100 字。
- 内容：科普、冷知识、生活小技巧、笑话等，中英文混排均可。
- **每一条都必须读起来是一个独立成立的正常回答**，不出现 mock / 测试 / 占位 / 示例 / "我是 AI 助手" 之类的字眼，也不要出现明显的编号或模板痕迹。
- 选取方式：每次请求随机取一条。

---

## 6. 前端设计

三处都要加渠道类型下拉项，否则在没加的那个界面里编辑这条渠道，类型会显示成 Unknown 并可能在保存时被洗掉。

| 前端 | 范围 |
|---|---|
| `web/classic` (`src/constants/channel.constants.js` + 渠道编辑页) | **完整编辑器** |
| `gravitex-api-admin` (`apps/web-antd/.../channel/channel-form.vue`，`CHANNEL_OPTIONS`) | **完整编辑器** |
| `web/default` (`src/features/channels/constants.ts`) | 仅补类型名映射；对 Mock 渠道显示只读提示，引导到上面两处配置 |

### 编辑器内容（classic 与 Vue 管理端形态保持一致）

1. **端点开关**：三个勾选框（chat/completions、responses、messages），默认全开。
2. **默认档位**：一组区间输入，作为未单独配置模型的兜底。
3. **按模型配置表格**：一行一个模型。模型名从该渠道「模型」字段里已填的列表中下拉选择。列为：文本输入、文本输出、缓存读取、缓存创建、1h 占比、（可选）思考输出、总用时、流式首字。区间统一用 `min ~ max` 两个数字输入框。
4. **缓存创建 / 1h 占比**两列仅在勾选了 `/v1/messages` 时可编辑，否则置灰并提示"仅 /v1/messages 生效"。
5. **思考输出**默认关闭，一个开关打开后才显示区间。
6. **保存前校验**与 §5.2 一致，错误就地红字提示，不要等后端报错。
7. **醒目警告条**与 **default 分组二次确认**：见 §7。

---

## 7. 风险与运维约束

**风险一：Mock 渠道会参与真实模型的负载均衡。**

渠道一旦启用，它声明的模型就进入 `abilities` 表，照常参与分组内的渠道选择。如果管理员给 Mock 渠道填了一个真实模型名又放在了默认分组里，**真实用户的请求有概率被路由到 Mock 渠道，拿回一条冷知识，并且照样被扣费**。

缓解（本期做这两条）：

1. 两个编辑器顶部固定一条醒目警告：**"Mock 渠道必须放在独立分组，不要与真实渠道共用分组，否则真实用户请求会被路由到此渠道。"**
2. 保存时若该渠道的分组包含 `default`，弹二次确认。**不硬阻止**——管理员可能就是想在一个隔离环境里这么用。

（记录但不做：后端硬性拒绝 Mock 渠道使用 `default` 分组。过于武断，且会挡住合法的隔离环境用法。）

**风险二：健康检查自动禁用。** 已在 §5.3 通过"测试路径跳过 sleep"解决。实现后需实际验证一轮定时健康检查不会禁用 Mock 渠道。

**风险三：额度可能被打成负数。** 预扣费按真实请求的 prompt token 计算（很小），结算按配置的输出 token 计算（可能很大），两者的差额在结算时补扣，可以把用户额度打成负数。**这是预期行为，不做 clamp**——真实渠道在上游返回远超预期的用量时也是这个行为，而且这正是 Mock 渠道值得观察的现象之一。§10 对此有明确断言。

**其他：**

- Mock 渠道的 `logs` 记录与真实请求在外观上无法区分。若后续需要在对账时剔除，需要额外的标记手段——本期不做，但实现时注意不要堵死这条路。

---

## 8. 测试要求

按 `AGENTS.md` 的后端测试规范，只写保护真实契约的测试：

1. **端到端用量测试（最重要）**：给定一组固定的 profile 取值，让合成的上游字节流过**真实的**响应处理器，断言最终得到的 `dto.Usage` 完全正确。三个端点 × 流式/非流式共 6 个场景。这条同时锁住了 §5.4 的换算表和"合成字节与真实上游同构"这个核心契约——如果字节合成错了，真实处理器会解析失败或解析出错误的数字，测试立刻暴露。
2. **配置校验测试**：表驱动，覆盖 `Min > Max`、负数、`LatencyMs.Min <= FirstTokenMs.Max`、`CacheCreation1hRatio` 越界、三端点全未勾选、超出硬上限。
3. **模型查找测试**：配了 `model_mapping` 时，按原始模型名仍能命中 `Models` 中的条目（这条直接保护 §5.2 里那个决定）。
4. **文案池测试**：断言恰好 100 条、每条非空且 ≤ 100 字、无重复。
5. **端点门禁测试**：未勾选的端点返回 404 且不记错误日志。
6. 随机取值与时延本身不写测试（无法稳定断言）；实现时把"取随机数"和"等待"抽成可注入的来源，让上述测试跑在确定性取值与零等待上。

---

## 9. 留给实现者决定的事

- 随机数来源与等待行为的具体注入方式（函数字段、接口、还是包级可替换变量）。
- 节奏化 `io.ReadCloser` 的具体实现（预先切好片 + timer，还是在 `Read` 里现算），只要满足"均匀铺满剩余窗口"且响应 context 取消即可。
- `DoResponse` 委托真实处理器的具体接法（内嵌真实 adaptor、还是直接调用对应包的导出函数）。
- 识别"渠道测试/健康检查路径"的 context key 命名与设置位置。
- 两个前端编辑器的具体控件选型，各自跟所在项目的既有风格一致即可。
- 100 条文案的具体内容。

---

## 10. 验收

**前置条件**：Mock 渠道要用的模型名必须先在「分组与模型定价设置」里配好倍率，否则请求会在 `ModelPriceHelper`（`relay/helper/price.go`）就失败，根本到不了适配器。

步骤：

1. 建一条 Mock AI 渠道，填任意 base_url 和 key，配两个模型（一个走默认档位、一个单独配），放独立分组。
2. 分别用 `/v1/chat/completions`、`/v1/responses`、`/v1/messages` 的流式与非流式各打一次，共 6 次。
3. 每次核对：响应结构与真实 API 一致；usage 数字落在配置区间内；缓存字段的归属关系符合 §5.4；端到端耗时落在总用时区间内。
4. 核对日志里的 `frt`（首字时间）落在首字区间内——这一条同时验证了"流量确实流过了真实的 `StreamScannerHandler`"。
5. 核对 `logs` 表写入正确、用户额度按预期扣减、管理端账单页展示正常。
6. 配一个超大输出 token 的模型打一次，确认额度被打成负数且无 panic、无错误计费。
7. 配一个长 latency 的模型，跑一轮定时健康检查，确认渠道没有被自动禁用。
8. 三种数据库各启动一次，确认无 `AutoMigrate` 异动。
