# Grok 4.6 Codex/xAI 隔离兼容实施计划

> **执行方式：** 当前 `main-alpha` 分支直接修改。仅新增 Grok/xAI 专属代码与对应测试；不修改共享请求 DTO、通用 Responses 转换、通用流式处理、通用计费逻辑或其他模型目录项。

## 目标

让映射到 xAI `grok-4.6` 的 Codex Responses 请求可安全使用 xAI 已有的 `web_search`、`x_search`、`code_interpreter`、远程 MCP、结构化输出和 compact 通道；对 xAI 不接受的 `custom` 工具给出本地清晰错误，并剥离 xAI 文档明确不支持的 MCP 字段。任何非 Grok/xAI 请求必须在进入 xAI adapter 时保持其工具原始 JSON 字节不变。

## 实施步骤

1. 为 xAI adapter 的 Grok 4.6 请求预处理写失败测试：精确匹配 `ChannelTypeXai` 加上已映射的 `UpstreamModelName=grok-4.6`；覆盖 `custom` 拒绝及非 Grok 原始工具 JSON 不变。
2. 在 `relay/channel/xai` 内实现预处理，不改变 `dto.OpenAIResponsesRequest` 或共享 relay helper。仅在上述精确条件成立时处理：拒绝 `custom`，移除 MCP 的 `connector_id` 和 `require_approval`；其他请求直接返回原 request。
3. 审计 xAI Responses 的普通和 SSE 回包、compact 支持与工具计费。若 xAI 的回包 item 类型需要额外计数，只在 xAI Grok 专属调用点增加，并对普通/SSE 建立成对测试；禁止修改 `relay/channel/openai` 通用处理器或 `RelayInfo.CountBillableToolCall`。
4. 审计 Codex catalog 的能力字段。只有在字段语义、Codex 客户端实际发送格式、且 catalog 可识别 xAI 映射三者均已验证时，才新增 Grok 专属 catalog profile；无法验证时保持字段不变，由 xAI 原生 Responses 请求直接透传，避免猜测实验字段造成其他模型或 Grok 非 xAI 路由变化。
5. 增加回归测试：非 Grok request JSON、普通/SSE 响应/usage 与计费路径均不触发新代码；运行 xAI 包的定向测试和可编译的相关包测试。若既有仓库测试因无关编译错误失败，记录其原始错误，不将其归因为本改动。

## 完成标准

- 新逻辑唯一入口在 xAI adapter，并由精确 channel + upstream model 判定。
- 非 Grok 请求不发生 JSON 重编码或字段增删。
- `custom` 不发送给 xAI，远程 MCP 请求不含 xAI 明确不支持的字段。
- compact、结构化输出、`web_search`/`x_search`/`code_interpreter` 均维持 xAI Responses 原生透传；shell 在获得 xAI 接口契约和回归测试前不做虚假转换。
- 所有新增测试通过；未修改共享计费、日志、流式与非流式代码。
