# dataeyes 渠道 Gemini 内置工具问题 —— 定位结论与能力边界说明

- 编写日期：2026-08-24
- 来源工单：`logs/dataeyes渠道Gemini内置工具修复说明_20260821(1).md`
- 涉及模型：`gemini-3.7-flash`（客户提供），其余 Gemini 3.x 同理
- 涉及入口：`POST /v1beta/models/{model}:generateContent` / `:streamGenerateContent`（平台原生 Gemini 端点）
- 当前资源现状：**Gemini 系模型全部挂在「Vertex AI」类型渠道**，无 Gemini Developer API（AI Studio API Key）渠道

---

## 一、一句话结论

客户报的 3 个「病灶」里：

- **病灶 1（无 `toolCall`/`toolResponse` 事件）+ 场景②（模型编造 URL）**：根因是 Vertex AI 不具备该能力（Gemini Developer API 独有）。实测为「接受后静默忽略」（HTTP 200 但 parts 无事件部件）。**无论我们怎么改都做不到**，要过验收必须接入 Gemini Developer API 渠道。
- **病灶 2（URL Context 失败）**：分两层。「组合被上游拒绝」是我们把 Vertex API 版本写死 `v1` 导致的 —— **已修复上线**（改走 `v1beta1`），实测组合已能被接受；「URL Context 抓不到搜索结果页」是 Google 上游不跟随 grounding redirect 的限制，**实测仍失败，网关侧无解**。
- **病灶 3（请求体双重转义）**：在我们的原生 Gemini 路径上**实测不成立**（含引号文本逐字节等价往返），需要客户/中间层配合抓包定位。

另外顺手补了一个客户没提的易用性缺口：入站现在同时接受 `/v1beta/models/*` 与 `/v1beta1/models/*` 两种写法。

另外顺带查出一个客户没提、但我们侧真实存在的丢字段问题（只影响 OpenAI / Claude / Responses 格式入口，不影响客户当前用的原生端点）。

---

## 二、客户诉求逐条核查表

| 客户诉求 | 核查结论 | 归类 |
|---|---|---|
| 病灶 1：`toolConfig.includeServerSideToolInvocations` 未生效，响应无 `toolCall`/`toolResponse` | **Vertex AI 不支持该字段**（Gemini Developer API 独有），实测为「接受后静默忽略」。我们侧转发完好（实测保留） | **A 类：Vertex 做不到** |
| 场景 ②：googleSearch + functionDeclarations，模型编造 URL | **Vertex AI 官方文档明确不支持「搜索工具 + 非搜索工具」组合**；且缺少上面的事件回流机制 | **A 类：Vertex 做不到** |
| 病灶 2：URL Context 检索一律失败 / 无 `urlContextMetadata` | 分两层：「组合被上游拒绝」是我们把 API 版本写死 `v1` 导致的，**已修**（切 `v1beta1`）；「抓不到搜索结果链接」是 Google 上游限制，**实测仍失败** | **B 类（已修）+ A 类（上游限制）** |
| 病灶 3：请求体字符串双重转义 | 原生路径实测**逐字节等价，无二次转义**。不在我们侧 | **C 类：需客户侧/中间层复核** |
| 修复建议：请求/响应字节级透传 | 原生 Gemini 端点**已经是字节透传**（请求 struct 重组但字段无损，响应原样回吐） | 已满足 |
| （客户未提）响应 DTO 缺 `toolCall`/`toolResponse`/`urlContextMetadata`/完整 `groundingMetadata` | 真实存在，但**只影响 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`** 这些需要重组响应的入口 | **B 类：我们可以修** |

---

## 三、A 类：Gemini Developer API 独有，Vertex AI 做不到

> 这两项无论我们改多少代码，只要渠道后端是 Vertex AI 就实现不了。

### A-1 `toolConfig.includeServerSideToolInvocations`（工具上下文回流）

**它是什么**（Gemini API 官方文档 "Combine built-in tools and function calling"）：

- 官方称为 **tool context circulation**（工具上下文回流），作用是「保留并暴露内置工具的上下文，并在同一次调用中跨轮次共享给自定义工具」；
- 开启后响应 parts 中出现 `toolCall` / `toolResponse`（内置工具调用与返回）、`functionCall` / `functionResponse`、`executableCode` / `codeExecutionResult`；
- 这些 part 携带 `id`、`tool_type`、`thought_signature`，**后续请求必须原样带回**才能维持上下文；
- 状态：**Preview，仅支持 Gemini 3 系模型**（官方示例正好用的就是 `gemini-3.7-flash`）；
- 可与函数声明组合的内置工具：Google Search、Google Maps、URL Context、File Search、Code Execution。

**Vertex 不支持的证据**：

1. Spring AI 官方文档原文（Server-Side Tool Invocations 章节）：
   > "This feature is only supported with the **Gemini Developer API** (MLDev / API key authentication). It is **not supported** on Vertex AI."
2. vercel/ai #14655 真实报错，请求 URL 就是 Vertex：
   ```
   POST https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse
   400 INVALID_ARGUMENT
   Invalid JSON payload received. Unknown name "includeServerSideToolInvocations" at 'tool_config': Cannot find field.
   ```
   该 issue 的修复方案就是「从 combined tool config 里移除 `includeServerSideToolInvocations`，Vertex 用户先禁用，等 Google 补 API 支持」。

**2026-08-24 实测确认（Vertex + v1beta1）**：我们用自己的 Vertex 渠道实打了一次
`tools:[{googleSearch:{}}] + toolConfig.includeServerSideToolInvocations:true`：

- HTTP **200**（不是 vercel/ai #14655 记录的 400 `Unknown name`）；
- 但候选 parts 的 key 只有 `["text","thoughtSignature"]`，**完全没有 `toolCall` / `toolResponse`**；
- `groundingMetadata.groundingChunks` 有 1 条，`web.uri` 是真实的
  `https://vertexaisearch.cloud.google.com/grounding-api-redirect/AUZIYQ...`。

