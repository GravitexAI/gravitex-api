package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCaptureOpenAIStreamUsageKeepsUsageFromNonFinalChunk(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	captureOpenAIStreamUsage(info, `{"id":"chatcmpl-test","usage":{"prompt_tokens":1037,"completion_tokens":0,"total_tokens":1037}}`)

	require.Equal(t, map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(1037),
			"completion_tokens": float64(0),
			"total_tokens":      float64(1037),
		},
	}, info.UpstreamResponses)

	captureOpenAIStreamUsage(info, "[DONE]")
	require.NotNil(t, info.UpstreamResponses)
}

func TestOpenaiHandlerClaudeConversionRecordsClientUsageAndPreservesUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	info.EnsureClaudeConvertInfo()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-volc-test",
			"object":"chat.completion",
			"created":1786516441,
			"model":"seed-2-0-lite-260428",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{
				"prompt_tokens":39,
				"completion_tokens":276,
				"total_tokens":315,
				"completion_tokens_details":{"reasoning_tokens":195}
			}
		}`)),
	}

	usage, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 39, usage.PromptTokens)
	require.Equal(t, 276, usage.CompletionTokens)

	var clientResponse dto.ClaudeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &clientResponse))
	require.NotNil(t, clientResponse.Usage)

	var conversion dto.ClaudeUsage
	conversionJSON, err := common.Marshal(info.UsageConversion)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(conversionJSON, &conversion))
	require.Equal(t, clientResponse.Usage, &conversion)
	require.Nil(t, conversion.BillingUsage)

	upstreamUsageJSON, err := common.Marshal(info.UpstreamResponses["usage"])
	require.NoError(t, err)
	var upstreamUsage dto.Usage
	require.NoError(t, common.Unmarshal(upstreamUsageJSON, &upstreamUsage))
	require.Equal(t, 39, upstreamUsage.PromptTokens)
	require.Equal(t, 276, upstreamUsage.CompletionTokens)
	require.Equal(t, 195, upstreamUsage.CompletionTokenDetails.ReasoningTokens)
}

func TestHandleFinalResponseClaudeConversionRecordsClientUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	info.EnsureClaudeConvertInfo()
	lastStreamData := `{
		"id":"chatcmpl-volc-stream-test",
		"object":"chat.completion.chunk",
		"created":1786516441,
		"model":"seed-2-0-lite-260428",
		"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":39,"completion_tokens":276,"total_tokens":315}
	}`
	captureOpenAIStreamUsage(info, lastStreamData)

	HandleFinalResponse(c, info, lastStreamData, "chatcmpl-volc-stream-test", 1786516441,
		"seed-2-0-lite-260428", "", &dto.Usage{PromptTokens: 39, CompletionTokens: 276, TotalTokens: 315}, true)

	var conversion dto.ClaudeUsage
	conversionJSON, err := common.Marshal(info.UsageConversion)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(conversionJSON, &conversion))
	require.Equal(t, 39, conversion.InputTokens)
	require.Equal(t, 276, conversion.OutputTokens)
	require.Nil(t, conversion.BillingUsage)
	upstreamUsageJSON, err := common.Marshal(info.UpstreamResponses["usage"])
	require.NoError(t, err)
	var upstreamUsage dto.Usage
	require.NoError(t, common.Unmarshal(upstreamUsageJSON, &upstreamUsage))
	require.Equal(t, 39, upstreamUsage.PromptTokens)
	require.Equal(t, 276, upstreamUsage.CompletionTokens)
}

func TestOpenaiHandlerGeminiConversionRecordsClientUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-gemini-test","object":"chat.completion","created":1786516441,"model":"seed-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":39,"completion_tokens":276,"total_tokens":315}
		}`)),
	}

	_, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	var conversion dto.GeminiUsageMetadata
	conversionJSON, err := common.Marshal(info.UsageConversion)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(conversionJSON, &conversion))
	require.Equal(t, 39, conversion.PromptTokenCount)
	require.Equal(t, 276, conversion.CandidatesTokenCount)
	require.Equal(t, 315, conversion.TotalTokenCount)
}

func TestOaiChatToResponsesHandlerRecordsRawAndConvertedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl-responses-test","object":"chat.completion","created":1786516441,"model":"seed-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":39,"completion_tokens":276,"total_tokens":315}
		}`)),
	}

	_, apiErr := OaiChatToResponsesHandler(c, info, resp)
	require.Nil(t, apiErr)
	var conversion dto.Usage
	conversionJSON, err := common.Marshal(info.UsageConversion)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(conversionJSON, &conversion))
	require.Equal(t, 39, conversion.InputTokens)
	require.Equal(t, 276, conversion.OutputTokens)
	require.Equal(t, 315, conversion.TotalTokens)

	upstreamUsageJSON, err := common.Marshal(info.UpstreamResponses["usage"])
	require.NoError(t, err)
	var upstreamUsage dto.Usage
	require.NoError(t, common.Unmarshal(upstreamUsageJSON, &upstreamUsage))
	require.Equal(t, 39, upstreamUsage.PromptTokens)
	require.Equal(t, 276, upstreamUsage.CompletionTokens)
}
