# Record Converted Usage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist the Claude-form usage delivered to clients when an OpenAI-compatible upstream response is converted, so `LogUsageConversionEnabled=true` records it alongside `upstream_responses`.

**Architecture:** Keep raw upstream usage capture unchanged. In the OpenAI response handlers, after `relayconvert` produces a Claude or Gemini response, pass `convertResult.Usage` to `RelayInfo.SetUsageConversion`; it is then conditionally emitted by the existing log service only for requests whose format changed. Cover both non-streaming and streaming response conversion paths.

**Tech Stack:** Go 1.22, Gin, relaykit/relayconvert, testify.

## Global Constraints

- Do not modify billing calculations, raw upstream response capture, or client response payloads.
- Use `common` JSON wrappers in root-module business code; do not add direct `encoding/json` marshal/unmarshal calls.
- Preserve explicit request scalar semantics and support existing channel types without channel-specific branching.
- Keep `relaykit/` independently buildable; no relaykit source change is needed.
- Work directly on `main-alpha` with the user's explicit approval; preserve unrelated `web/bun.lock` modification.

---

### Task 1: Record usage after OpenAI-to-Claude response conversion

**Files:**
- Modify: `relay/channel/openai/relay-openai.go:339-360`
- Test: `relay/channel/openai/relay-openai_test.go`

**Interfaces:**
- Consumes: `relayconvert.ConvertResponse(c, info, types.RelayFormatClaude, &simpleResponse) (*relayconvert.ResponseResult, error)`.
- Produces: `relayInfo.UsageConversion`, populated through `relayInfo.SetUsageConversion(convertResult.Usage)`.

- [ ] **Step 1: Write the failing test**

Add a handler-level test with an OpenAI non-stream response containing `prompt_tokens=39`, `completion_tokens=276`, `reasoning_tokens=195`, a Claude relay format, and a `RelayInfo`. Assert after handling that `UsageConversion` equals the Claude payload usage fields:

```go
require.Equal(t, map[string]any{
    "input_tokens": float64(39),
    "output_tokens": float64(276),
    "cache_creation_input_tokens": float64(0),
    "cache_read_input_tokens": float64(0),
}, info.UsageConversion)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./relay/channel/openai -run TestOpenaiHandlerRecordsClaudeUsageConversion -count=1`

Expected: failure because `info.UsageConversion` is nil.

- [ ] **Step 3: Write minimal implementation**

Immediately after successful `relayconvert.ConvertResponse` in the `RelayFormatClaude` branch, add:

```go
if convertResult.Usage != nil {
    info.SetUsageConversion(convertResult.Usage)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./relay/channel/openai -run TestOpenaiHandlerRecordsClaudeUsageConversion -count=1`

Expected: PASS.

### Task 2: Record usage after streamed OpenAI-to-Claude conversion

**Files:**
- Modify: `relay/channel/openai/helper.go:34-58`
- Test: `relay/channel/openai/helper_test.go`

**Interfaces:**
- Consumes: `relayconvert.ConvertStreamResponse(c, info, types.RelayFormatClaude, &streamResponse) (*relayconvert.ResponseResult, error)`.
- Produces: final converted Claude usage in `relayInfo.UsageConversion` only when the stream response includes usage.

- [ ] **Step 1: Write the failing test**

Add a test calling `handleClaudeFormat` with a terminal OpenAI stream chunk that contains `prompt_tokens=39`, `completion_tokens=276`, `reasoning_tokens=195` and a Claude relay `RelayInfo`. Assert that `info.UsageConversion` contains the Claude format usage described in Task 1.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./relay/channel/openai -run TestHandleClaudeFormatRecordsUsageConversion -count=1`

Expected: failure because the stream converter result usage is not stored.

- [ ] **Step 3: Write minimal implementation**

After `relayconvert.ConvertStreamResponse` succeeds and before writing converted chunks, add:

```go
if result.Usage != nil {
    info.SetUsageConversion(result.Usage)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./relay/channel/openai -run TestHandleClaudeFormatRecordsUsageConversion -count=1`

Expected: PASS.

### Task 3: Run regression verification and stage only related files

**Files:**
- Modify: `relay/channel/openai/relay-openai.go`
- Modify: `relay/channel/openai/helper.go`
- Modify/Create: OpenAI channel test file(s) selected in Tasks 1 and 2

- [ ] **Step 1: Run package regression tests**

Run: `go test ./relay/channel/openai -count=1`

Expected: PASS.

- [ ] **Step 2: Run log-gating regression tests**

Run: `go test ./service -run 'TestAppendUsageConversion|TestStreamResponseOpenAI2ClaudeCapturesOnlyFinalUsage' -count=1`

Expected: PASS.

- [ ] **Step 3: Check formatting and diff scope**

Run: `gofmt -w relay/channel/openai/relay-openai.go relay/channel/openai/helper.go <test files>` and `git diff --check`.

Expected: no formatting or whitespace errors; `web/bun.lock` remains unrelated and unstaged.

- [ ] **Step 4: Stage only implementation and test files**

Run: `git add relay/channel/openai/relay-openai.go relay/channel/openai/helper.go <test files>`.

Expected: related changes staged; do not stage `web/bun.lock`.