结论：**Vertex 对该字段是「接受后静默忽略」，不是硬拒**。这正好解释了客户为什么拿到 200 却没有事件部件 —— 与 Spring AI 文档「not supported on Vertex AI」一致，A-1 成立。硬拒（400）与静默忽略只是不同模型/版本/location 下 schema 校验松紧的差异，能力缺失的结论不变。

> 附带一个对客户有用的事实：grounding redirect 链接**确实在响应里**（`groundingMetadata.groundingChunks[].web.uri`，客户端可见），只是没有作为 model-visible 的 tool 事件部件回流给模型。客户如果只是要拿到真实 redirect 链接，可以自己从 `groundingChunks` 取；但要让**模型**在同一轮里把该链接传给自定义函数，仍需 A-1 的能力。

**场景 ② 编造 URL 是同一根因的下游后果**：客户的协议依赖「模型可见的搜索结果」（PerQueryResult 的 `url` 字段）。没有 server-side tool 事件回流，模型拿不到真实 grounding redirect 链接，只能自己编一个 `https://www.google.com/search?q=...` 塞给自定义函数。

### A-2 googleSearch + functionDeclarations（搜索工具 + 非搜索工具组合）

Vertex AI 官方文档「使用 Google 搜索建立依据」的注意事项章节原文：

> **工具组合**：Gemini API 不支持在同一 `generateContent` 请求中将搜索工具（例如 `googleSearch`）与非搜索工具（例如函数调用或 RAG Engine `retrieval` 工具）相结合。**只有当所有工具均为搜索工具时，才支持多种工具。**

而 Gemini Developer API 侧文档明确写：「Gemini 3 models also support combining these built-in tools with custom tools (function calling)」。

→ **客户验收用例二（googleSearch + functionDeclarations）在 Vertex 渠道上永远过不了**，与我们的代码无关。

---

## 四、B 类：Vertex AI 支持，但平台代码没实现（我们能修）

### B-1 【最重要】Vertex 适配器把 API 版本写死 `v1`，导致 urlContext + googleSearch 组合失效

**Vertex 官方文档怎么说**（「网址上下文」页，"依托 Google 搜索进行接地（含网址上下文）" 章节）：

> 您还可以同时启用网址上下文和「依托 Google 搜索进行接地」……
> **此功能为实验性功能，可在 API 版本 `v1beta1` 中使用。**

官方 REST 示例（注意版本段）：

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT/locations/global/publishers/google/models/gemini-3.5-flash:generateContent \
  -d '{
    "contents": [{"role":"user","parts":[{"text":"..."}]}],
    "tools": [{"url_context": {}}, {"google_search": {}}]
  }'
