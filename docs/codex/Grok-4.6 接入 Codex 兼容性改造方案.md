# Grok-4.6 接入 Codex 兼容性改造方案

> 适用仓库：`gravitex-api`  
> 文档日期：2026-09-16  
> 范围：梳理已有 Codex 接入能力，并定义 `grok-4.6` 通过 Codex Desktop/CLI 调用时的兼容改造边界。本文不包含部署或数据库执行。

## 1. 结论

仓库已经具备 Codex 接入的主链路：Codex 风格模型目录、`/v1/responses` 请求入口、模型/渠道路由、OpenAI Responses 原样或转换转发、流式与非流式响应处理、用量计费与请求日志。

`grok-4.6` 当前的阻塞点不是模型选择、鉴权或路由，而是 Codex 发出的部分工具定义使用 `tools[].type = "custom"`；已观察到的 Grok 上游 Responses 实现返回 422，声明只接受 `function`、`web_search`、`x_search`、`image_generation`、`collections_search`、`file_search`、`code_execution`、`code_interpreter`、`mcp`、`shell`、`tool_search`。

### 当前实施状态（2026-09-16）

- 已在 `relay/channel/xai` 增加 **channel=xAI 且映射上游模型=grok-4.6** 的窄门：只在该组合下丢弃 xAI 不支持且不可安全转换的 Codex `custom`/`namespace` 工具、删除 xAI 不支持的 `external_web_access` 请求级字段，并从远程 MCP 删除 xAI 不支持的 `connector_id` 与 `require_approval`。过滤同时在 converter 与 xAI 发送前执行，因而覆盖 `PassThroughRequestEnabled` / `PassThroughBodyEnabled`。其他模型不会解析或重新编码其 body 或 `tools` 字段。
- `web_search`、`x_search`、`code_interpreter` 和 `text.format` 维持原始 Responses 请求透传；新增测试校验其字段未被此门修改。
- 已修复同一精确组合的 `/v1/responses/compact` 回包处理，使其使用 Responses compact 格式读取 usage。其他 xAI 模型仍维持原有分支。
- 尚未猜测 Codex catalog 的实验字段或伪造 shell 工具转换；在确认客户端字段语义和 xAI shell 契约前保持不变。

### 2026-09-16 Responses schema 审计结果

发送前适配不再按上游 400/422 逐项补丁。对精确的 xAI/Grok 4.6 组合，顶层仅保留 xAI `/v1/responses` schema 中的字段：`model`、`input`、`instructions`、`max_output_tokens`、`max_tool_calls`、`metadata`、`parallel_tool_calls`、`previous_response_id`、`prompt_cache_key`、`reasoning`、`safety_identifier`、`service_tier`、`store`、`stream`、`temperature`、`text`、`tool_choice`、`tools`、`top_logprobs`、`top_p`、`truncation`、`user`、`background`。`client_metadata`、`context_management`、`include`、`prompt_cache_options`、`prompt_cache_retention`、`stream_options`、`frequency_penalty`、`presence_penalty` 及任何未列出的 Codex/OpenAI 扩展字段均不发送至 xAI。

工具仅保留 xAI 已声明可接受的类型：`function`、`web_search`、`x_search`、`image_generation`、`collections_search`、`file_search`、`code_execution`、`code_interpreter`、`mcp`、`shell`、`tool_search`。其中额外清理 xAI 明确拒绝的嵌套兼容字段：`web_search.external_web_access`、`web_search.search_context_size`、`web_search.user_location`、`file_search.filters`、`file_search.ranking_options`、`code_interpreter.container`；MCP 继续删除 `connector_id`、`require_approval`。`tool_choice` 只保留 `none`、`auto`、`required` 或指定 `function`，避免 Codex 的 `custom`/`shell` 等强制工具类型进入 xAI。

为避免通过了类型检查却因缺少 xAI 必填字段继续返回 422，发送前还验证 `function.name`/`function.parameters`、`file_search.vector_store_ids`、`mcp.server_label`/`mcp.server_url` 和 `shell.environment`。缺失时只移除该工具；不会构造猜测的字段或改写成另一种工具协议。

改造原则：

