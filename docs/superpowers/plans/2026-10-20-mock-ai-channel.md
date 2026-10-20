# Mock AI 渠道类型 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增渠道类型 66 `Mock AI`，不发真实上游请求，就地合成与真实上游逐字节同构的响应，让它流过完整的解析→聚合→计费→写日志链路。

**Architecture:** `DoRequest` 返回一个合成的 `*http.Response`，其 `Body` 是按时间节奏吐字节的 `io.ReadCloser`；`DoResponse` 直接委托给 `claude` / `openai` 包里真实的响应处理器。Mock 自己不解析、不聚合、不构造 `dto.Usage`。配置存在已有的 `channels.settings` JSON 列里，不加数据库列。

**Tech Stack:** Go 1.22 + Gin + GORM（后端）；React + Semi（`web/classic`）；Vue 3 + Ant Design Vue（`gravitex-api-admin`）

**Spec:** `docs/superpowers/specs/2026-10-20-mock-ai-channel-design.md`

## Global Constraints

- JSON 序列化/反序列化一律用 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr`，禁止直接调用 `encoding/json`。
- `relaykit/` 下的代码不得 import root module 任何包；改动后必须跑 `cd relaykit && GOWORK=off go build ./...`。
- 日志 content、SysLog、error message 一律英文。
- 渠道类型号固定 `66`，`ChannelTypeDummy` 顺延到 `67`。
- 不新增数据库列；配置写进已有的 `channels.settings`（Go 侧 `Channel.OtherSettings`，JSON tag `settings`）。
- Go 测试用 `github.com/stretchr/testify/require`（致命）与 `assert`（非致命）。
- 提交信息格式：`feat:bingyu xxx` / `fix:bingyu. xxx` / `style:bingyu xxx`，不带任何额外标识或协作作者行。
- 不得修改 `new-api` / `QuantumNous` 相关标识。
- token 区间硬上限对齐 `maxTokensLimit`（`relay/helper/valid_request.go`）；时延硬上限 600000ms。

## Review Focus

以下输入类别 spec 蕴含但容易没人测，按"最可能咬到人"排序。每条都已指派给拥有该代码的任务：

1. **客户端中途断开**：节奏化 reader 必须立刻停止等待并返回，不能把 goroutine 挂满整个 latency 窗口。→ Task 4
2. **渠道 `settings` 里没有 `mock_ai` 字段**（老渠道被改成 Mock 类型但没配）：必须回落到一套内置默认档位，不能 panic、不能零 token。→ Task 3
3. **`CacheCreation1hRatio` 舍入**：`ephemeral_5m + ephemeral_1h` 必须严格等于 `cache_creation_input_tokens`，舍入误差不能让两者对不上。→ Task 6
4. **区间上下界相等 / 只有一片正文 frame**：`Min == Max` 的随机取值与 frame 数为 1 时的时间均分计算都不能 panic 或除零。→ Task 3、Task 4
5. **请求的模型名不在 `Models` 里**：必须静默回落到 `Default` 档位并正常计费，不能报错。→ Task 3

---

### Task 1: 渠道类型注册与配置结构

**Files:**
- Create: `relaykit/dto/mock_ai.go`
- Create: `relaykit/dto/mock_ai_test.go`
- Modify: `relaykit/dto/channel_settings.go`（`ChannelOtherSettings` 加字段）
- Modify: `constant/channel.go`、`constant/api_type.go`、`common/api_type.go`、`common/endpoint_type.go`
- Modify: `model/channel.go:1217-1240`（校验钩子）

**Interfaces:**
- Consumes: 无
- Produces: `dto.MockAIConfig`、`dto.MockAIEndpoints`、`dto.MockAIRange`、`dto.MockAIModelProfile`（字段定义见 spec §5.2）；`(c *MockAIConfig) Validate() error`；`ChannelOtherSettings.MockAI *MockAIConfig`，JSON tag `mock_ai,omitempty`；`constant.ChannelTypeMockAI = 66`；`constant.APITypeMockAI`

- [ ] **Step 1: 写失败测试** `relaykit/dto/mock_ai_test.go`

表驱动 `TestMockAIConfigValidate`，每行 `{name, config, wantErr}`，覆盖：
`Min > Max`（TextInput）；`Min < 0`；`CacheCreation1hRatio = -0.1`；`CacheCreation1hRatio = 1.1`；`LatencyMs.Min == FirstTokenMs.Max`（必须报错，要求严格大于）；`LatencyMs.Min > FirstTokenMs.Max`（通过）；三个端点全 false；`TextInput.Max` 超过 token 硬上限；`LatencyMs.Max = 600001`；一个全合法的完整配置（通过）。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/caihongzhan/gravitexSpace/api/gravitex-api && go test ./relaykit/dto/ -run TestMockAIConfigValidate -v`
Expected: 编译失败，`undefined: MockAIConfig`

