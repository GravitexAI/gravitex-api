package mockai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClaudeProfile 是一个每个区间都退化为单点的档位，因此 roll 的结果完全确定：
// TextInput=100 / CacheRead=40 / CacheCreation=60（1h 占比 0.5 → 30/30）/ TextOutput=25。
// 刻意不配 Reasoning：真实 Anthropic /v1/messages 不单独下发思考 token（已含在
// output_tokens 里），设计 §5.4 的 messages 换算表也没有这一行。
func mockClaudeProfile() dto.MockAIModelProfile {
	return dto.MockAIModelProfile{
		TextInput:            dto.MockAIRange{Min: 100, Max: 100},
		TextOutput:           dto.MockAIRange{Min: 25, Max: 25},
		CacheRead:            dto.MockAIRange{Min: 40, Max: 40},
		CacheCreation:        dto.MockAIRange{Min: 60, Max: 60},
		CacheCreation1hRatio: 0.5,
		LatencyMs:            dto.MockAIRange{Min: 2, Max: 2},
		FirstTokenMs:         dto.MockAIRange{Min: 1, Max: 1},
	}
}

func mockClaudeConfig() *dto.MockAIConfig {
	profile := mockClaudeProfile()
	return &dto.MockAIConfig{
		Endpoints: dto.MockAIEndpoints{Messages: true},
		Default:   profile,
		Models:    map[string]dto.MockAIModelProfile{"mock-origin-model": profile},
	}
}

// newClaudeRelayContext 在 chat 的测试上下文基础上切到 /v1/messages 格式。
func newClaudeRelayContext(t *testing.T, cfg *dto.MockAIConfig, stream bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	c, recorder, info := newMockRelayContext(t, cfg, stream)
	info.RelayFormat = types.RelayFormatClaude
	info.RequestURLPath = "/v1/messages"
	info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{
		LastMessagesType: relaycommon.LastMessageTypeNone,
	}
	return c, recorder, info
}

// assertClaudeUsage 锁住设计 §5.4 的 messages 换算表。与 OpenAI 侧最关键的区别是
// PromptTokens 为 100（纯文本输入）而不是 140：Claude 的 input_tokens 与两个缓存
// 字段是并列关系，缓存不计入其中。
func assertClaudeUsage(t *testing.T, usage *dto.Usage) {
	t.Helper()
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 40, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 60, usage.PromptTokensDetails.CachedCreationTokens)
	assert.Equal(t, 30, usage.ClaudeCacheCreation5mTokens)
	assert.Equal(t, 30, usage.ClaudeCacheCreation1hTokens)
	assert.Equal(t, 25, usage.CompletionTokens)
}

// TestClaudeEndToEndUsage 让合成的上游字节流过真实的 ClaudeHandler，
// 断言最终用量符合设计文档 §5.4 的 messages 换算表。
func TestClaudeEndToEndUsage(t *testing.T) {
	c, _, info := newClaudeRelayContext(t, mockClaudeConfig(), false)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	resp, ok := raw.(*http.Response)
	require.True(t, ok, "DoRequest must return *http.Response")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	rawUsage, apiErr := a.DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	usage, ok := rawUsage.(*dto.Usage)
	require.True(t, ok)

	assertClaudeUsage(t, usage)
	assert.Equal(t, 125, usage.TotalTokens)
	assert.NotEmpty(t, info.UpstreamResponseId)
}

// TestClaudeStreamEndToEndUsage 覆盖流式：usage 由真实的 FormatClaudeResponseInfo
// 跨 message_start / message_delta 聚合，计费侧拿到的数字与非流式一致。
func TestClaudeStreamEndToEndUsage(t *testing.T) {
	c, _, info := newClaudeRelayContext(t, mockClaudeConfig(), true)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	resp, ok := raw.(*http.Response)
	require.True(t, ok, "DoRequest must return *http.Response")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	rawUsage, apiErr := a.DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	usage, ok := rawUsage.(*dto.Usage)
	require.True(t, ok)

	assertClaudeUsage(t, usage)
	assert.Equal(t, 125, usage.TotalTokens)
	// 合成的字节确实流过了真实的 StreamScannerHandler，而不是被 Mock 自己解析掉。
	assert.Positive(t, info.ReceivedResponseCount)
	assert.NotEmpty(t, info.UpstreamResponseId)
}

// claudeStreamEvent 是测试侧对一条合成 SSE 帧的解析结果。
type claudeStreamEvent struct {
	Type  string
	Usage map[string]any
}