1. 不改全局 `ResponsesHelper` 对所有模型的通用转发语义。
2. 不将 `custom` 伪造为 `function`；两者输入格式和结果事件语义不等价。
3. 只在 Codex 模型目录构建阶段，对已确认不支持 `custom` 的模型关闭会触发它的客户端能力。
4. OpenAI 原生模型仍保留现有 `apply_patch`/自由格式工具能力，其他模型的现有调用不因 Grok 改造而改变。
5. 每一项“上游支持/不支持”的判断都必须按模型或渠道配置，不能按“第三方模型”做宽泛猜测。

## 2. 已有 Codex 接入能力

### 2.1 客户端配置与协议入口

Codex 使用 OpenAI Responses 协议。客户端以 `base_url + /responses` 请求网关，模型列表由 `GET /v1/models` 获取；Codex 客户端在查询中带 `client_version` 时，网关返回 Codex 专用目录，而普通 OpenAI 客户端仍返回既有 `object/data` 模型列表。

| 能力 | 实现位置 | 当前行为 |
| --- | --- | --- |
| `/v1/models` 路由 | `router/relay-router.go` | OpenAI 鉴权链路下的模型列表入口 |
| Codex 目录切换 | `controller/model.go` 的 `ListModels` | 仅 `client_version` 非空时输出 `{ "models": [...] }`，避免改变普通 OpenAI 客户端响应 |
| Codex 模型目录 DTO | `relaykit/dto/codex_catalog.go` | 定义 `slug`、上下文窗口、工具能力、推理档位、系统提示词等 Codex 字段 |
| 模型目录构建 | `service/codex_catalog.go` | 仅收录 chat/reasoning 且声明 `openai-response` 的模型 |
| 本地客户端配置说明 | `docs/codex/Codex 接入 Gravitex 配置指南.md` | 说明 `model_provider`、`wire_api = "responses"`、`model_catalog_json` 与认证方式 |

### 2.2 模型准入与目录数据保护

`BuildCodexCatalog` 不会把全部平台模型直接暴露给 Codex，而是依次校验：

1. 模型必须有扩展配置；
2. 主模式必须为 `chat` 或 `reasoning`；
3. `io_schema.endpoint_types` 必须包括 `openai-response`；
4. 必须有合法上下文窗口；
5. 输入模态只向 Codex 输出 `text`、`image`；数据库中的 `file`、`audio` 不会原样透传给 Codex。

OpenAI 官方模型使用 `codexOfficialWindows` 提供 Codex 实际会话窗口；其他模型使用扩展配置的 `context_window`。这能避免模型目录把不支持的端点、图像/视频/向量模型或错误窗口暴露给 Codex。

### 2.3 Responses 请求处理

| 阶段 | 实现位置 | 当前能力 |
| --- | --- | --- |
| 请求解析与基本校验 | `relay/helper/valid_request.go` | 解析 `dto.OpenAIResponsesRequest`，校验 `model`、`input`、最大输出 token |
| 入口分发 | `controller/relay.go`、`relay/responses_handler.go` | 将 `/v1/responses` 请求交给 Responses 处理器 |
| 端点能力校验 | `relay/responses_handler.go`、`common.SupportsOpenAIResponsesEndpoint` | 选中渠道不支持 Responses 时返回明确 404，不误转发到错误渠道 |
| 模型映射与推理后缀 | `relay/responses_handler.go`、`relay/helper` | 执行渠道模型映射与推理模型参数处理 |
| 渠道适配 | 各 `relay/channel/*/adaptor.go` | `ConvertOpenAIResponsesRequest` 按渠道原样转发或转为目标协议 |
| 参数禁用与覆盖 | `relay/common` | 非透传链路可删除渠道禁用字段并应用参数覆盖 |
| 请求透传 | `relay/responses_handler.go` | 全局或渠道 Body PassThrough 启用时保留客户端原始 JSON |
| 响应、用量与计费 | 各渠道 adaptor、`service`、`relaykit/relayconvert` | 支持响应转换、SSE/非流式处理、usage 归一化、日志和额度结算 |
| `/v1/responses/compact` | `router/relay-router.go`、`relay/responses_handler.go` | 单独入口；仅透传该端点允许的字段，避免把工具等非标准字段带给上游 |
| Codex Alpha Search | `router/relay-router.go` | 提供 `/v1/alpha/search` 路由；是否可用于某模型仍取决于模型目录和渠道能力 |

