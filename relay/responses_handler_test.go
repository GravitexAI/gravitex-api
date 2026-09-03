package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesHelperRejectsUnsupportedAnthropicResponsesEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)

	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-fable-5-1",
		RelayMode:       relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "claude-fable-5-1",
			Input: json.RawMessage(`[{"role":"user","content":"hi"}]`),
		},
	}

	apiErr := ResponsesHelper(c, info)

	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	require.Equal(t, types.ErrorCode("endpoint_not_found"), apiErr.GetErrorCode())
	require.False(t, types.IsRecordErrorLog(apiErr))

	openAIError := apiErr.ToOpenAIError()
	require.Equal(t, "not_found_error", openAIError.Type)
	require.Equal(t, "endpoint_not_found", openAIError.Code)
	require.Equal(t, "Requested endpoint /v1/responses is not supported.", openAIError.Message)
}

func TestOpenAIResponsesEndpointSupportKeepsOtherChannelsAvailable(t *testing.T) {
	require.False(t, common.SupportsOpenAIResponsesEndpoint(constant.ChannelTypeAnthropic))
	require.True(t, common.SupportsOpenAIResponsesEndpoint(constant.ChannelTypeOpenAI))
	require.True(t, common.SupportsOpenAIResponsesEndpoint(constant.ChannelTypeAdvancedCustom))
}
