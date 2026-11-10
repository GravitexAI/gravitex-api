package claude

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游 usage.speed=fast 时必须记为 fast 档位，否则快速模式请求按标准价少收。
func TestClaudeUsageSpeedSetsUpstreamServiceTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		stream   bool
		events   []string
		wantTier string
	}{
		{
			name:   "stream fast in message_start",
			stream: true,
			events: []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":1,"speed":"fast"}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
			},
			wantTier: "fast",
		},
		{
			name:   "stream fast in message_delta",
			stream: true,
			events: []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":1}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42,"speed":"fast"}}`,
			},
			wantTier: "fast",
		},
		{
			name:   "stream standard",
			stream: true,
			events: []string{
				`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":10,"output_tokens":1,"speed":"standard"}}}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
			},
			wantTier: "",
		},
		{
			name:     "non-stream fast",
			events:   []string{`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":42,"speed":"fast"}}`},
			wantTier: "fast",
		},
		{
			name:     "non-stream without speed",
			events:   []string{`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":42}}`},
			wantTier: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
			claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			for _, event := range tt.events {
				if tt.stream {
					require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, event))
				} else {
					require.Nil(t, HandleClaudeResponseData(c, info, claudeInfo, nil, []byte(event)))
				}
			}
			assert.Equal(t, tt.wantTier, info.UpstreamServiceTier)
		})
	}
}