### 2.4 工具字段的现状

`dto.OpenAIResponsesRequest.Tools` 是 `json.RawMessage`。这是为了保留 Responses/MCP 工具的完整请求结构，而不是在网关入口将每一种工具类型强制收窄为 Go struct。

因此，当 Grok 选中 OpenAI 兼容渠道且未启用请求体透传时，默认 OpenAI Responses 适配器会把工具数组继续发送给上游；若上游不认识其中某个类型，错误会在上游解析请求体时产生。此次 422 正属于该边界。

## 3. Grok-4.6 已确认的兼容边界

### 3.1 422 的直接原因

已观察到的上游错误：

```text
tools[6].type: unknown variant `custom`
```

这表示请求已经正确抵达 `/v1/responses`，但上游拒绝了第 7 个工具定义。它不是 `web_search` 或 `x_search` 的失败：这两个类型出现在上游的“可接受类型”列表中，真正不被接受的是 `custom`。

### 3.2 `apply_patch_tool_type` 的影响

Codex 目录中的：

```json
"apply_patch_tool_type": "freeform"
```

会使 Codex 为模型注册自由格式补丁工具。该能力在请求侧表现为 `type = "custom"`，而 Grok 上游已明确不支持该工具类型。

当前仓库已经有正确的第一层隔离：`service/codex_catalog.go` 仅对 `codexOfficialWindows` 中的 OpenAI 原生模型设置 `ApplyPatchToolType`；`grok-4.6` 不应带该字段。对应测试为 `TestBuildCodexCatalogOnlyOffersFreeformApplyPatchToOpenAIModels`。

### 3.3 仍须验证的动态工具来源

删除 `apply_patch_tool_type` 后若仍有 `type = "custom"`，不能直接判定目录设置无效。Codex 还可能通过 Node REPL 或其他动态工具机制生成自由格式工具。目录 DTO 已包含：

```go
NodeReplDisabled bool `json:"node_repl_disabled"`
```

当前目录构造把它统一设置为 `false`。对于 Grok，下一步应使用一次经过脱敏的请求体日志确认 `tools[6]` 的完整对象、名称和来源，再决定是否将 Grok 的 `node_repl_disabled` 设为 `true`。

不能在没有该证据时同时关闭更多工具，因为这样会把“去掉 422”和“无必要地削弱 Codex 能力”混在一起。

## 4. Grok-4.6 所需改动

### 4.1 必须改：以模型能力为条件禁用 freeform apply_patch

**目标**：目录中 `grok-4.6` 不输出 `apply_patch_tool_type`，使 Codex 不再向 Grok 请求发送由该能力生成的 `custom` 工具。

**现有代码状态**：已实现。`service/codex_catalog.go` 只在模型位于 `codexOfficialWindows` 时设置该字段；`grok-4.6` 不在其中。

**部署/数据要求**：

1. 线上服务必须部署包含该逻辑的版本；
2. Codex 必须重新拉取服务端 `/v1/models?client_version=...`，或本地静态 `model_catalog_json` 必须删除 Grok 条目的该键；
3. 已创建的 Codex 任务可能保留旧模型能力快照，应新建任务验证；
4. 不得通过修改 `codexOfficialWindows` 来排除 Grok；该表只描述 OpenAI 官方模型窗口与官方能力。

**不可采用的方案**：在 `ResponsesHelper` 中对所有请求删除 `tools[].type = "custom"`。这会破坏 OpenAI 原生模型的 apply_patch 调用与工具调用生命周期，并可能导致模型请求了一个网关悄悄丢弃的工具。

### 4.2 条件改：若请求取证确认 Node REPL 也是来源，则仅对 Grok 禁用

**触发条件**：Grok 目录已没有 `apply_patch_tool_type`，新建任务发出的请求仍含 `tools[].type = "custom"`，且脱敏日志表明该工具来自 Node REPL/动态工具。

**建议改动**：为 Codex 目录构建增加“模型工具兼容能力”判定。例如只对明确不支持 `custom` 的模型设置：

```go
entry.NodeReplDisabled = modelName == "grok-4.6"
```

更适合长期维护的形式是从模型扩展配置读取显式能力，例如 `codex_capabilities.supports_custom_tools`，由模型/渠道数据决定，而不是把多个模型名散落在代码中。