- [ ] **Step 3: 在 `relaykit/dto/mock_ai.go` 实现结构体与 `Validate()`**

结构体字段与 JSON tag 严格照 spec §5.2 的代码块抄。`Validate()` 逐条检查 Step 1 列出的约束，错误信息用英文。token 硬上限在本文件定义一个包级常量（不能 import root module 的 `relay/helper`，照 `maxTokensLimit` 的数值复制一份并写注释说明来源）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relaykit/dto/ -run TestMockAIConfigValidate -v`
Expected: PASS

- [ ] **Step 5: 接上渠道类型注册点**

按 spec §5.1 的表格逐项修改：`constant/channel.go`（常量、`ChannelBaseURLs` 第 66 项空串、`ChannelTypeNames` 补 `"Mock AI"`）、`constant/api_type.go`（`APITypeMockAI` 插在 `APITypeNewAPI` 之后、`APITypeDummy` 之前）、`common/api_type.go` 的 `ChannelType2APIType`、`common/endpoint_type.go` 的 `GetEndpointTypesByChannelType`（返回 `EndpointTypeOpenAI`、`EndpointTypeOpenAIResponse`、`EndpointTypeAnthropic` 三者）。
`ChannelOtherSettings` 加 `MockAI *MockAIConfig`。
`model/channel.go` 的校验钩子里，照 `AdvancedCustom` 那几行的样子，在 `channel.Type == constant.ChannelTypeMockAI` 时要求 `MockAI != nil` 并调用其 `Validate()`。

- [ ] **Step 6: 验证构建**

Run: `cd relaykit && GOWORK=off go build ./... && cd .. && go build ./...`
Expected: 两条都无输出

- [ ] **Step 7: 提交**

```bash
git add relaykit/dto/mock_ai.go relaykit/dto/mock_ai_test.go relaykit/dto/channel_settings.go constant/ common/api_type.go common/endpoint_type.go model/channel.go
git commit -m "feat:bingyu 新增 Mock AI 渠道类型常量与配置结构"
```

---

### Task 2: 固定回答文案池

**Files:**
- Create: `relay/channel/mockai/answers.go`
- Create: `relay/channel/mockai/answers_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `var mockAnswers []string`（包级，100 条）

- [ ] **Step 1: 写失败测试** `relay/channel/mockai/answers_test.go`

`TestMockAnswers` 断言：`len(mockAnswers) == 100`；每条 `strings.TrimSpace` 后非空；每条 `utf8.RuneCountInString` ≤ 100；无重复（用 map 查）；每条都不含下列任一子串（大小写不敏感）：`mock`、`测试`、`占位`、`示例`、`placeholder`、`AI 助手`、`AI助手`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -run TestMockAnswers -v`
Expected: 编译失败，`undefined: mockAnswers`

- [ ] **Step 3: 写 `relay/channel/mockai/answers.go`**

一个包级 `var mockAnswers = []string{...}`，100 条。内容为科普、冷知识、生活小技巧、笑话，中英文混排均可。每条读起来必须是一个独立成立的正常回答，不带编号、不带模板痕迹、不自我指涉。不用 `go:embed`，不引外部资源文件。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -run TestMockAnswers -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add relay/channel/mockai/answers.go relay/channel/mockai/answers_test.go
git commit -m "feat:bingyu 新增 Mock AI 渠道固定回答文案池"
```

---

### Task 3: 档位查找与随机取值

**Files:**
- Create: `relay/channel/mockai/profile.go`
- Create: `relay/channel/mockai/profile_test.go`