// parseClaudeFrames 把合成帧拆成事件序列，并顺带校验 Claude 原生 SSE 的
// `event: <type>` + `data: {...}` 两行结构，以及 event 行与载荷 type 一致。
func parseClaudeFrames(t *testing.T, frames [][]byte) []claudeStreamEvent {
	t.Helper()
	events := make([]claudeStreamEvent, 0, len(frames))
	for _, f := range frames {
		raw := string(f)
		require.True(t, strings.HasSuffix(raw, "\n\n"), "frame must end with a blank line: %q", raw)
		lines := strings.SplitN(strings.TrimSuffix(raw, "\n\n"), "\n", 2)
		require.Len(t, lines, 2, "frame must carry an event line and a data line: %q", raw)
		require.True(t, strings.HasPrefix(lines[1], "data: "), "second line must be the data line: %q", raw)

		var payload map[string]any
		require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(lines[1], "data: "), &payload))
		evType, _ := payload["type"].(string)
		require.NotEmpty(t, evType)
		assert.Equal(t, "event: "+evType, lines[0], "event line must match payload type")

		ev := claudeStreamEvent{Type: evType}
		if u, ok := payload["usage"].(map[string]any); ok {
			ev.Usage = u
		}
		if msg, ok := payload["message"].(map[string]any); ok {
			if u, ok := msg["usage"].(map[string]any); ok {
				ev.Usage = u
			}
		}
		events = append(events, ev)
	}
	return events
}

// TestClaudeStreamUsageConsistency 断言 message_start 与 message_delta 两处的
// 缓存数字自洽（设计 §5.4 明确要求），否则 FormatClaudeResponseInfo 的整体覆盖
// 语义会让两处不一致直接变成错误的扣费金额。
func TestClaudeStreamUsageConsistency(t *testing.T) {
	r := mockRoll{TextInput: 100, CacheRead: 40, CacheCreation: 60, CacheCreation5m: 30, CacheCreation1h: 30, TextOutput: 25}
	events := parseClaudeFrames(t, buildClaudeMessagesFrames(r, "你好, mock", "m", true))

	var start, delta map[string]any
	for _, ev := range events {
		switch ev.Type {
		case "message_start":
			start = ev.Usage
		case "message_delta":
			delta = ev.Usage
		}
	}
	require.NotNil(t, start, "message_start must carry usage")
	require.NotNil(t, delta, "message_delta must carry usage")

	startCache, ok := start["cache_creation"].(map[string]any)
	require.True(t, ok, "message_start usage must carry a cache_creation object")
	deltaCache, ok := delta["cache_creation"].(map[string]any)
	require.True(t, ok, "message_delta usage must carry a cache_creation object")
	assert.Equal(t, startCache, deltaCache, "cache_creation must be identical in message_start and message_delta")

	assert.EqualValues(t, 100, start["input_tokens"])
	assert.EqualValues(t, 40, start["cache_read_input_tokens"])
	assert.EqualValues(t, 60, start["cache_creation_input_tokens"])
	assert.EqualValues(t, 30, startCache["ephemeral_5m_input_tokens"])
	assert.EqualValues(t, 30, startCache["ephemeral_1h_input_tokens"])
	assert.EqualValues(t, 25, delta["output_tokens"])
}

// TestClaudeStreamEventOrder 锁住事件序列：message_start 在最前，content_block_start
// 先于 delta，content_block_stop 先于 message_delta，message_stop 收尾。
func TestClaudeStreamEventOrder(t *testing.T) {
	r := mockRoll{TextInput: 100, CacheRead: 40, CacheCreation: 60, CacheCreation5m: 30, CacheCreation1h: 30, TextOutput: 25}
	events := parseClaudeFrames(t, buildClaudeMessagesFrames(r, "hi, mock", "m", true))

	order := make([]string, 0, len(events))
	for _, ev := range events {
		order = append(order, ev.Type)
	}
	require.GreaterOrEqual(t, len(order), 6)

	indexOf := func(want string) int {
		for i, ty := range order {
			if ty == want {
				return i
			}
		}
		return -1
	}
	require.Equal(t, "message_start", order[0])
	assert.Equal(t, "message_stop", order[len(order)-1])
	assert.Equal(t, 1, countOf(order, "message_start"))
	assert.Equal(t, 1, countOf(order, "message_delta"))
	assert.Equal(t, 1, countOf(order, "message_stop"))

	blockStart := indexOf("content_block_start")
	blockStop := indexOf("content_block_stop")
	messageDelta := indexOf("message_delta")
	require.Positive(t, blockStart, "content_block_start must follow message_start")
	require.Greater(t, blockStop, blockStart)
	require.Greater(t, messageDelta, blockStop, "message_delta must follow content_block_stop")

	firstDelta, lastDelta := -1, -1
	for i, ty := range order {
		if ty == "content_block_delta" {
			if firstDelta < 0 {
				firstDelta = i
			}
			lastDelta = i
		}
	}
	require.Greater(t, firstDelta, blockStart, "deltas must follow content_block_start")
	assert.Less(t, lastDelta, blockStop, "deltas must precede content_block_stop")
}

