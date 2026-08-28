package claude

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertClaudeRequestPreservesBodyFieldsForNativeMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAnthropic,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AnthropicBetaTarget: "bedrock-converse",
			},
		},
	}
	request := &dto.ClaudeRequest{
		Model:             "claude-opus-5",
		Messages:          []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
		OutputConfig:      json.RawMessage(`{"effort":"gdsnglskg","custom":"keep"}`),
		ContextManagement: json.RawMessage(`{"edits":[{"type":"keep"}]}`),
	}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(c, info, request)
	require.NoError(t, err)
	got := converted.(*dto.ClaudeRequest)
	require.JSONEq(t, `{"effort":"gdsnglskg","custom":"keep"}`, string(got.OutputConfig))
	require.JSONEq(t, `{"edits":[{"type":"keep"}]}`, string(got.ContextManagement))
}