**Interfaces:**
- Consumes: Task 1 的 `dto.MockAIConfig` / `dto.MockAIModelProfile`
- Produces:
  - `func resolveProfile(cfg *dto.MockAIConfig, originModel string) dto.MockAIModelProfile`
  - `type rollSource interface { Intn(n int) int }`（注入点，生产用 `math/rand`）
  - `type mockRoll struct { TextInput, TextOutput, CacheRead, CacheCreation, CacheCreation5m, CacheCreation1h, Reasoning int; LatencyMs, FirstTokenMs int }`
  - `func roll(p dto.MockAIModelProfile, src rollSource) mockRoll`
  - `var defaultProfile dto.MockAIModelProfile`（`cfg == nil` 时的内置兜底）

- [ ] **Step 1: 写失败测试** `relay/channel/mockai/profile_test.go`

`TestResolveProfile`：
- 配置里 `Models["gpt-4o"]` 存在，传 `"gpt-4o"` → 返回该条目（**Review Focus #5 的反例**）。
- 传 `"not-configured"` → 返回 `cfg.Default`（**Review Focus #5**）。
- `cfg == nil` → 返回 `defaultProfile`，且其 `TextInput.Max > 0`、`LatencyMs.Min > FirstTokenMs.Max`（**Review Focus #2**）。

`TestRollWithinRange`：用一个固定返回 0 的 `rollSource` 和一个固定返回 `n-1` 的 `rollSource` 各跑一次，断言每个字段分别落在区间下界和上界上。

`TestRollDegenerateRange`：所有区间都填 `Min == Max` 时不 panic，且每个字段恰好等于该值（**Review Focus #4**）。

`TestRollCacheCreationSplit`：`CacheCreation` 固定、`CacheCreation1hRatio` 取 `0`、`0.5`、`1`、`0.333` 四种，断言 `CacheCreation5m + CacheCreation1h == CacheCreation` 恒成立（**Review Focus #3**）。

`TestRollReasoningDisabled`：`Reasoning == nil` 时 `mockRoll.Reasoning == 0`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -run 'TestResolveProfile|TestRoll' -v`
Expected: 编译失败

- [ ] **Step 3: 实现 `relay/channel/mockai/profile.go`**

`resolveProfile` 按 **重定向之前的原始模型名** 查 `cfg.Models`，未命中回落 `cfg.Default`，`cfg == nil` 回落 `defaultProfile`。
`roll` 对每个区间取 `min + src.Intn(max-min+1)`（注意 `max == min` 时 `Intn(1)`）。
缓存创建拆分：先算 `h1 = int(math.Round(float64(CacheCreation) * ratio))`，再 `m5 = CacheCreation - h1`，保证两者之和恒等。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -run 'TestResolveProfile|TestRoll' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add relay/channel/mockai/profile.go relay/channel/mockai/profile_test.go
git commit -m "feat:bingyu 新增 Mock AI 渠道档位查找与用量取值"
```

---

### Task 4: 适配器骨架 + 节奏化 Body + OpenAI Chat 格式

**Files:**
- Create: `relay/channel/mockai/adaptor.go`
- Create: `relay/channel/mockai/paced_body.go`
- Create: `relay/channel/mockai/wire_openai.go`
- Create: `relay/channel/mockai/adaptor_openai_test.go`
- Create: `relay/channel/mockai/paced_body_test.go`
- Modify: `relay/relay_adaptor.go`（`GetAdaptor` 加 case）

**Interfaces:**
- Consumes: Task 2 的 `mockAnswers`；Task 3 的 `resolveProfile` / `roll` / `mockRoll`
- Produces:
  - `type Adaptor struct{}`，实现 `channel.Adaptor` 全部方法
  - `func newPacedBody(ctx context.Context, frames [][]byte, firstDelay, totalDelay time.Duration, sleep func(context.Context, time.Duration) error) io.ReadCloser`
  - `func buildOpenAIChatFrames(r mockRoll, answer, model string, stream bool) [][]byte`
  - `const ChannelName = "mock_ai"`
- 后续 Task 5/6 复用 `newPacedBody` 与 `Adaptor.DoRequest` 的分发骨架

- [ ] **Step 1: 写失败测试** `relay/channel/mockai/paced_body_test.go`