// TestClaudeNonStreamBody 锁住非流式 body 的形状与 §5.4 换算表（直接看字节，
// 不经过处理器，避免处理器的宽松解析掩盖字段名写错）。
func TestClaudeNonStreamBody(t *testing.T) {
	r := mockRoll{TextInput: 100, CacheRead: 40, CacheCreation: 60, CacheCreation5m: 30, CacheCreation1h: 30, TextOutput: 25}
	frames := buildClaudeMessagesFrames(r, "hi", "m", false)
	require.Len(t, frames, 1)

	var got map[string]any
	require.NoError(t, common.Unmarshal(frames[0], &got))
	assert.Equal(t, "message", got["type"])
	assert.Equal(t, "assistant", got["role"])
	assert.Equal(t, "m", got["model"])
	assert.Equal(t, "end_turn", got["stop_reason"])

	usage, ok := got["usage"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 100, usage["input_tokens"])
	assert.EqualValues(t, 40, usage["cache_read_input_tokens"])
	assert.EqualValues(t, 60, usage["cache_creation_input_tokens"])
	assert.EqualValues(t, 25, usage["output_tokens"])
	cacheCreation, ok := usage["cache_creation"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 30, cacheCreation["ephemeral_5m_input_tokens"])
	assert.EqualValues(t, 30, cacheCreation["ephemeral_1h_input_tokens"])
}

// TestClaudeCacheCreationSplitSumsExact 覆盖会产生舍入的 1h 占比：5m + 1h 必须
// 严格等于 cache_creation_input_tokens，否则缓存创建部分会少算或多算一个 token。
func TestClaudeCacheCreationSplitSumsExact(t *testing.T) {
	cfg := mockClaudeConfig()
	profile := mockClaudeProfile()
	profile.CacheCreation = dto.MockAIRange{Min: 7, Max: 7}
	profile.CacheCreation1hRatio = 0.333
	cfg.Default = profile
	cfg.Models["mock-origin-model"] = profile

	for _, stream := range []bool{false, true} {
		c, _, info := newClaudeRelayContext(t, cfg, stream)
		a := &Adaptor{}
		raw, err := a.DoRequest(c, info, nil)
		require.NoError(t, err)
		resp, ok := raw.(*http.Response)
		require.True(t, ok)

		rawUsage, apiErr := a.DoResponse(c, resp, info)
		require.Nil(t, apiErr)
		usage, ok := rawUsage.(*dto.Usage)
		require.True(t, ok)

		assert.Equal(t, 7, usage.PromptTokensDetails.CachedCreationTokens, "stream=%v", stream)
		// round(7 * 0.333) = 2，剩下的全部归 5m。
		assert.Equal(t, 2, usage.ClaudeCacheCreation1hTokens, "stream=%v", stream)
		assert.Equal(t, 5, usage.ClaudeCacheCreation5mTokens, "stream=%v", stream)
		assert.Equal(t, usage.PromptTokensDetails.CachedCreationTokens,
			usage.ClaudeCacheCreation5mTokens+usage.ClaudeCacheCreation1hTokens,
			"5m + 1h must sum exactly to cache_creation_input_tokens, stream=%v", stream)
	}
}

// TestClaudeEndpointNotEnabled 锁住端点门禁：未勾选 messages 时返回 404。
func TestClaudeEndpointNotEnabled(t *testing.T) {
	cfg := mockClaudeConfig()
	cfg.Endpoints = dto.MockAIEndpoints{ChatCompletions: true}
	c, _, info := newClaudeRelayContext(t, cfg, false)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	require.Nil(t, raw, "disabled endpoint must not synthesize an upstream response")

	_, apiErr := a.DoResponse(c, nil, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCode("endpoint_not_found"), apiErr.GetErrorCode())
	assert.False(t, types.IsRecordErrorLog(apiErr))
}