**隔离要求**：

* 只改 Grok 对应目录条目；
* OpenAI 原生模型保留 `node_repl_disabled = false`；
* 其他第三方模型不得被默认关闭，除非各自有相同的上游错误证据；
* 变更后只影响 Codex 目录字段，不改变普通 OpenAI API、渠道调用、计费或现有任务插件。

### 4.3 长期改：把工具协议能力从“模型名特例”升为模型/渠道能力

`SupportsFunctionCalling` 只能表达一般函数调用，不足以表达 Responses API 的每种工具变体。建议后续在模型扩展或渠道设置中新增以下粒度的能力位：

| 能力位 | 控制的 Codex 目录/请求行为 |
| --- | --- |
| `supports_responses` | 是否可进入 Codex 目录（现有 `openai-response` 已承担） |
| `supports_function_tools` | 是否保留标准 `type = "function"` 工具 |
| `supports_mcp_tools` | 是否允许 MCP 工具定义 |
| `supports_custom_tools` | 是否允许 freeform apply_patch、Node REPL 等 `type = "custom"` 工具 |
| `supports_web_search` | 是否向目录声明搜索能力 |
| `supports_shell_tools` | 是否声明/保留 shell 类工具 |

默认策略应采用保守准入：没有明确能力证据时不宣称该能力；但引入此能力位必须先为已有模型配置迁移值，防止默认值把现网模型工具全部关掉。

## 5. 请求参数与转换策略

### 5.1 不需要转换的参数

下列字段已在现有 Responses DTO/转发链路中保留，应按渠道能力决定是否禁用，不应为了 Grok 单独重写：

* `model`、`input`、`stream`
* `instructions`、`previous_response_id`
* `reasoning`、`text`、`tool_choice`
* 标准 `function`、`mcp`、`shell` 工具（前提是目标上游声明支持）
* `prompt_cache_key`、`prompt_cache_options`、`service_tier`

### 5.2 不能安全转换的参数

| 客户端字段 | 不应做的转换 | 原因 |
| --- | --- | --- |
| `tools[].type = "custom"` | 强转成 `function` | custom 是自由格式输入；function 需要名称和 JSON Schema，调用结果事件也不同 |
| `tools[].type = "custom"` | 静默删除而仍向 Codex 声称可用 | 模型会等待不存在的工具结果，形成后续协议错误 |
| `web_search` / `x_search` | 因本次 422 一并删除 | 当前错误的拒绝对象是 custom；搜索是否支持应独立按模型/渠道验证 |
| `reasoning.effort` | 对所有模型统一强制某一档 | 各模型支持档位不同，应由目录 `supported_reasoning_levels` 与渠道参数规则控制 |

### 5.3 通用转发层的保护建议