`TestPacedBodyCancellation`：用一个已 cancel 的 `context.Context` 构造 reader，断言 `io.ReadAll` 在毫秒级返回且返回 `context.Canceled`，不等待 `totalDelay`（**Review Focus #1**）。
`TestPacedBodyEmitsAllFrames`：注入一个立即返回 nil 的 `sleep`，断言读出的字节等于所有 frame 拼接。
`TestPacedBodySingleFrame`：`frames` 只有 1 片时不 panic、不除零，且等待总时长为 `totalDelay`（**Review Focus #4**）。

- [ ] **Step 2: 写失败测试** `relay/channel/mockai/adaptor_openai_test.go`

`TestOpenAIChatEndToEndUsage`（非流式）：构造 `gin` 测试 context 与 `relaycommon.RelayInfo`（`RelayFormat = types.RelayFormatOpenAI`，`IsStream = false`），用固定的 `mockRoll`（`TextInput=100, CacheRead=40, TextOutput=25, Reasoning=5`）走 `Adaptor.DoRequest` → `Adaptor.DoResponse`，断言返回的 `*dto.Usage` 满足 spec §5.4 的换算表：
`PromptTokens == 140`、`PromptTokensDetails.CachedTokens == 40`、`CompletionTokens == 25`、`CompletionTokenDetails.ReasoningTokens == 5`、`TotalTokens == 165`。

`TestOpenAIChatStreamEndToEndUsage`（流式）：同样的输入，`IsStream = true`，断言得到相同的 `*dto.Usage`，且**客户端没有传 `stream_options.include_usage`** 时流里依然带 usage。

`TestEndpointNotEnabled`：配置里 `Endpoints.ChatCompletions = false`，断言返回的 `*types.NewAPIError` 状态码为 404、code 为 `endpoint_not_found`，且带 `NoRecordErrorLog`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -v`
Expected: 编译失败，`undefined: Adaptor`

- [ ] **Step 4: 实现三个文件**

`paced_body.go`：`newPacedBody` 返回的 reader 在首次 `Read` 前等 `firstDelay`，随后把剩余 frame 在 `totalDelay - firstDelay` 窗口内均匀铺开。等待走注入的 `sleep`，生产实现用 `select { case <-ctx.Done(): case <-time.After(d): }`。非流式即 `frames` 只有一片、`firstDelay == totalDelay`。

`wire_openai.go`：`buildOpenAIChatFrames` 产出**上游** OpenAI 格式字节——非流式是一个完整 JSON body；流式是若干 `data: {...}\n\n` 的 chunk 加 `data: [DONE]\n\n`，正文按 rune 切片，usage 放在最后一个带 usage 的 chunk 里（无条件下发，不看 `include_usage`）。usage 字段按 spec §5.4 的 chat/completions 换算表填。

`adaptor.go`：`DoRequest` 按 `info.RelayFormat` 选 wire builder，组装 `*http.Response`（`StatusCode: 200`，流式 `Content-Type: text/event-stream`、非流式 `application/json`，Body 用 `newPacedBody`）。端点未勾选时返回 404 错误，形态照 `relay/responses_handler.go:33-41` 那段（`types.WithOpenAIError` + `ErrOptionWithSkipRetry` + `ErrOptionWithNoRecordErrorLog`）。`DoResponse` 在 `RelayFormatOpenAI` 下委托 `openai.OaiStreamHandler(c, info, resp)` / `openai.OpenaiHandler(c, info, resp)`。其余接口方法：`Init` / `SetupRequestHeader` 空实现，`GetRequestURL` 返回 `""`，`Convert*Request` 原样返回入参，`GetModelList` 返回 `nil`，`GetChannelName` 返回 `ChannelName`。

`relay/relay_adaptor.go`：`GetAdaptor` 加 `case constant.APITypeMockAI: return &mockai.Adaptor{}`。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -v && go build ./...`
Expected: PASS，构建无输出

- [ ] **Step 6: 确认流式看门狗阈值**

读 `relay/helper/stream_scanner.go`，确认 `StreamScannerHandler` 是否存在空闲超时看门狗及其阈值。若该阈值小于 `relaykit/dto/mock_ai.go` 里的 600000ms 时延硬上限，把硬上限收紧到看门狗阈值以下，并在 `mock_ai.go` 的常量旁写注释说明来源。
Expected: 要么确认无冲突，要么硬上限已收紧且 Task 1 的校验测试同步更新

