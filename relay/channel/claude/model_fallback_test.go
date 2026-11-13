package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 流式 message_start：上游换模型时既要记录回退，也要照旧把响应模型写回
// UpstreamModelName（两个行为都锁住，防止检测破坏既有写回）。
func TestClaudeStreamMessageStartRecordsModelFallback(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-requested"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	events := []string{
		`{"type":"message_start","message":{"id":"msg_test","model":"claude-swapped","usage":{"input_tokens":70,"output_tokens":1}}}`,
		`{"type":"message_delta","usage":{"output_tokens":26}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		require.Nil(t, HandleStreamResponseData(c, info, state, event))
	}

	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "claude-requested", info.UpstreamModelFallback.SentModel)
	assert.Equal(t, "claude-swapped", info.UpstreamModelFallback.ActualModel)
	assert.Equal(t, "claude-swapped", info.UpstreamModelName)
}

// 流式模型一致：不记回退，UpstreamModelName 照旧写回。
func TestClaudeStreamMessageStartSameModelNoFallback(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-requested"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	event := `{"type":"message_start","message":{"id":"msg_test","model":"claude-requested","usage":{"input_tokens":70,"output_tokens":1}}}`
	require.Nil(t, HandleStreamResponseData(c, info, state, event))

	assert.Nil(t, info.UpstreamModelFallback)
	assert.Equal(t, "claude-requested", info.UpstreamModelName)
}

// 非流式：响应模型与发出模型不一致时记录回退。
func TestClaudeNonStreamRecordsModelFallback(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-requested"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	body := `{"type":"message","id":"msg_test","model":"claude-swapped","content":[],"stop_reason":"end_turn","usage":{"input_tokens":70,"output_tokens":5}}`
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	require.Nil(t, HandleClaudeResponseData(c, info, state, resp, []byte(body)))

	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "claude-requested", info.UpstreamModelFallback.SentModel)
	assert.Equal(t, "claude-swapped", info.UpstreamModelFallback.ActualModel)
}

// 输出中途拒绝的官方回退：message_start 仍是发出模型（不会触发顶层模型比对），
// 换模型信号只在 fallback 内容块（from/to）里——检测必须靠这个块。
func TestClaudeStreamFallbackContentBlockRecordsMidStreamSwap(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-fable-5"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	events := []string{
		`{"type":"message_start","message":{"id":"msg_test","model":"claude-fable-5","usage":{"input_tokens":412,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"fallback","from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"cyber"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":263,"iterations":[{"type":"message","model":"claude-fable-5","input_tokens":535,"output_tokens":0},{"type":"fallback_message","model":"claude-opus-4-8","input_tokens":412,"output_tokens":263}]}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		require.Nil(t, HandleStreamResponseData(c, info, state, event))
	}

	require.NotNil(t, info.UpstreamModelFallback)
	assert.Equal(t, "claude-fable-5", info.UpstreamModelFallback.SentModel)
	assert.Equal(t, "claude-opus-4-8", info.UpstreamModelFallback.ActualModel)
	assert.Equal(t, "cyber", info.ClaudeFallbackTriggerCategory)
	require.Len(t, info.ClaudeUsageIterations, 2)
	assert.Equal(t, "claude-fable-5", info.ClaudeUsageIterations[0].Model)
	assert.Equal(t, 535, info.ClaudeUsageIterations[0].InputTokens)
	assert.Equal(t, "claude-opus-4-8", info.ClaudeUsageIterations[1].Model)
}

// 非流式 fallback 内容块：顶层模型比对已覆盖换模型检测，这里主要锁
// trigger.category 的采集（计费要用）。
func TestClaudeNonStreamFallbackContentBlockCapturesTrigger(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-fable-5"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	body := `{"type":"message","id":"msg_test","model":"claude-opus-4-8","content":[{"type":"fallback","from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"trigger":{"type":"refusal","category":"bio"}}],"stop_reason":"end_turn","usage":{"input_tokens":412,"output_tokens":264,"iterations":[{"type":"message","model":"claude-fable-5","input_tokens":535,"output_tokens":0},{"type":"fallback_message","model":"claude-opus-4-8","input_tokens":412,"output_tokens":264}]}}`
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
	require.Nil(t, HandleClaudeResponseData(c, info, state, resp, []byte(body)))

	assert.Equal(t, "bio", info.ClaudeFallbackTriggerCategory)
	require.Len(t, info.ClaudeUsageIterations, 2)
	assert.Equal(t, 535, info.ClaudeUsageIterations[0].InputTokens)
}

// message_start 的 usage 里带 iterations 时也要采集（快照语义：后到的完整
// 数组覆盖先到的部分数组）。
func TestClaudeStreamMessageStartUsageCarriesIterations(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-fable-5"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	event := `{"type":"message_start","message":{"id":"msg_test","model":"claude-opus-4-8","usage":{"input_tokens":412,"output_tokens":1,"iterations":[{"type":"message","model":"claude-fable-5","input_tokens":535,"output_tokens":0},{"type":"fallback_message","model":"claude-opus-4-8","input_tokens":412,"output_tokens":1}]}}}`
	require.Nil(t, HandleStreamResponseData(c, info, state, event))

	require.Len(t, info.ClaudeUsageIterations, 2)
	assert.Equal(t, "message", info.ClaudeUsageIterations[0].Type)
	assert.Equal(t, "fallback_message", info.ClaudeUsageIterations[1].Type)
}