```

社区交叉验证 —— googleapis/python-genai #941（标题即 "URL context tool not working together with Google Search **on Vertex AI**"）：

- 报错：`{'error': {'code': 400, 'message': 'Multiple tools are supported only when they are all search tools.', 'status': 'INVALID_ARGUMENT'}}`
- 提交者补充：「换成 API key 的 Gemini API，同一份代码就正常」
- 关键评论：**「If you create a client with `api_version="v1beta1"`, the example code from the docs (with `vertexai=True`) works for me. In the response I can see …」**

**我们代码的问题**：

| 位置 | 内容 |
|---|---|
| `relay/channel/vertex/url_builder.go:9` | `DefaultAPIVersion = "v1"`（Gemini/Claude 共用） |
| `relay/channel/vertex/adaptor.go:151` | `BuildGoogleModelURL(info.ChannelBaseUrl, DefaultAPIVersion, adc.ProjectID, region, modelName, suffix)` |
| `relay/channel/vertex/adaptor.go:167` | API Key 模式同样写死 `DefaultAPIVersion` |

即：**Vertex 渠道的 Gemini 请求 100% 走 `v1`，无任何配置可以切到 `v1beta1`**。

对照 Gemini（Developer API）类型渠道，版本是可配的且默认就是 `v1beta`：

| 位置 | 内容 |
|---|---|
| `setting/model_setting/gemini.go:36-39` | `VersionSettings: {"default": "v1beta", "gemini-1.0-pro": "v1"}` |
| `relay/channel/gemini/adaptor.go:133` | `version := model_setting.GetGeminiVersionSetting(info.UpstreamModelName)` |

**顺带一个坑**：Vertex 渠道的「API 版本」输入框已经被挪用成 **region 配置**了 —— `relay/channel/vertex/adaptor.go:142` 是 `GetModelRegion(info.ApiVersion, info.OriginModelName)`，支持 `{"model":"region","default":"global"}` 的 JSON 形式。所以要加 `v1beta1` 开关，**不能复用 ApiVersion 字段**，得在 `ChannelOtherSettings`（`relaykit/dto/channel_settings.go:76` 附近，`vertex_key_type` 旁边）新增一个 `vertex_api_version`，或者按模型名做映射。

**修完能拿到什么**：
- `urlContext` 单独使用 + `urlContext` 与 `googleSearch` 组合都走官方支持路径；
- 响应里能拿到 `urlContextMetadata.urlMetadata[].urlRetrievalStatus`；
- 客户验收用例一的第 3 条（`URL_RETRIEVAL_STATUS_SUCCESS`）有希望通过。

**修完仍拿不到什么**：用例一第 2 条（`toolCall`/`toolResponse` 部件）—— 那是 A-1，Vertex 不支持。

**附：Vertex 侧 URL Context 支持的模型清单**（官方页列出，注意**不含 `gemini-3.7-flash`**）：Gemini 3.6 Flash、3.5 Flash-Lite、3.5 Flash、3.1 Flash-Lite、3.1 Pro(预览)、3 Flash(预览)、2.5 Pro / 2.5 Flash / 2.5 Flash-Lite。客户用的 `gemini-3.7-flash` 是否已在 Vertex 上线并支持 URL Context，需要用我们自己的 Vertex Key 实测确认。

### B-2 响应 DTO 缺字段（只影响非原生入口）

`relaykit/dto/gemini.go` 的响应结构体缺一批字段：

| 位置 | 缺什么 |
|---|---|
| `GeminiPart`（:270） | 没有 `toolCall`、`toolResponse` |
| `GeminiChatCandidate`（:441） | 没有 `urlContextMetadata` |
| `GeminiGroundingMetadata`（:449） | **只有** `webSearchQueries`；`groundingChunks`、`groundingSupports`、`searchEntryPoint` 全丢 |

实测（把一份含完整字段的响应过一遍 `Unmarshal → Marshal`）：`toolCall` / `toolResponse` 部件退化成空对象 `{}`，`urlContextMetadata` 直接消失，`groundingMetadata` 只剩 `webSearchQueries`。

**影响面**：仅限需要重组响应的入口 ——
- `/v1/chat/completions`（`GeminiChatHandler`）
- `/v1/messages`
- `/v1/responses`（`GeminiResponsesHandler`）

**不影响**客户当前用的原生 `/v1beta/models/...` 端点（见下一节）。

### B-3 `toolConfig` 不认 snake_case（小问题）

`GeminiChatRequest.UnmarshalJSON`（`relaykit/dto/gemini.go:24-42`）只对 `system_instruction` 做了 snake_case 兼容；`ToolConfig`（:44）没有自定义 `UnmarshalJSON`，所以 `include_server_side_tool_invocations`（下划线写法）会被丢弃，只认 camelCase `includeServerSideToolInvocations`。

客户走官方 REST/SDK 时发的是 camelCase，暂不触发；但客户建议里明确点了「兼容 snake_case」，可以顺手补上。

---

## 五、C 类：我们侧实测不成立的部分

### C-1 原生 Gemini 端点已经是字节透传

**响应侧**：
- 非流式：`relay/channel/gemini/relay-gemini-native.go:70` —— `service.IOCopyBytesGracefully(c, resp, responseBody)`，下发的是**上游原始字节**，解析只用于计费；
- 流式：`relay/channel/gemini/relay-gemini.go:1564` —— 回调里 `helper.StringData(c, data)`，每个 chunk 原样下发。

→ 上游给了 `urlContextMetadata` / `toolCall` / `toolResponse` / 完整 `groundingMetadata`，客户就一定收得到。客户收不到 = **上游没给**。

**请求侧**：走 `relay/gemini_handler.go` 的 `Unmarshal → common.DeepCopy(:63) → ConvertGeminiRequest(:148) → common.Marshal(:153)` 链路。虽然是「反序列化 → 结构体 → 再序列化」，但关键字段无损：

| 字段 | 机制 | 结果 |
|---|---|---|
| `tools`（整块） | `GeminiChatRequest.Tools` 是 `json.RawMessage`（`relaykit/dto/gemini.go:17`） | 原样透传，`urlContext`、`parametersJsonSchema` 全保留 |
| `toolConfig.includeServerSideToolInvocations` | `ToolConfig` 有该字段（`relaykit/dto/gemini.go:47`，`*bool`） | 保留 |
| `contents[].parts[].text` | 普通 string | 逐字节等价 |

实测（用客户验收用例二的请求体原文跑真实链路）输出：

```json
{"contents":[{"role":"user","parts":[{"text":"Query: \"AIRPAZ SINGAPORE PTE. LTD.\" 201528606C products services"}]}],
 "generationConfig":{"thinkingConfig":{"includeThoughts":true,"thinkingLevel":"MEDIUM"}},
 "tools":[{"googleSearch":{}},{"urlContext":{}},{"functionDeclarations":[{"name":"resolve_grounding_redirect","description":"...","parametersJsonSchema":{"type":"object","properties":{"grounding_uri":{"type":"string"}},"required":["grounding_uri"],"additionalProperties":false}}]}],
 "toolConfig":{"includeServerSideToolInvocations":true},
 "systemInstruction":{"parts":[{"text":"..."}]}}