- [ ] **Step 7: 提交**

```bash
git add relay/channel/mockai/ relay/relay_adaptor.go relaykit/dto/mock_ai.go
git commit -m "feat:bingyu 新增 Mock AI 渠道适配器与 chat/completions 端点"
```

---

### Task 5: Responses 格式

**Files:**
- Create: `relay/channel/mockai/wire_responses.go`
- Create: `relay/channel/mockai/adaptor_responses_test.go`
- Modify: `relay/channel/mockai/adaptor.go`（`DoRequest` / `DoResponse` 加 Responses 分支）

**Interfaces:**
- Consumes: Task 4 的 `newPacedBody`、`Adaptor`
- Produces: `func buildResponsesFrames(r mockRoll, answer, model string, stream bool) [][]byte`

- [ ] **Step 1: 写失败测试** `relay/channel/mockai/adaptor_responses_test.go`

`TestResponsesEndToEndUsage` 与 `TestResponsesStreamEndToEndUsage`：照 Task 4 测试的形状，`RelayFormat = types.RelayFormatOpenAIResponses`、`RelayMode = relayconstant.RelayModeResponses`，同样的 `mockRoll` 输入，断言 `*dto.Usage` 满足 spec §5.4 的 responses 换算表：`InputTokens == 140`、`InputTokensDetails.CachedTokens == 40`、`OutputTokens == 25`、`CompletionTokenDetails.ReasoningTokens == 5`、`TotalTokens == 165`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -run TestResponses -v`
Expected: FAIL

- [ ] **Step 3: 实现 `wire_responses.go` 并接进 `adaptor.go`**

流式需产出合法的 Responses 事件序列（至少 `response.created`、若干 `response.output_text.delta`、`response.completed`，usage 在 `response.completed` 里）。`DoResponse` 在该格式下委托 `openai.OaiResponsesStreamHandler(c, info, resp)` / `openai.OaiResponsesHandler(c, info, resp)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add relay/channel/mockai/
git commit -m "feat:bingyu Mock AI 渠道支持 responses 端点"
```

---

### Task 6: Claude Messages 格式

**Files:**
- Create: `relay/channel/mockai/wire_claude.go`
- Create: `relay/channel/mockai/adaptor_claude_test.go`
- Modify: `relay/channel/mockai/adaptor.go`

**Interfaces:**
- Consumes: Task 4 的 `newPacedBody`、`Adaptor`
- Produces: `func buildClaudeFrames(r mockRoll, answer, model string, stream bool) [][]byte`

- [ ] **Step 1: 写失败测试** `relay/channel/mockai/adaptor_claude_test.go`

`TestClaudeEndToEndUsage` / `TestClaudeStreamEndToEndUsage`：`RelayFormat = types.RelayFormatClaude`，输入 `TextInput=100, CacheRead=40, CacheCreation=60, CacheCreation1hRatio=0.5, TextOutput=25`，断言 `*dto.Usage`：
`PromptTokens == 100`（**不含缓存**，与 OpenAI 侧的 140 形成对照）、`PromptTokensDetails.CachedTokens == 40`、`PromptTokensDetails.CachedCreationTokens == 60`、`ClaudeCacheCreation5mTokens == 30`、`ClaudeCacheCreation1hTokens == 30`、`CompletionTokens == 25`。

`TestClaudeStreamUsageConsistency`：解析合成的 SSE，断言 `message_start` 与 `message_delta` 两处 usage 的 `cache_creation` 子对象数字一致。

`TestClaudeCacheCreationSplitSumsExact`：`CacheCreation=7`、`ratio=0.333` 这类会产生舍入的组合，断言最终 `dto.Usage` 里 5m + 1h 严格等于 60/7 对应的 `CachedCreationTokens`（**Review Focus #3 的端到端版本**）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -run TestClaude -v`
Expected: FAIL

- [ ] **Step 3: 实现 `wire_claude.go` 并接进 `adaptor.go`**

