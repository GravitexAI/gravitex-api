package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// HandleStreamResponseData 必须把 server_tool_use.web_search_requests 记入
// info.ResponsesUsageInfo.BuiltInTools["web_search"] — 这是 PostTextConsumeQuota 计算
// Claude 内置工具调用 surcharge 的唯一来源（2026-11 合并第 8 批起由 ctx key 改为官方
// BuiltInTools 计数）。message_start 与 message_delta 两种事件都要捕获。
func TestHandleStreamResponseData_RecordsClaudeWebSearchRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		data          string
		wantWebSearch int
	}{
		{
			name:          "message_delta carries server_tool_use",
			data:          `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":50,"server_tool_use":{"web_search_requests":3}}}`,
			wantWebSearch: 3,
		},
		{
			name:          "message_start carries server_tool_use",
			data:          `{"type":"message_start","message":{"id":"msg_123","model":"claude-3-5-sonnet","usage":{"input_tokens":100,"output_tokens":1,"server_tool_use":{"web_search_requests":2}}}}`,
			wantWebSearch: 2,
		},
		{
			name:          "no server_tool_use records nothing",
			data:          `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":50}}`,
			wantWebSearch: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
			info.RelayFormat = types.RelayFormatClaude

			claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

			apiErr := HandleStreamResponseData(c, info, claudeInfo, tt.data)
			require.Nil(t, apiErr)

			got := 0
			if info.ResponsesUsageInfo != nil {
				if tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch]; tool != nil {
					got = tool.CallCount
				}
			}
			require.Equal(t, tt.wantWebSearch, got)
		})
	}
}