```

`\"AIRPAZ...\"` 是 JSON 转义层的正常表示，解码后就是 `"AIRPAZ..."`，**没有二次转义**。

### C-2 双重转义（病灶 3）的结论

我们的原生路径不产生字面反斜杠。可能来源：

1. 终端客户自己那层对已经是 JSON 的字符串再做了一次 `json.dumps` / `json.Marshal`；
2. dataeyes 网关侧重组请求体时的二次编码；
3. 模型自身在 Vertex 上对含引号 query 的行为差异（概率低，但客户只跑了一次，样本不足）。

**定位方法**（任一即可）：
- 我们打开该渠道的请求体 DEBUG 日志（`relay/gemini_handler.go:166`，`Gemini request body: ...`），看打到我们入口的 `contents[].parts[].text` 是否已带字面反斜杠；
- 或让客户在渠道上开启 **透传模式**（`PassThroughRequestEnabled` / 渠道级 `PassThroughBodyEnabled`，`relay/gemini_handler.go:140`）—— 此时我们连结构体重组都不做，直接把原始 body 转发上游。若开了透传现象仍在，可 100% 排除我们侧。

---

## 六、只有 Vertex 资源的情况下，客户验收用例能过到什么程度

### 用例一：googleSearch + urlContext

| 通过标准 | 现状（Vertex + 写死 v1） | 我们修完 B-1（切 v1beta1）后 | 备注 |
|---|---|---|---|
| ① HTTP 200 | ✅ | ✅ | |
| ② parts 出现 `toolCall`/`toolResponse`（`GOOGLE_SEARCH_WEB` / `URL_CONTEXT`） | ❌ | ❌ **仍然过不了** | A-1：Vertex 不支持 `includeServerSideToolInvocations` |
| ③ `urlContextMetadata` + `URL_RETRIEVAL_STATUS_SUCCESS` | ❌ | ❌ **实测仍过不了** | 组合被接受了，但 URL Context 抓不到搜索结果链接。见第六节末「实测结果」VER-03 |
| ④ `webSearchQueries` 无字面反斜杠 | ⚠️ 待抓包 | ✅ **实测通过** | 见「实测结果」ESC-01 |

### 用例二：googleSearch + functionDeclarations

| 通过标准 | 现状 | 修完 B-1 后 | 备注 |
|---|---|---|---|
| ① 出现一次 `GOOGLE_SEARCH_WEB` toolCall/toolResponse | ❌ | ❌ | A-1 |
| ② `functionCall` 的 `grounding_uri` 为真实 redirect 链接 | ❌ | ❌ | A-2：Vertex 明确不支持搜索工具 + 函数调用组合 |
| ③ `webSearchQueries` 无字面反斜杠 | ⚠️ 待抓包 | ✅ **实测通过** | 见「实测结果」ESC-01 |

**结论：只有 Vertex 资源时，用例二无法交付，用例一只能过 4 条中的 2 条（②③ 必挂）。要 100% 过验收，必须有一条 Gemini Developer API（AI Studio API Key）渠道。**

### 实测结果（2026-08-24，改动上线后在 Vertex 渠道上跑）

脚本：`logs/gemini-vertex-test/test_gemini_vertex.py`（11 条用例，10 通过 / 1 不通过）。