当通用 Responses 入口收到渠道无法支持的工具类型时，应优先在发送上游前返回清晰的 400，例如：

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "unsupported_tool_type",
    "message": "Model grok-4.6 on the selected channel does not support Responses tool type custom.",
    "param": "tools[6].type"
  }
}
```

该保护必须由“选中渠道 + 实际上游模型”的能力表驱动；不能作为全局删除/全局拒绝 custom 的规则。它的作用是将不可恢复的上游 422 前移为可理解的网关 400，不替代客户端目录侧的能力收敛。

## 6. 实施顺序与验证

### 6.1 实施顺序

1. 确认线上版本包含“非 OpenAI 原生模型省略 `apply_patch_tool_type`”的目录构造逻辑；
2. 通过带 `client_version` 的 `/v1/models` 响应核对 `grok-4.6` 条目不含 `apply_patch_tool_type`；
3. 删除本地静态模型目录中 Grok 的同名字段，或改为使用服务端动态目录；
4. 完全退出 Codex Desktop，重新启动后新建任务；不要复用已有任务验证模型能力变化；
5. 记录一次脱敏后的 `/v1/responses` 请求工具数组；
6. 只有仍发现 `custom` 时，再确认来源并实施 Grok 专属的 `node_repl_disabled = true`；
7. 如需保留完整 Codex 工具能力，再推动上游 Responses 实现原生支持 `custom`，而不是在 Gravitex 伪转换。

### 6.2 最小回归矩阵

| 用例 | 预期结果 | 防回归目的 |
| --- | --- | --- |
| `grok-4.6` 拉取 Codex 目录 | 有 Grok 条目；无 `apply_patch_tool_type` | 不让 Codex 注册已知不兼容工具 |
| `grok-4.6` 普通文本 Responses | 不出现 custom 导致的 422 | 验证基础会话可用 |
| `grok-4.6` 标准 function 工具 | 仅在上游确认支持后验证 | 不因禁 custom 误伤函数调用 |
| OpenAI 原生模型 | 仍保留 `apply_patch_tool_type = freeform` | 不损失原有补丁能力 |
| 其他第三方模型 | 目录字段与变更前一致 | 防止 Grok 特例扩散 |
| 普通 OpenAI `/v1/models` 客户端 | 仍得到既有 `object/data` 响应 | 防止 Codex 目录影响其他客户端 |
| 普通 `/v1/responses` 调用 | 请求参数和渠道转换不变 | 防止修改全局转发产生回归 |

## 7. 相关代码与测试定位

| 主题 | 文件 |
| --- | --- |
| Codex 目录 DTO 与工具字段 | `relaykit/dto/codex_catalog.go` |
| 模型目录构建、OpenAI 专属 apply_patch 策略 | `service/codex_catalog.go` |
| Codex 目录硬性字段与 Grok apply_patch 隔离测试 | `service/codex_catalog_test.go` |
| 模型列表的 Codex/普通 OpenAI 响应分流 | `controller/model.go` |
| `/v1/models`、`/v1/responses`、`/v1/responses/compact`、`/v1/alpha/search` 路由 | `router/relay-router.go` |
| Responses 请求解析 | `relay/helper/valid_request.go` |
| Responses 通用处理、透传和渠道转换 | `relay/responses_handler.go` |
| OpenAI Responses 请求 DTO（原始 tools 字段） | `relaykit/dto/openai_request.go` |
| 客户端配置说明 | `docs/codex/Codex 接入 Gravitex 配置指南.md` |

## 8. 当前状态与待确认项

已由代码和本机报错确认：Grok 上游拒绝 `custom`；仓库已具备将 freeform apply_patch 限制为 OpenAI 原生模型的代码路径。

尚未由完整请求体确认：移除 apply_patch 后仍出现的 `custom` 是否来自 Node REPL，还是客户端仍使用旧目录/旧任务能力快照。该结论必须以新建任务的脱敏请求体或网关请求日志为准。

在这个证据补齐前，不应改动全局 Tools DTO、通用 Responses 转发、普通模型的目录能力或模型渠道配置。

## 9. xAI 官方能力核对（2026-09-16，仅作兼容边界参考）

xAI 官方当前将 `grok-4.6` 定义为可通过 OpenAI 兼容 Responses API 调用的编码/Agent 模型：500,000 token 上下文窗口、文本和图像输入、文本输出，以及 `low`、`medium`、`high`、`xhigh` 四档推理。模型目录可以据此准确声明上下文和推理档位；不要为该模型声明不存在的无限输出长度配置。

### 9.1 xAI 能力清单与本次范围

xAI 官方能力清单用于定义 Grok/xAI 精确目标的专属能力。本次需要在该精确目标中接入 xAI 已有的搜索、代码执行、远程 MCP、结构化输出和上下文压缩；这些能力不得成为其他模型的默认值、不得进入共享参数处理分支。

| Codex/Responses 能力 | xAI 官方状态 | Gravitex 接入策略 | 本次是否新增/开启 |
| --- | --- | --- | --- |
| 普通文本、图像输入、SSE | Grok 4.6 支持 Responses API、文本/图像输入和流式调用 | 仅在 Grok/xAI profile 中复用既有 `openai-response` 路由与响应适配 | 是 |
| `reasoning.effort` | 支持 `low`、`medium`、`high`、`xhigh` | 仅在 Grok/xAI profile 中声明四档；不改通用参数处理 | 是 |
| 标准 `function` | 支持。客户端执行后以 `function_call_output` + `previous_response_id` 续接 | 按 xAI Responses 原语义透传；不影响其他模型工具链路 | 是 |
| `web_search` | 支持；OpenAI Responses API 的工具名就是 `web_search` | 仅向 Grok/xAI profile 声明，服务端执行结果按原事件转发 | 是 |
| `x_search` | xAI 以 Grok 4.6 给出 OpenAI Responses 直接示例；可限制账号、日期范围，并可开启图片/视频理解 | 仅向 Grok/xAI profile 声明并原样透传 | 是 |
| `code_interpreter` | xAI 以 Grok 4.6 给出 OpenAI Responses 直接示例；xAI 原生 SDK 名为 code execution，Responses 工具名为 `code_interpreter` | 仅向 Grok/xAI profile 声明；服务端沙箱事件、usage 和结果原样保留 | 是 |
| `mcp`（远程 MCP） | 支持 OpenAI 兼容 Responses API | 仅对 Grok/xAI 转换为 `server_url`/`server_label`；不向 xAI 发送不支持的 `connector_id`、`require_approval` | 是 |
| 上下文压缩 | 支持 `POST /v1/responses/compact`，压缩结果必须原样接到后续 `input` | 仅在 Grok/xAI compact 路径保留 xAI opaque item 的全部字节与顺序 | 是 |
| 结构化输出 | Grok 4 系列支持 JSON Schema，且可与服务端工具/函数工具组合 | 仅在 Grok/xAI 透传 `text.format`，不重写 schema | 是 |
| `custom` / freeform apply_patch | 本次上游实测明确拒绝 `type = custom` | 关闭会触发它的 Codex 目录能力；不得转换为 `function` | 否 |
| Codex 本地 `shell` | 本次错误的解析器列为可接受类型，但 xAI 的 Grok 4.6 模型页未给出完整 Responses 工具契约 | 仅在 Grok/xAI profile 中实现并完成 shell call/result、流式/非流式契约测试；它与服务端 code interpreter 是两种能力 | 是，测试解锁后 |

### 9.2 Grok 专属能力目录与转换建议

Grok 的目录应包含 `slug`、上下文窗口、基础指令、四档推理、输入模态及 Grok 专属能力 profile。profile 只对 10.1 所定义的精确目标生效，负责声明/保留标准 function、`web_search`、`x_search`、`code_interpreter`、远程 MCP、结构化输出与 compact。`apply_patch_tool_type` 仍必须省略，以阻止 Codex 注册已确认会令 xAI 422 的 freeform `custom` 工具。

不得通过修改现有全局 `supports_web_search`、`supports_function_calling` 的来源或含义来表达上述能力。必须新增模型+渠道专属 profile，并为非 Grok 返回“无 profile”；profile 不得进入任何非 Grok 的目录或请求转换。

Codex 本地 `shell` 也需要接入，但 xAI 当前没有给出与 `x_search`/`code_interpreter` 同等的 Responses 工具契约。因此只能以 Grok/xAI 专属的受控能力实现：先用假上游覆盖 shell call、shell result 续接、流式和非流式事件；再用脱敏真实请求确认 xAI 接受完整语义后才将该字段对真实 Grok 目录设为启用。未通过这两层测试时，该 profile 必须拒绝/隐藏 shell，不能以通用降级或全局转换代替。

### 9.3 必须继续做的实测

官方文档证明的是 xAI 直连 API 的能力，不等于当前 Gravitex 某个渠道已经完整保留该协议。每项新增目录能力应使用同一个 `grok-4.6` 渠道做最小真实请求，并保存脱敏请求/响应证据：

1. 单独验证 `function` 的调用和 `function_call_output` 续接；
2. 分别验证 `web_search`、`x_search`、`code_interpreter` 的流式与非流式事件、最终响应和 usage；
3. 验证远程 `mcp` 的 `server_url`/`server_label` 透传，不发送 xAI 不支持的 `connector_id`、`require_approval`；
4. 验证 `text.format` 的 JSON Schema 原样透传，并覆盖它与 function/服务端工具组合的结果；
5. 验证 `/v1/responses/compact` 的 opaque compaction item 能被原样续接；
6. 验证 shell call、shell result 续接、流式与非流式语义；未通过即不启用 shell；
7. 为每一项执行同一轮 OpenAI 原生模型、其他第三方模型、普通 `/v1/responses` 的字节级回归。

上述实测属于本次上线验收：分别验证每项 Grok 专属工具的非流式、流式、工具结果续接、错误与 usage；同时满足第 10 节的所有非 Grok 不变量。

## 10. 非 Grok 请求与响应字节级不变（上线硬门槛）

本节优先级高于任何能力扩展。Grok 接入不能以“其他模型看起来还能调用”为验收标准；对所有不满足精确 Grok 判定的请求，必须证明其入站后的出口请求字节、上游响应处理和客户端可见结果均与改造前一致。

### 10.1 唯一允许产生差异的判定

不得仅根据客户端传入的 `model == "grok-4.6"` 判断，因为模型映射可能把同一个客户端模型名导向不同上游，也可能把别名映射为 Grok。

唯一允许变化的目标必须同时满足：

1. 已完成渠道选择；
2. 已完成该渠道的模型映射；
3. 最终渠道具备 xAI Responses 兼容能力；
4. 最终上游模型精确等于 `grok-4.6`（或经数据表明确标注为其等价别名）。

除上述目标外，一律视为“非 Grok”。非 Grok 不读取、不应用 Grok 的工具能力、参数覆盖、工具拦截或响应转换规则。

### 10.2 非 Grok 不变量

对每一个非 Grok 请求，下列内容必须逐字节与改造前基线一致；不能用 JSON 语义相等、字段子集相等或“调用成功”替代：

| 面向 | 必须不变的证据 |
| --- | --- |
| Codex 模型目录 | `GET /v1/models?client_version=...` 的完整响应字节 |
| 普通 Responses 非流式 | 上游出口请求 body、响应 body、HTTP 状态、错误 body |
| 普通 Responses 流式 | 上游出口请求 body、每个 SSE 事件的原始字节和顺序、结束标记 |
| Chat Completions 非流式/流式 | 转 Responses 的请求体、返回体或 SSE 事件、finish reason |
| 工具续接 | `function_call`、`function_call_output`、`previous_response_id`、工具参数和结果 body |
| 服务端工具 | `web_search`、`x_search`、`code_interpreter`、`mcp`、shell、tool choice、并行工具调用字段 |
| 上下文压缩 | `/v1/responses/compact` 的请求与响应，以及 opaque compaction item 作为下一轮 input 的原始内容和顺序 |
| 参数处理 | 模型映射、渠道参数覆盖、禁用字段、Body PassThrough 开/关两条路径 |
| 运行语义 | 重试次数、渠道选择、错误码/错误体、usage、预扣/结算额度、日志字段 |

这里的“出口请求 body”应在调用 adaptor 后、写入上游 HTTP 请求前采集；“客户端可见结果”应在写回客户端前采集。两处都必须对比，才能覆盖转换前后任一层误改参数的风险。

### 10.3 禁止触碰的共享代码与配置

不得为了 Grok 修改以下共享行为：

* `dto.OpenAIResponsesRequest.Tools` 的 DTO 类型、JSON 序列化或通用 token 计数；
* `relay.ResponsesHelper`、通用 Responses adaptor 转换、通用流式写回逻辑；
* `relaycommon.RemoveDisabledFields`、全局 `PassThroughRequestEnabled`、全局参数覆盖；
* 非 Grok 模型的 Codex 目录默认字段、模型扩展能力、渠道配置或模型映射；
* 任意通用“删除 `tools[].type = custom`”或“把 custom 转为 function”的规则。

现有 `RemoveDisabledFields` 属于渠道级字段策略，不是 Grok 专用开关；用它或全局 Body PassThrough 解决 Grok 422 都会扩大影响面，禁止采用。

### 10.4 Grok 的最小差异白名单

Grok/xAI 精确目标中，默认只允许以下已审批差异：

| 层级 | 允许差异 |
| --- | --- |
| Codex 目录 | 仅 Grok/xAI 条目允许缺失 `apply_patch_tool_type`，并声明已批准的 Grok 专属能力 profile |
| Grok 请求 | 不产生由 apply_patch 注册导致的 `type = "custom"`；按 profile 保留/转换已批准的工具字段 |
| Grok 失败保护 | 仅当脱敏请求证实仍有 `custom` 时，对该精确目标返回 `unsupported_tool_type`，指出 `tools[n].type` |
| Node REPL | 仅在同一证据确认其为 custom 来源时，为该精确目标设置 `node_repl_disabled = true` |
| Grok 专属工具 | 仅 profile 内允许 `web_search`、`x_search`、`code_interpreter`、远程 MCP、结构化输出、compact 和通过契约测试的 shell |

白名单外的任意差异均视为回归，包括字段顺序、空值与缺失字段的变化、SSE event 名称/顺序变化，以及 usage 或日志字段变化。

### 10.5 必须新增的回归测试与抓包基线

实现前先建立改造前基线；实现后使用相同固定输入、同一假上游/adaptor 和同一渠道设置执行对照。测试不能访问真实密钥，捕获内容必须脱敏。

1. **目录快照对照**：OpenAI 原生、Claude、Gemini、Kimi 和一个普通第三方模型各一份；断言每个非 Grok 条目的完整 JSON 字节相同。
2. **Responses 双态对照**：每个样本模型分别跑非流式和 SSE；断言出口请求 body、响应 body 或 SSE 帧序列逐字节相同。
3. **Chat Completions 双态对照**：覆盖直连和“Chat 经 Responses”转换路径，包含 stop、length、tool_calls 等 finish reason。
4. **工具生命周期对照**：固定 function、MCP、search、shell、并行工具调用的输入，再对 `function_call_output` 续接；断言每一轮 body、事件与结果一致。
5. **compact 对照**：固定 compaction item，断言 compact 响应和下一轮 `input` 中 item 的原始字节、位置与顺序一致。
6. **参数矩阵对照**：逐项覆盖 `reasoning`、`text`、`tool_choice`、`instructions`、`previous_response_id`、缓存字段、`service_tier`、`store`；每项再覆盖渠道 PassThrough 开与关。
7. **运行结果对照**：验证渠道选择、重试、错误码、usage、消费日志、预扣和最终结算均无差异。

任一非 Grok 对照失败，Grok 改造不得合并、不得部署，也不得以“该模型能正常返回”豁免。

### 10.6 现有代码的风险与实施前置条件

当前 `BuildCodexCatalog` 对 `apply_patch_tool_type` 的判定是“是否 OpenAI 原生模型”，而不是“是否 Grok/xAI 渠道”；它已使所有非 OpenAI 模型在 Codex 目录中省略该字段。该既有行为不得被本次改造扩大，也不能被误称为 Grok 专属隔离。

当前 `NodeReplDisabled` 对目录条目统一为 `false`。禁止将其改为全局 `true`；只有在 10.1 的精确目标和 10.4 的请求证据同时成立时，才允许引入 Grok 专属值。

`go test ./service -run '^TestBuildCodexCatalogOnlyOffersFreeformApplyPatchToOpenAIModels$'` 当前会在执行目标测试前被 `service/task_billing_test.go` 的既有编译错误阻断。因此不能将现有单测当作上线证明；必须在可独立编译的测试包中建立上述基线对照，且不修复或修改无关的 task billing 测试。

### 10.7 参数、流与计费的零例外要求

对非 Grok，**没有任何允许变化的请求参数、响应字段、流事件或计费字段**。这包含当前 DTO 未建模、仅靠 `json.RawMessage` 透传的未来字段；它们同样属于原始请求 body，不能因 Grok 接入被解析、重排、删除、补默认值或覆盖。

计费验收不能只比较 `usage.total_tokens`。在固定时钟、固定用户/令牌/渠道、固定额度和固定假上游响应的测试环境中，改造前后必须逐字段相同：

* 请求预扣额度、最终结算额度、退款额度和用户/令牌剩余额度；
* prompt、completion、reasoning、cached、工具相关 usage 的值及缺失/零值状态；
* 消费日志、渠道日志、模型名、渠道 ID、倍率、状态、错误信息和 `other` 元数据；
* 请求次数、重试次数、计费时间点及所有持久化记录的字段。

流式验收必须保存完整原始 SSE 字节序列，不得只比较拼接后的文本；非流式验收必须比较完整 HTTP body，不能只比较 `output_text`。如果响应中含随机 ID、时间戳或请求追踪 ID，测试必须使用可控假上游和固定时钟生成确定基线，不能先删除这些字段再声称“无影响”。

任何非 Grok 参数、流、计费或日志出现一丁点差异，均为阻断级回归：停止 Grok 改造，不允许通过调整全局逻辑、放宽断言或忽略字段来绕过。
