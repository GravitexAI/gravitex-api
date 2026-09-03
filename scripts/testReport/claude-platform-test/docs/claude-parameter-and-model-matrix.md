# Claude 模型系列与参数用例矩阵

## 独立模型 Case

每个被测模型在 `config.py` 的 `MODEL_CASES` 中有且只有一个独立登记项，编号为 `MODEL-001`～`MODEL-005`。该登记项用于报告中的官方预期基线，不会为了“登记模型”再额外发送 API 请求；实际请求仍按“模型 × 通用功能用例”执行。

模型能力预期只在代码配置中维护，用于运行时判定和测试明细中的预期结果；Excel 报告不再单独生成“模型预期矩阵”页。

## 模型分组

| 模型 | 系列 | Thinking 模式 | 说明 |
|---|---|---|---|
| `claude-fable-5` | fable | adaptive | 新一代自适应思考模型 |
| `claude-haiku-4-5-20251001` | haiku | manual | 不使用 adaptive thinking |
| `claude-opus-4-8` | opus | adaptive | `thinking.type=enabled` 预期 400 |
| `claude-sonnet-5` | sonnet | adaptive | `thinking.type=enabled` 预期 400 |
| `claude-opus-5` | opus | adaptive | 使用 adaptive thinking |

## 参数边界用例

以下用例按官方模型规则校验；采样参数请求不启用 `thinking`，因此旧模型与新模型的预期不同：

| 用例 | 请求参数 | 旧模型（无 thinking） | Fable 5 / Opus 4.7+ / Sonnet 5 |
|---|---|---|---|
| `AN-PARAM-001` | `temperature=0.2` | HTTP 2xx | HTTP 4xx |
| `AN-PARAM-002` | `top_p=0.9` | HTTP 2xx | HTTP 4xx |
| `AN-PARAM-003` | `top_k=40` | HTTP 2xx | HTTP 4xx |
| `AN-PARAM-004` | `max_tokens=128001` | HTTP 4xx | HTTP 4xx |

采样参数用例不携带 `thinking`：官方规则是 Fable 5、Opus 4.7/4.8/5、Sonnet 5 的非默认 `temperature/top_p/top_k` 固定返回 400；4.5/Haiku 4.5/4.6 在不启用 thinking 时允许这些参数。`max_tokens=128001` 超过新模型 128k 和旧模型 64k 上限，全部预期 4xx。具体状态码、错误类型和错误信息全部保存到 Excel 明细中。

## 已覆盖的能力用例

- 基础 Messages：普通对话、system、多轮上下文、停止序列、`max_tokens` 截断。
- 参数边界：`temperature`、`top_p`、`top_k`、超大 `max_tokens`。
- Prompt Cache：默认 5 分钟缓存和 1 小时缓存的命中率（种子轮写入 + 20 轮读取；命中率 = 读取 ÷ (读取 + 写入) tokens，含种子轮，≥ 85% 通过，理论上限 95.2%）。
- 流式：基础 SSE、工具调用增量拼装、thinking 增量。
- Thinking：manual/adaptive 自动选择、复杂推理、thinking + tools；effort 统一取最高档 `max`（低档位官方允许模型不思考，逐档测会误判）。
- 工具：自动选择、强制选择、工具结果闭环、并行工具调用。
- 视觉/PDF：Base64、URL 直链、小大资源、多图和超限行为；URL 资源两种传输方式任一成功即通过。
- 错误：非法模型、空消息、错误密钥。
- 其他：上下文隔离；响应延迟分位 P50/P95/P99（取自提示词缓存用例的调用耗时，仅统计不判定）。
- 联网搜索：所有模型均执行 `web_search_20250305` 的流式和非流式服务端工具用例；因资源来源不固定（官方支持、Bedrock 不支持），结论记为跳过，不计入通过率。

## 联网搜索用例

联网搜索使用官方基础服务端工具定义：

```json
{
  "type": "web_search_20250305",
  "name": "web_search"
}
```

每个模型执行两条联网搜索用例：

- `AN-WEB-SEARCH-001`：非流式；
- `AN-WEB-SEARCH-002`：流式。

判定要求：HTTP 200、出现 `server_tool_use`、出现 `web_search_tool_result`，并且返回非空文本。响应报告会额外记录服务端工具调用数、搜索结果块数、SSE 事件类型和搜索提示词。

如果返回 400，报告会保留完整错误；常见原因包括组织未启用 Web Search、平台不支持该服务端工具或上游没有透传该工具。不能把 400 自动判断为模型参数错误。

## 官方依据

- [Messages API](https://platform.claude.com/docs/en/api/messages)
- [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)
- [Claude models overview](https://platform.claude.com/docs/en/about-claude/models/overview)

官方文档指出：Claude 4.5 及更早模型使用 manual thinking；4.6 的 manual thinking 已弃用；4.7 及以后模型不支持 `thinking.type=enabled`，应使用 `thinking.type=adaptive`。实际请求是否被 Gravitex 完整透传，仍以测试响应和保存的请求/响应记录为准。

官方参考：

- [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)
- [Messages API - Create a Message](https://platform.claude.com/docs/en/api/messages/create)
- [Web search tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool)