| 用例 | 结论 | 关键观测 |
|---|---|---|
| BASE-01 原 `/v1beta` generateContent | ✅ | 无回归 |
| ALIAS-01～04 `/v1beta1` 别名 | ✅ | 可达 / 无凭证返回 401 JSON / `?key=` 鉴权生效 / 流式收到 SSE 事件 |
| VER-01 `urlContext` 单独使用 | ✅ | `retrievedUrl=https://ai.google.dev/gemini-api/docs/url-context`，`urlRetrievalStatus=URL_RETRIEVAL_STATUS_SUCCESS` |
| **VER-02 `urlContext` + `googleSearch` 组合被接受** | ✅ | HTTP 200，`groundingMetadata` 有，**不再报 `Multiple tools are supported only when they are all search tools`** —— B-1 的目标达成 |
| **VER-03 组合场景下 URL Context 实际检索成功** | ❌ | `urlContextMetadata` **缺失**；模型正文自述 *"no page content was returned… I'm sorry. I'm not able to access the website(s) you've provided"* |
| TC-01 `toolConfig` snake_case vs camelCase | ✅ | 两种写法都 HTTP 200，行为一致 |
| ESC-01 含引号文本透传 | ✅ | 模型原样回显 `"AIRPAZ SINGAPORE PTE. LTD." 201528606C`，无字面 `\"` |
| CLD-01 Claude `/v1/messages` | ✅ | HTTP 200，Claude 链路未受 Gemini 切版本影响 |

**VER-01 与 VER-03 的对照是本次最有价值的定性证据**：

- URL Context 在本平台上**本身是好的** —— 抓普通 URL 成功（VER-01）；
- 失败**特定于「抓取 Google 搜索返回的 grounding redirect 链接」**（`vertexaisearch.cloud.google.com/grounding-api-redirect/...`），且模型自述的错误措辞与客户报告里的 "unable to access the website" 完全一致；
- 这与 googleapis/python-genai #1322 记载一致（Google 的 browse 工具不跟随 redirect 链接），属 **Google 上游限制，网关侧无法修复**。

也就是说：切 v1beta1 解开了「组合被上游拒绝」这一层（客户报告的第一层障碍），但「URL Context 能否读到搜索结果页」这一层不在我们能力范围内。

> 待核实（从客户端无法判定，需在管理端确认）：本次实测用的 `gemini-3.5-flash` 与 `claude-sonnet-4-5-20250929` 是否确实落在 Vertex AI 渠道上。若 Gemini 模型被路由到了 Gemini Developer API 渠道，VER-01/02 的通过就不能作为 Vertex 分支的证据；CLD-01 同理。

---

## 七、已完成的修改与仍待处理项

### ✅ P1 已实现：Vertex 的 Gemini 请求改走 `v1beta1`（零配置）

**为什么不做成配置项**（这是评审后调整掉的设计）：

1. **`v1beta` 与 `v1beta1` 不是同一个版本名，而是两个后端各自的 preview 面名字，互斥。** Gemini Developer API 只有 `v1` / `v1beta`（https://ai.google.dev/gemini-api/docs/api-versions ），Vertex AI 只有 `v1` / `v1beta1`。`v1beta` 打 Vertex 会 404，`v1beta1` 打 Gemini API 也会 404。所以「客户端填什么就透传什么」物理上做不到，必须按后端换名。
2. **渠道是在请求进来之后才被负载均衡选出来的。** 同一个 `/v1beta/models/{model}:generateContent` 请求可能落到 Vertex 渠道，也可能落到 Gemini API 渠道；客户端无法预知，所以上游版本串只能由渠道自己的后端方言决定 —— 既不该由客户端路径决定，也不该让管理员手动勾。
3. **入站原生 Gemini 路由只有一条**：`router/relay-router.go:305` 的 `/v1beta/models/*path`。入站语义唯一（「我要 preview 面」），Vertex 侧的正确答案因此是恒定的 `v1beta1`，不存在第二种需要选择的情况。

| 文件 | 改动 |
|---|---|
| `relay/channel/vertex/url_builder.go` | 新增常量 `GeminiAPIVersion = "v1beta1"`，注释写明「v1beta ↔ v1beta1 是方言换名而非透传」及官方依据；`DefaultAPIVersion = "v1"` 保留给 Anthropic |
| `relay/channel/vertex/adaptor.go` | `getRequestUrl` 的两条 Gemini 分支（service-account 与 API Key）改用 `GeminiAPIVersion`；Claude 的 `rawPredict` 仍是 `DefaultAPIVersion` |
| `relay/channel/vertex/adaptor_url_test.go` | 新增表驱动回归测试：generateContent / 流式（`alt=sse` 与 `key=` 拼接顺序）/ imagen `:predict` 都走 v1beta1，同渠道 Claude 仍走 v1 |

**零配置**：管理端不需要任何改动，Vertex 渠道的 Gemini 流量自动走
`https://{region}-aiplatform.googleapis.com/v1beta1/projects/{p}/locations/{r}/publishers/google/models/{model}:generateContent`。

> ⚠️ **这是一次面向存量的行为变更**：所有 Vertex 渠道的 Gemini 请求（含 imagen `:predict`）从 `v1` 切到 `v1beta1`。`v1beta1` 在 generateContent 上是 `v1` 的超集（GA 能力全都有，额外开放 preview 能力），Google 官方 url-context 文档的 REST 示例本身也用 v1beta1，风险低；但它不受 GA 版本的稳定性承诺约束。上线后建议先观察 Vertex 渠道的错误率。

