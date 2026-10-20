package mockai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTestProfile 是一个每个区间都退化为单点的档位，因此 roll 的结果完全确定：
// TextInput=100 / CacheRead=40 / TextOutput=25 / Reasoning=5。
func mockTestProfile() dto.MockAIModelProfile {
	return dto.MockAIModelProfile{
		TextInput:     dto.MockAIRange{Min: 100, Max: 100},
		TextOutput:    dto.MockAIRange{Min: 25, Max: 25},
		CacheRead:     dto.MockAIRange{Min: 40, Max: 40},
		CacheCreation: dto.MockAIRange{Min: 0, Max: 0},
		Reasoning:     &dto.MockAIRange{Min: 5, Max: 5},
		LatencyMs:     dto.MockAIRange{Min: 2, Max: 2},
		FirstTokenMs:  dto.MockAIRange{Min: 1, Max: 1},
	}
}

func newMockRelayContext(t *testing.T, cfg *dto.MockAIConfig, stream bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// StreamScannerHandler 用 constant.StreamingTimeout 建 ticker，为 0 会 panic。
	oldStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 300
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		IsStream:        stream,
		OriginModelName: "mock-origin-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeMockAI,
			ApiType:              constant.APITypeMockAI,
			UpstreamModelName:    "mock-upstream-model",
			ChannelOtherSettings: dto.ChannelOtherSettings{MockAI: cfg},
		},
	}
	return c, recorder, info
}

func mockChatConfig() *dto.MockAIConfig {
	profile := mockTestProfile()
	return &dto.MockAIConfig{
		Endpoints: dto.MockAIEndpoints{ChatCompletions: true},
		Default:   profile,
		Models:    map[string]dto.MockAIModelProfile{"mock-origin-model": profile},
	}
}

// TestOpenAIChatEndToEndUsage 让合成的上游字节流过真实的 OpenaiHandler，
// 断言最终用量符合设计文档 §5.4 的 chat/completions 换算表。
func TestOpenAIChatEndToEndUsage(t *testing.T) {
	c, _, info := newMockRelayContext(t, mockChatConfig(), false)

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

	assert.Equal(t, 140, usage.PromptTokens)
	assert.Equal(t, 40, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 25, usage.CompletionTokens)
	assert.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 165, usage.TotalTokens)
}

// TestOpenAIChatStreamEndToEndUsage 覆盖流式：客户端没有传
// stream_options.include_usage（info.ShouldIncludeUsage 为 false），
// 合成的流里依然带 usage，计费侧拿到的数字与非流式一致。
func TestOpenAIChatStreamEndToEndUsage(t *testing.T) {
	c, _, info := newMockRelayContext(t, mockChatConfig(), true)
	require.False(t, info.ShouldIncludeUsage)

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

	assert.Equal(t, 140, usage.PromptTokens)
	assert.Equal(t, 40, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 25, usage.CompletionTokens)
	assert.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 165, usage.TotalTokens)
	// 合成的字节确实流过了真实的 StreamScannerHandler，而不是被 Mock 自己解析掉。
	assert.Positive(t, info.ReceivedResponseCount)
	// 必须断到 Done 而不是 IsNormalEnd()：后者把 EOF 也算正常结束
	// （relay/common/stream_status.go），删掉 data: [DONE] 收尾帧它照样绿。
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}

// TestCompletionsEndpointNotSupported 锁住 §2 的端点范围：RelayFormatOpenAI 同时
// 覆盖 /v1/chat/completions 与 /v1/completions，后者不在支持范围内，必须 404，
// 而不是拿 chat.completion 的形状去冒充 text_completion。
func TestCompletionsEndpointNotSupported(t *testing.T) {
	c, _, info := newMockRelayContext(t, mockChatConfig(), false)
	info.RelayMode = relayconstant.RelayModeCompletions
	info.RequestURLPath = "/v1/completions"

	a := &Adaptor{}
	raw, err := a.DoRequest(c, info, nil)
	require.NoError(t, err)
	require.Nil(t, raw, "/v1/completions must not synthesize a chat.completion response")

	_, apiErr := a.DoResponse(c, nil, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCode("endpoint_not_found"), apiErr.GetErrorCode())
}

// TestEndpointNotEnabled 锁住端点门禁：未勾选的端点返回 404 endpoint_not_found
// 且不记错误日志。
func TestEndpointNotEnabled(t *testing.T) {
	cfg := mockChatConfig()
	cfg.Endpoints = dto.MockAIEndpoints{ChatCompletions: false, Messages: true}

	c, _, info := newMockRelayContext(t, cfg, false)

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