流式需产出合法的 Claude 事件序列：`message_start`（带完整 usage，含 `cache_creation` 子对象）、`content_block_start`、若干 `content_block_delta`、`content_block_stop`、`message_delta`（带 `output_tokens` 与同样的 `cache_creation` 子对象）、`message_stop`。`stop_reason` 固定 `end_turn`。`DoResponse` 在该格式下委托 `claude.ClaudeStreamHandler(c, resp, info)` / `claude.ClaudeHandler(c, resp, info)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add relay/channel/mockai/
git commit -m "feat:bingyu Mock AI 渠道支持 messages 端点"
```

---

### Task 7: 渠道测试与健康检查跳过时延

**Files:**
- Modify: `constant/context_key.go`（新增 key）
- Modify: `controller/channel-test.go:167-175` 附近（设置 key）
- Modify: `relay/channel/mockai/adaptor.go`
- Create: `relay/channel/mockai/adaptor_channel_test_test.go`

**Interfaces:**
- Consumes: Task 4 的 `Adaptor`
- Produces: `constant.ContextKeyChannelTesting ContextKey = "channel_testing"`

- [ ] **Step 1: 写失败测试**

`TestSkipLatencyUnderChannelTest`：配一个 `LatencyMs` 为 `{30000, 30000}` 的档位，在 context 上用 `common.SetContextKey(c, constant.ContextKeyChannelTesting, true)` 标记，断言 `DoRequest` + 读完 Body 的耗时 < 1 秒。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./relay/channel/mockai/ -run TestSkipLatency -v`
Expected: FAIL（耗时 30 秒或编译失败）

- [ ] **Step 3: 实现**

`constant/context_key.go` 加常量。`controller/channel-test.go` 的 `testChannel` 在那批 `c.Set(...)` 旁边加一行设置该 key（健康检查复用 `testChannel`，所以一处即可覆盖两条路径）。`adaptor.go` 在构造 `newPacedBody` 前用 `common.GetContextKeyBool(c, constant.ContextKeyChannelTesting)` 判断，为真时把 `firstDelay` 与 `totalDelay` 都置 0。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./relay/channel/mockai/ -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add constant/context_key.go controller/channel-test.go relay/channel/mockai/
git commit -m "feat:bingyu Mock AI 渠道测试路径跳过模拟时延"
```

---

### Task 8: 定价端点来源接线

**Files:**
- Modify: `model/pricing.go`（`getPricingEndpointTypesForAbility` 及其配置预加载）

**Interfaces:**
- Consumes: Task 1 的 `dto.MockAIConfig`
- Produces: 无（内部接线）

- [ ] **Step 1: 写失败测试**