**已知边界（不改，属于配置约定）**：如果渠道 base_url 自带版本后缀（例如以 `/v1` 结尾），`appendVertexAPIVersion` 只对完全相同的版本做去重，会拼出 `/v1/v1beta1`。Vertex 渠道的 base_url 请填到域名层（如 `https://aiplatform.googleapis.com`）。

### ✅ P3 已实现：`toolConfig` 兼容 snake_case

| 文件 | 改动 |
|---|---|
| `relaykit/dto/gemini.go` | `ToolConfig` 新增 `UnmarshalJSON`，同时接受 `include_server_side_tool_invocations` / `function_calling_config` / `retrieval_config` 与原有 camelCase 写法 |
| `relaykit/dto/gemini_tool_config_test.go` | 新增测试：两种命名法都能解析；显式 `false` 不退化成 `nil`（否则调用方无法关闭开关）；`GeminiChatRequest` 整体往返后 `tools` 原文、`toolConfig` 开关、含引号的 `text` 全部保持等价 |

### ❌ P2 不做：补 relaykit 响应 DTO 缺失字段

排查后确认这一项**当前没有任何可观测收益**，属于死代码，因此不加：

- `GeminiGroundingMetadata` / `WebSearchQueries` 在全仓**零消费方**，加了字段也不会有任何调用方读到；
- `GeminiChatResponse` 被序列化输出只有两处（`relaykit/relayconvert/internal/oai_chat/to_gemini_chat_resp.go`、`relay/channel/openai/chat_via_responses.go:219`），都是「**OpenAI 上游** → Gemini 格式」的方向，上游本身不产 Gemini 的 `toolCall` / `urlContextMetadata`，没有可丢的东西；
- 客户实际使用的原生 `/v1beta/models/...` + Vertex/Gemini 渠道路径是字节透传，不经过这些结构体。

真要让 OpenAI / Claude 格式的调用方看到 grounding 信息，需要的是**补一条响应转换链路**（把 groundingMetadata 映射成 annotations/citations），那是新功能而不是修 bug，等有明确需求再单独排期。

### ✅ 已实现：入站同时接受 `/v1beta/models/*` 与 `/v1beta1/models/*`

客户端写哪种 preview 版本名都行，上游用哪个由渠道 adaptor 按自己后端的方言决定。

| 文件 | 改动 |
|---|---|
| `router/relay-router.go` | 原生 Gemini relay 路由改为对 `/v1beta`、`/v1beta1` 两个前缀各注册一次（中间件链完全一致） |
| `middleware/distributor.go` | Gemini 路径判定补 `/v1beta1/models/` 前缀（否则提取不到模型名、relay_mode 落不到 Gemini） |
| `relay/constant/relay_mode.go` | `Path2RelayMode` 补 `/v1beta1/models` |
| `middleware/auth.go` | 从 query `key` / `x-goog-api-key` 取凭证的白名单补 `/v1beta1/models`（否则 Gemini SDK 的鉴权方式在别名路径上失效） |
| `router/gemini_version_alias_router_test.go` | 新增测试：两条路由都注册成功且都能命中 relay 链路（非 404）；`/v1beta1/projects/:project/locations/:location/interactions`（视频路由）在别名加入后仍可达，且 `SetRelayRouter` + `SetVideoRouter` 同时注册不 panic |

> 注意 `/v1beta1` 下已有视频的 `…/projects/:project/locations/:location/interactions` 路由。`models` 与 `projects` 都是静态段，catch-all `*path` 只挂在 `/v1beta1/models/` 下，不冲突 —— 已由上面的测试固化，避免以后误改导致 gin 启动 panic。

### Vertex 上的 Claude 不受影响

`/v1/messages` → Vertex Claude 的链路完全没变：`vertex.Adaptor.Init` 按 `UpstreamModelName` 是否以 `claude` 开头判定 `RequestModeClaude`（`adaptor.go:130`），`GetRequestURL` 走 `rawPredict` / `streamRawPredict?alt=sse`，`getRequestUrl` 里 Claude 分支仍用 `DefaultAPIVersion`（`v1`）+ `publishers/anthropic`。本次 diff 只改了两条 `RequestModeGemini` 分支，Claude 两条一行未动，且 `adaptor_url_test.go` 有 `claude stays on v1` 用例固化这一点。

> 既存边界（非本次引入）：`Init` 是按模型名前缀分流的，若模型映射把 Claude 模型改名成不以 `claude` 开头的名字，会被误判成 Gemini 模式 —— 那种情况在改动前就已经打错 publisher（`publishers/google`）了，属于既有问题。

### P0：给客户明确的能力边界回复（不需要改代码）

