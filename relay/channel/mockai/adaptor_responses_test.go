package mockai

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockResponsesConfig() *dto.MockAIConfig {
	cfg := mockChatConfig()
	cfg.Endpoints = dto.MockAIEndpoints{Responses: true}
	return cfg
}

// newResponsesRelayContext 在 chat 的测试上下文基础上切到 /v1/responses 格式。
func newResponsesRelayContext(t *testing.T, cfg *dto.MockAIConfig, stream bool) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	c, _, info := newMockRelayContext(t, cfg, stream)
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	return c, info
}

func assertResponsesUsage(t *testing.T, usage *dto.Usage) {
	t.Helper()
	assert.Equal(t, 140, usage.PromptTokens)
	assert.Equal(t, 40, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 25, usage.CompletionTokens)
	// 真实 OaiResponses*Handler 只从 completion_tokens_details 读 reasoning，而官方
	// /v1/responses 下发的是 output_tokens_details，所以真实上游流过它同样得到 0。
	// Mock 按设计 §5.4 与「逐字节同构」下发官方字段，这里如实锁住该既存行为；
	// output_tokens_details.reasoning_tokens == 5 由 TestResponsesNonStreamBody 在字节层锁住。
	assert.Equal(t, 0, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 165, usage.TotalTokens)
}

// TestResponsesEndToEndUsage 让合成的上游字节流过真实的 OaiResponsesHandler，
// 断言最终用量符合设计文档 §5.4 的 responses 换算表。
func TestResponsesEndToEndUsage(t *testing.T) {
	c, info := newResponsesRelayContext(t, mockResponsesConfig(), false)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	resp, ok := raw.(*http.Response)
	require.True(t, ok, "DoRequest must return *http.Response")
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	rawUsage, apiErr := a.DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	usage, ok := rawUsage.(*dto.Usage)
	require.True(t, ok)
	assertResponsesUsage(t, usage)
	assert.NotEmpty(t, info.UpstreamResponseId)
}

// TestResponsesStreamEndToEndUsage 覆盖流式：usage 在 response.completed 里，
// 且合成字节确实流过了真实的 StreamScannerHandler。
func TestResponsesStreamEndToEndUsage(t *testing.T) {
	c, info := newResponsesRelayContext(t, mockResponsesConfig(), true)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	resp, ok := raw.(*http.Response)
	require.True(t, ok, "DoRequest must return *http.Response")
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	rawUsage, apiErr := a.DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	usage, ok := rawUsage.(*dto.Usage)
	require.True(t, ok)
	assertResponsesUsage(t, usage)
	assert.Positive(t, info.ReceivedResponseCount)
	assert.NotEmpty(t, info.UpstreamResponseId)
}

// TestResponsesStreamEventOrder 锁住事件序列：created 在最前，delta 居中，completed 在最后，
// 且 completed 之后没有任何事件；delta 拼起来就是完整答案。
func TestResponsesStreamEventOrder(t *testing.T) {
	answer := "你好, mock"
	frames := buildResponsesFrames(mockRoll{TextInput: 100, CacheRead: 40, TextOutput: 25, Reasoning: 5}, answer, "m", true)
	require.NotEmpty(t, frames)

	var types_ []string
	var text strings.Builder
	for _, f := range frames {
		require.True(t, bytes.HasPrefix(f, []byte("event: ")), "frame must start with an event line: %q", f)
		lines := strings.SplitN(strings.TrimSuffix(string(f), "\n\n"), "\n", 2)
		require.Len(t, lines, 2)
		require.True(t, strings.HasPrefix(lines[1], "data: "))
		var ev dto.ResponsesStreamResponse
		require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(lines[1], "data: "), &ev))
		assert.Equal(t, "event: "+ev.Type, lines[0], "event line must match payload type")
		types_ = append(types_, ev.Type)
		if ev.Type == "response.completed" {
			// 流式的 total_tokens / reasoning 经处理器后会被重算或丢弃，只能在字节层锁。
			var completed struct {
				Response struct {
					Usage map[string]any `json:"usage"`
				} `json:"response"`
			}
			require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(lines[1], "data: "), &completed))
			u := completed.Response.Usage
			require.NotNil(t, u)
			assert.EqualValues(t, 140, u["input_tokens"])
			assert.EqualValues(t, 25, u["output_tokens"])
			assert.EqualValues(t, 165, u["total_tokens"])
			assert.EqualValues(t, 40, u["input_tokens_details"].(map[string]any)["cached_tokens"])
			assert.EqualValues(t, 5, u["output_tokens_details"].(map[string]any)["reasoning_tokens"])
		}
		if ev.Type == "response.output_text.delta" {
			text.WriteString(ev.Delta)
		}
	}

	require.Equal(t, "response.created", types_[0])
	assert.Equal(t, "response.completed", types_[len(types_)-1])
	assert.Equal(t, 1, countOf(types_, "response.completed"))
	firstDelta, lastDelta := -1, -1
	for i, ty := range types_ {
		if ty == "response.output_text.delta" {
			if firstDelta < 0 {
				firstDelta = i
			}
			lastDelta = i
		}
	}
	require.GreaterOrEqual(t, firstDelta, 1, "deltas must come after response.created")
	assert.Less(t, lastDelta, len(types_)-1, "deltas must come before response.completed")
	assert.Equal(t, answer, text.String())
}

func countOf(items []string, want string) int {
	n := 0
	for _, s := range items {
		if s == want {
			n++
		}
	}
	return n
}

// TestResponsesNonStreamBody 锁住非流式 body 的形状与 §5.4 换算表（直接看字节，
// 不经过处理器，避免处理器的宽松解析掩盖字段名写错）。
func TestResponsesNonStreamBody(t *testing.T) {
	frames := buildResponsesFrames(mockRoll{TextInput: 100, CacheRead: 40, TextOutput: 25, Reasoning: 5}, "hi", "m", false)
	require.Len(t, frames, 1)
	body, err := io.ReadAll(bytes.NewReader(frames[0]))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(body, &got))
	assert.Equal(t, "response", got["object"])
	assert.Equal(t, "completed", got["status"])
	assert.Equal(t, "m", got["model"])
	usage, ok := got["usage"].(map[string]any)
	require.True(t, ok)
	assert.EqualValues(t, 140, usage["input_tokens"])
	assert.EqualValues(t, 25, usage["output_tokens"])
	assert.EqualValues(t, 165, usage["total_tokens"])
	assert.EqualValues(t, 40, usage["input_tokens_details"].(map[string]any)["cached_tokens"])
	assert.EqualValues(t, 5, usage["output_tokens_details"].(map[string]any)["reasoning_tokens"])
}

func TestResponsesEndpointNotEnabled(t *testing.T) {
	cfg := mockResponsesConfig()
	cfg.Endpoints = dto.MockAIEndpoints{ChatCompletions: true}
	c, info := newResponsesRelayContext(t, cfg, false)

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	require.Nil(t, raw)
	_, apiErr := a.DoResponse(c, nil, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}
