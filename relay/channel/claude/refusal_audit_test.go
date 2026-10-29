package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeResponsesStreamRefusalAudit(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
		DisablePing: true,
	}
	stream := "data: " + `{"type":"message_start","message":{"id":"msg_test","model":"claude-test","role":"assistant","content":[],"usage":{"input_tokens":70,"output_tokens":0}}}` + "\n\n" +
		"data: " + `{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"Original wording"}},"usage":{"output_tokens":0}}` + "\n\n" +
		"data: " + `{"type":"message_stop"}` + "\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	usage, apiErr := ClaudeResponsesStreamHandler(c, resp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 70, usage.PromptTokens)
	audit, err := common.Marshal(info.ClaudeRefusalAudit)
	require.NoError(t, err)
	assert.JSONEq(t, `{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"Original wording"}}`, string(audit))
}

func TestClaudeRefusalAuditPreservesResponseAndUsage(t *testing.T) {
	for _, tt := range []struct {
		name, stop, details string
	}{
		{"refusal", "refusal", `,"stop_details":{"type":"refusal","category":"cyber","explanation":"Original wording","extra":{"code":9007199254740993}}`},
		{"null fields", "refusal", `,"stop_details":{"type":"refusal","category":null,"explanation":null}`},
		{"null details", "refusal", `,"stop_details":null`},
		{"missing details", "refusal", ""},
		{"ordinary response", "end_turn", `,"stop_details":null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}
			state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			body := `{"type":"message","id":"msg_test","content":[],"stop_reason":"` + tt.stop + `"` + tt.details + `,"usage":{"input_tokens":70,"output_tokens":0,"cache_read_input_tokens":10}}`
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}}
			require.Nil(t, HandleClaudeResponseData(c, info, state, resp, []byte(body)))
			assert.Equal(t, body, w.Body.String())
			assert.Equal(t, 70, state.Usage.PromptTokens)
			assert.Zero(t, state.Usage.CompletionTokens)
			assert.Equal(t, 10, state.Usage.PromptTokensDetails.CachedTokens)
			if tt.stop != "refusal" {
				assert.Nil(t, info.ClaudeRefusalAudit)
				return
			}
			audit, err := common.Marshal(info.ClaudeRefusalAudit)
			require.NoError(t, err)
			assert.JSONEq(t, `{"stop_reason":"refusal"`+tt.details+`}`, string(audit))
		})
	}
}

func TestClaudeStreamRefusalAuditSurvivesLaterUsageAndRetry(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	events := []string{
		`{"type":"message_start","message":{"id":"msg_test","model":"claude-test","usage":{"input_tokens":70,"output_tokens":1}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":null}},"usage":{"output_tokens":25}}`,
		`{"type":"message_delta","usage":{"output_tokens":26}}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		require.Nil(t, HandleStreamResponseData(c, info, state, event))
	}
	audit, err := common.Marshal(info.ClaudeRefusalAudit)
	require.NoError(t, err)
	assert.JSONEq(t, `{"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":null}}`, string(audit))
	assert.Equal(t, 70, state.Usage.PromptTokens)
	assert.Equal(t, 26, state.Usage.CompletionTokens)
	assert.True(t, strings.Contains(w.Body.String(), `"stop_details":{"type":"refusal","category":"cyber","explanation":null}`))
	info.InitChannelMeta(c)
	assert.Nil(t, info.ClaudeRefusalAudit, "next channel attempt must not inherit refusal evidence")
}