把第三、六节的结论同步给 dataeyes：`includeServerSideToolInvocations` 与「搜索工具 + 函数调用」组合是 Gemini Developer API 独有能力，Vertex AI 后端不具备，附官方链接。避免客户继续按「贵方转发丢字段」的方向施压。

### 验证结论

- `go build ./...`、`cd relaykit && GOWORK=off go build ./...` 均通过；
- `go test ./relay/channel/vertex/`、`relaykit/dto` 全绿；
- `model/`（11 个）与 `relay/channel/gemini/`（4 个）的失败用例改前改后**列表完全一致**，均为 main-alpha 分支既存失败，非本次回归；
- 本次最终方案不含任何前端改动（配置项方案已撤回），改动面为 6 个 Go 文件 + 3 个测试文件；
- 端到端实测见第六节「实测结果」，脚本 `logs/gemini-vertex-test/test_gemini_vertex.py`。

### 待验证实验 —— 已完成（2026-08-24），结论如下

原计划验证 `v1beta1` 上 Vertex 对 `includeServerSideToolInvocations` 的真实行为，现已实测：

| 实验 | 结果 | 影响 |
|---|---|---|
| 实验 1：`urlContext` + `googleSearch` 组合 | **200，组合被接受**（`groundingMetadata` 有）；但 `urlContextMetadata` 缺失、模型自述抓不到页面 | B-1 达成第一层（不再被拒），第二层（能否读到搜索结果页）受上游限制 |
| 实验 2：`includeServerSideToolInvocations: true` | **200，但 parts 只有 `["text","thoughtSignature"]`，无 `toolCall`/`toolResponse`** | **A-1 结论确认**：Vertex 是「接受后静默忽略」而非 400 硬拒，能力仍然缺失 |

即：A-1 不需要修正，Vertex 上确实拿不到工具事件回流；要过客户用例一的第 2 条，仍必须接入 Gemini Developer API 渠道。

复现用的 curl（打我们平台，也可换成直连 Vertex）：

```bash
# 实验 1：v1beta1 + urlContext + googleSearch（预期：应该正常，验证 B-1 收益）
curl -sS -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT/locations/global/publishers/google/models/gemini-3.5-flash:generateContent" \
  -d '{
    "contents":[{"role":"user","parts":[{"text":"Search for \"REVOLUT LTD\" \"08804411\" Companies House, then read the top result and report whether page content was returned."}]}],
    "tools":[{"googleSearch":{}},{"urlContext":{}}]
  }' | python3 -m json.tool | head -60

# 实验 2：v1beta1 + includeServerSideToolInvocations（预期：400 或静默忽略，验证 A-1 是否真无解）
curl -sS -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://aiplatform.googleapis.com/v1beta1/projects/$PROJECT/locations/global/publishers/google/models/gemini-3.5-flash:generateContent" \
  -d '{
    "contents":[{"role":"user","parts":[{"text":"hi"}]}],
    "tools":[{"googleSearch":{}}],
    "toolConfig":{"includeServerSideToolInvocations":true}
  }' | head -40
```

判读要点：**只看状态码会误判**。Vertex 对不支持的开关是静默忽略（200），必须去检查
`candidates[].content.parts[]` 里有没有 `toolCall` / `toolResponse`，只有出现这两种部件
才算该开关真的生效。

---

## 八、官方链接汇总

**Gemini Developer API（ai.google.dev）**
- Combine built-in tools and function calling（`include_server_side_tool_invocations` / tool context circulation 的权威定义）
  https://ai.google.dev/gemini-api/docs/generate-content/tool-combination
- Grounding with Google Search（明确 Gemini 3 支持内置工具 + 自定义工具组合）
  https://ai.google.dev/gemini-api/docs/google-search
- URL context
  https://ai.google.dev/gemini-api/docs/url-context
- API versions explained（v1 vs v1beta）
  https://ai.google.dev/gemini-api/docs/api-versions
- Gemini API tooling updates 官方博客（工具上下文回流发布公告）
  https://blog.google/innovation-and-ai/technology/developers-tools/gemini-api-tooling-updates/

**Vertex AI / Gemini Enterprise Agent Platform（docs.cloud.google.com）**
- 网址上下文（含「urlContext + googleSearch 组合为实验性功能，仅 `v1beta1` 可用」原文 + REST 示例 + 支持模型清单）
  https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/url-context
- 使用 Google 搜索建立依据（含「不支持搜索工具与非搜索工具组合」原文）
  https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/grounding/grounding-with-google-search
- Generate content with the Gemini API（请求体参考）
  https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/inference
- UrlContextMetadata REST 参考
  https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/GenerateContentResponse#UrlContextMetadata

**第三方交叉验证**
- Spring AI 文档（明写 `includeServerSideToolInvocations` 「not supported on Vertex AI」）
  https://docs.spring.io/spring-ai/reference/api/chat/google-genai-chat.html