在 `model/` 下新增 `TestMockAIPricingEndpointTypes`：构造一条 `ChannelType == ChannelTypeMockAI` 且只勾选了 `Messages` 的 ability，断言 `getPricingEndpointTypesForAbility` 只返回 `EndpointTypeAnthropic`；未配置（`MockAI == nil`）时返回 `GetEndpointTypesByChannelType` 的三端点全集。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./model/ -run TestMockAIPricingEndpointTypes -v`
Expected: FAIL

- [ ] **Step 3: 实现**

照 `ChannelTypeAdvancedCustom` 的既有写法扩展：预加载该渠道的 `MockAI` 配置（沿用 `loadPricingAdvancedCustomConfigs` 同样的按 channelId 聚合模式，**遵守其中注释记录的 `updatePricingLock -> channelSyncLock` 锁序**），在 `getPricingEndpointTypesForAbility` 里按勾选的端点过滤。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./model/ -run TestMockAIPricingEndpointTypes -v && go build ./...`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add model/pricing.go model/
git commit -m "feat:bingyu Mock AI 渠道按配置上报支持的端点"
```

---

### Task 9: `web/classic` 编辑器与 `web/default` 类型名

**Files:**
- Modify: `web/classic/src/constants/channel.constants.js`（`CHANNEL_OPTIONS` 加 66）
- Modify: `web/classic/src/components/table/channels/modals/EditChannelModal.jsx`
- Create: `web/classic/src/components/table/channels/modals/MockAISettingsPanel.jsx`
- Modify: `web/default/src/features/channels/constants.ts`（仅加类型名）

**Interfaces:**
- Consumes: Task 1 定下的 `settings.mock_ai` JSON 结构
- Produces: 无（后续 Task 10 独立实现同形态的 Vue 版本）

- [ ] **Step 1: `CHANNEL_OPTIONS` 加一项**

`{ value: 66, color: 'orange', label: 'Mock AI' }`。不加进 `CHANNEL_DEPRECATED_TYPES`。

- [ ] **Step 2: 写 `MockAISettingsPanel.jsx`**

仅在 `inputs.type === 66` 时渲染，内容照 spec §6「编辑器内容」的 7 项：三个端点勾选框（默认全开）、默认档位区间组、按模型配置表格（模型名下拉源自 `inputs.models`）、缓存创建与 1h 占比两列在未勾选 messages 时置灰、思考输出开关、保存前校验、顶部醒目警告条。读写走 `EditChannelModal.jsx` 里已有的 settings-JSON 助手（`web/classic/src/components/table/channels/modals/EditChannelModal.jsx:598-609` 那个按 key 写入 `inputs.settings` 的函数），键名 `mock_ai`。

- [ ] **Step 3: 接上 default 分组二次确认**

保存时若 `inputs.group` 含 `default`，弹确认框，文案见 spec §7 第 1 条。确认后照常保存，取消则中止。

- [ ] **Step 4: `web/default` 加类型名**

`web/default/src/features/channels/constants.ts` 的 `CHANNEL_TYPES` 加 `66: 'Mock AI'`。不做编辑器。

- [ ] **Step 5: 验证**

Run: `cd web/classic && bun run eslint && bun run build`
Expected: 无 lint 错误、构建成功

- [ ] **Step 6: 提交**

```bash
git add web/classic/src web/default/src/features/channels/constants.ts
git commit -m "feat:bingyu classic 渠道管理支持 Mock AI 类型配置"
```

---

### Task 10: Vue 管理端编辑器

**Files:**
- Modify: `gravitex-api-admin/apps/web-antd/src/views/gravitex-api/channel/channel-form.vue`
- Create: `gravitex-api-admin/apps/web-antd/src/views/gravitex-api/channel/mock-ai-settings.vue`
- Modify: Vue 侧 `CHANNEL_OPTIONS` 所在文件

**Interfaces:**
- Consumes: Task 1 定下的 `settings.mock_ai` JSON 结构（与 Task 9 完全一致）
- Produces: 无

- [ ] **Step 1: `CHANNEL_OPTIONS` 加 66 `Mock AI`**

- [ ] **Step 2: 写 `mock-ai-settings.vue`**

内容与校验规则与 Task 9 的 classic 面板**逐项一致**（同样的 7 项、同样的字段名、同样的校验）。读写接进 `channel-form.vue:360-363` 那段 `settingsJson = { ...existingSettings }` 的合并逻辑，键名 `mock_ai`；仅在 `formState.type === 66` 时写入。加一个 `TYPE_MOCK_AI = 66` 常量，放在文件顶部那组渠道类型常量旁边。

- [ ] **Step 3: 接上 default 分组二次确认**

与 Task 9 Step 3 同样的文案与行为。

- [ ] **Step 4: 验证**

Run: `cd /Users/caihongzhan/gravitexSpace/api/gravitex-api-admin && bun run test:unit && bun run build`
Expected: 测试通过、构建成功

- [ ] **Step 5: 提交**（在 `gravitex-api-admin` 仓库）

```bash
git add apps/web-antd/src/views/gravitex-api/channel/
git commit -m "feat:bingyu 管理端渠道管理支持 Mock AI 类型配置"
```

---

### Task 11: 三库启动验证与端到端验收

**Files:** 无代码改动

- [ ] **Step 1: 全量测试与构建**

Run: `go test ./... && go build ./... && cd relaykit && GOWORK=off go build ./...`
Expected: 全绿。注意 `main-alpha` 上有 3 个**既存**失败测试（theme_compat panic 与两个 usedata_flow），不要误判成本次回归。

- [ ] **Step 2: 三库启动**

分别用 SQLite、MySQL、PostgreSQL 各启动一次，每种启动两次确认幂等。
Expected: 日志里无针对 `channels` 表的 `ALTER TABLE`

- [ ] **Step 3: 端到端验收**

按 spec §10 的 8 个步骤逐条执行并记录结果，含前置条件（先在「分组与模型定价设置」里给 Mock 模型配好倍率）。

- [ ] **Step 4: 记录**

把三种数据库的确切版本、命令与结果写进交接说明（`AGENTS.md` 要求）。
