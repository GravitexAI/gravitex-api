package claude

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func replayClaudeStreamEvents(t *testing.T, events []string) *ClaudeResponseInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	for _, event := range events {
		require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, event))
	}
	return claudeInfo
}

// message_start 与 message_delta 的 5m/1h 拆分不一致时以 message_delta 为准，
// message_start 的 1h 不能残留下来与 5m 叠加（曾导致同一批 token 按 5m+1h 各算一次）。
func TestStreamMessageDeltaCacheCreationSplitOverridesMessageStart(t *testing.T) {
	claudeInfo := replayClaudeStreamEvents(t, []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":35458,"cache_read_input_tokens":45495,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":35458},"output_tokens":1}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"cache_creation":{"ephemeral_5m_input_tokens":35458},"cache_creation_input_tokens":35458,"cache_read_input_tokens":45495,"input_tokens":2,"output_tokens":1042}}`,
	})

	require.Equal(t, 35458, claudeInfo.Usage.ClaudeCacheCreation5mTokens)
	require.Equal(t, 0, claudeInfo.Usage.ClaudeCacheCreation1hTokens)
	require.Equal(t, 35458, claudeInfo.Usage.PromptTokensDetails.CachedCreationTokens)
}

// message_delta 不带 cache_creation 子对象时，保留 message_start 的拆分。
func TestStreamMessageDeltaWithoutCacheCreationKeepsMessageStartSplit(t *testing.T) {
	claudeInfo := replayClaudeStreamEvents(t, []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":2,"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200},"output_tokens":1}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
	})

	require.Equal(t, 100, claudeInfo.Usage.ClaudeCacheCreation5mTokens)
	require.Equal(t, 200, claudeInfo.Usage.ClaudeCacheCreation1hTokens)
}