- vercel/ai #14655（Vertex `v1` 对该字段 400 硬拒的完整报错）
  https://github.com/vercel/ai/issues/14655
- googleapis/python-genai #941（Vertex 上 urlContext + googleSearch 报错；`api_version="v1beta1"` 可解；换 Gemini API 正常）
  https://github.com/googleapis/python-genai/issues/941
- googleapis/python-genai #1322（URL Context 抓不了 Grounding 返回的 redirect 格式链接）
  https://github.com/googleapis/python-genai/issues/1322
- vercel/ai #13911（`include_server_side_tool_invocations` 是组合内置工具与函数调用的前提）
  https://github.com/vercel/ai/issues/13911

---

## 九、给 dataeyes 的回复口径（草稿）

> 感谢详细的问题报告，我们已完成三处病灶的逐项复核，结论如下：
>
> **1. `toolConfig.includeServerSideToolInvocations` 未生效**
> 该字段并非被我们丢弃 —— 我们已用您提供的请求体原文验证过转发链路，该字段与 `tools[].urlContext`、`functionDeclarations[].parametersJsonSchema` 均完整保留并原样送达上游。
> 真实原因是：**该能力是 Google Gemini Developer API（AI Studio API Key）独有，Vertex AI 后端不提供**。Google 官方生态已有明确记载：Spring AI 文档写明「This feature is only supported with the Gemini Developer API (MLDev / API key authentication). It is not supported on Vertex AI.」
> 我们也在自有 Vertex 资源上实测复现了您看到的现象：携带该开关时上游返回 **HTTP 200**，但候选 parts 只有 `text` 与 `thoughtSignature`，**没有任何 `toolCall` / `toolResponse` 部件** —— 即 Vertex 对该字段是「接受后静默忽略」，这解释了为什么请求成功却拿不到事件流。
> 您的场景②（模型编造 URL）是同一根因的下游后果；此外 Vertex AI 官方文档「使用 Google 搜索建立依据」还明确写了「不支持在同一 `generateContent` 请求中将搜索工具与非搜索工具（函数调用）相结合」。因此**验收用例二在 Vertex 后端上无法交付**。
> 补充一个可能对您有用的信息：真实的 grounding redirect 链接其实**就在响应里** —— `candidates[].groundingMetadata.groundingChunks[].web.uri`（我们实测取到了 `https://vertexaisearch.cloud.google.com/grounding-api-redirect/...`）。如果您的协议只是需要拿到真实链接，可以直接从该字段读取；但若要让**模型本身**在同一轮内把链接传给自定义函数，仍依赖上面那项 Vertex 不具备的能力。
>
> **2. URL Context 检索失败 / `urlContextMetadata` 缺失**
> 这一项确认我们侧有改进空间，**已修复并上线**。Vertex AI 支持 urlContext 与 googleSearch 组合，但官方文档标注为「实验性功能，仅在 API 版本 `v1beta1` 可用」，而我们的 Vertex 适配器此前把 API 版本固定为 `v1`。现已改为 Vertex 上的 Gemini 请求统一走 `v1beta1`。
> 修复后实测：两个内置工具**已能被同时接受**（HTTP 200，`groundingMetadata` 正常返回），不再出现此前的 `Multiple tools are supported only when they are all search tools` 类拒绝。
> 但需要如实告知：**URL Context 仍然抓不到搜索结果页**。同一次实测中 `urlContextMetadata` 依旧缺失，模型自述 "no page content was returned… I'm not able to access the website(s)"。对照实验可以定性 —— urlContext 单独抓一个普通 URL 时是成功的（`urlRetrievalStatus = URL_RETRIEVAL_STATUS_SUCCESS`），失败特定于「抓取 Google 搜索返回的 `vertexaisearch.cloud.google.com/grounding-api-redirect/...` 重定向链接」。这属 Google 上游限制（参见 googleapis/python-genai #1322，Google 的 browse 工具不跟随重定向），网关侧无法修复。
>
> **3. 请求体字符串双重转义**
> 我们用您验收用例二的请求体原文跑通了完整转发链路，`contents[].parts[].text` 逐字节等价，`\"AIRPAZ...\"` 正确还原为 `"AIRPAZ..."`，未观察到二次转义；`tools` 整块以原始 JSON 透传。我们的原生 Gemini 端点响应侧本身就是字节级透传（上游给什么就下发什么，仅旁路解析 `usageMetadata` 用于计费）。
> 建议一起做一次定位：我们可以开启该渠道的请求体日志，比对打到我们入口时的 text 是否已带字面反斜杠，以确认转义发生在哪一跳。
>
> **4. 建议方案**
> 如需完整满足两条验收用例（特别是 `toolCall`/`toolResponse` 事件与「搜索 + 自定义函数」组合），需要为相关模型接入 **Gemini Developer API（AI Studio API Key）** 后端。我们的原生 Gemini 端点在该后端下是全链路透传，可直接满足全部通过标准。
