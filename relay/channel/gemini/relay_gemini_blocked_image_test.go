package gemini

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// blockedImageResponse mirrors what Vertex returns when the image is generated
// and then withheld by an output-side safety filter: the candidate exists with
// no content parts, finishReason is IMAGE_SAFETY (or IMAGE_PROHIBITED_CONTENT),
// and usageMetadata still reports the IMAGE-modality tokens.
func blockedImageResponse(finishReason string) dto.GeminiChatResponse {
	reason := finishReason
	return dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content:      dto.GeminiChatContent{Role: "model"},
			FinishReason: &reason,
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     972,
			CandidatesTokenCount: 1120,
			ThoughtsTokenCount:   357,
			TotalTokenCount:      2449,
			CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 1120},
			},
		},
	}
}

func deliveredImageResponse() dto.GeminiChatResponse {
	reason := "STOP"
	return dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "aGk="}}},
			},
			FinishReason: &reason,
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     972,
			CandidatesTokenCount: 1120,
			ThoughtsTokenCount:   357,
			TotalTokenCount:      2449,
			CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "IMAGE", TokenCount: 1120},
			},
		},
	}
}

func runGeminiHandler(t *testing.T, path string, relayFormat types.RelayFormat, payload dto.GeminiChatResponse,
	handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError),
) (*gin.Context, *dto.Usage) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     relayFormat,
		OriginModelName: "gemini-3-pro-image",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-pro-image",
		},
	}

	body, err := common.Marshal(payload)
	require.NoError(t, err)

	usage, apiErr := handler(c, info, &http.Response{Body: io.NopCloser(bytes.NewReader(body))})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	return c, usage
}

// Google's own blocked-image response says "You will not be charged for blocked
// images", so an image the user never received must not reach billing — neither
// as image output tokens nor folded into the billable completion total.
func TestGeminiHandlersDoNotBillUndeliveredImageTokens(t *testing.T) {
	handlers := map[string]struct {
		path        string
		relayFormat types.RelayFormat
		fn          func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		"native generateContent":  {"/v1beta/models/gemini-3-pro-image:generateContent", types.RelayFormatGemini, GeminiTextGenerationHandler},
		"openai chat completions": {"/v1/chat/completions", types.RelayFormatOpenAI, GeminiChatHandler},
	}

	for _, finishReason := range []string{"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION", "NO_IMAGE"} {
		for name, h := range handlers {
			t.Run(name+"/"+finishReason, func(t *testing.T) {
				c, usage := runGeminiHandler(t, h.path, h.relayFormat, blockedImageResponse(finishReason), h.fn)

				assert.Equal(t, 0, usage.CompletionTokenDetails.ImageTokens, "blocked image must not be billed as image output")
				assert.Equal(t, 0, c.GetInt("gemini_image_output_tokens"), "blocked image must not reach the img_o billing variable")
				assert.Equal(t, 357, usage.CompletionTokens, "only thinking tokens remain billable")
				assert.Equal(t, 357, usage.CompletionTokenDetails.ReasoningTokens)
				assert.Equal(t, 972, usage.PromptTokens, "input is still charged by Google and stays billable")
			})
		}
	}
}

// The guard must not change billing for images that were actually returned.
func TestGeminiHandlersStillBillDeliveredImageTokens(t *testing.T) {
	handlers := map[string]struct {
		path        string
		relayFormat types.RelayFormat
		fn          func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		"native generateContent":  {"/v1beta/models/gemini-3-pro-image:generateContent", types.RelayFormatGemini, GeminiTextGenerationHandler},
		"openai chat completions": {"/v1/chat/completions", types.RelayFormatOpenAI, GeminiChatHandler},
	}

	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			c, usage := runGeminiHandler(t, h.path, h.relayFormat, deliveredImageResponse(), h.fn)

			assert.Equal(t, 1120, usage.CompletionTokenDetails.ImageTokens)
			assert.Equal(t, 1120, c.GetInt("gemini_image_output_tokens"))
			assert.Equal(t, 1477, usage.CompletionTokens, "image + thinking tokens")
		})
	}
}

// Streaming: the final chunk carries usageMetadata but no inline data, so the
// guard has to use image delivery accumulated over the whole stream — otherwise
// every successful streamed image would look blocked.
func TestGeminiStreamHandlerImageDeliveryDecidesImageBilling(t *testing.T) {
	const usageChunk = `data: {"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"%s"}],"usageMetadata":{"promptTokenCount":972,"candidatesTokenCount":1120,"thoughtsTokenCount":357,"totalTokenCount":2449,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":1120}]}}`
	const imageChunk = `data: {"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"aGk="}}]}}]}`

	cases := []struct {
		name           string
		body           string
		wantImage      int
		wantCompletion int
	}{
		{
			name:           "image withheld by output filter",
			body:           fmt.Sprintf(usageChunk, "IMAGE_SAFETY") + "\ndata: [DONE]\n",
			wantImage:      0,
			wantCompletion: 357,
		},
		{
			name:           "image delivered in an earlier chunk",
			body:           imageChunk + "\n" + fmt.Sprintf(usageChunk, "STOP") + "\ndata: [DONE]\n",
			wantImage:      1120,
			wantCompletion: 1477,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

			oldTimeout := constant.StreamingTimeout
			constant.StreamingTimeout = 300
			t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

			info := &relaycommon.RelayInfo{
				OriginModelName: "gemini-3-pro-image",
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3-pro-image"},
			}

			usage, apiErr := geminiStreamHandler(c, info, &http.Response{Body: io.NopCloser(bytes.NewReader([]byte(tc.body)))},
				func(_ string, _ *dto.GeminiChatResponse) bool { return true })
			require.Nil(t, apiErr)
			require.NotNil(t, usage)

			assert.Equal(t, tc.wantImage, usage.CompletionTokenDetails.ImageTokens)
			assert.Equal(t, tc.wantImage, c.GetInt("gemini_image_output_tokens"))
			assert.Equal(t, tc.wantCompletion, usage.CompletionTokens)
		})
	}
}

// A text-only answer from an image-capable model (the model-refusal path, which
// finishes with STOP) must be billed at the text output price, not the image
// output price.
func TestGeminiNativeHandlerBillsTextRefusalAsTextOutput(t *testing.T) {
	reason := "STOP"
	payload := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{
			Content: dto.GeminiChatContent{
				Role:  "model",
				Parts: []dto.GeminiPart{{Text: "I can't generate that image."}},
			},
			FinishReason: &reason,
		}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount:     120,
			CandidatesTokenCount: 48,
			TotalTokenCount:      168,
		},
	}

	c, usage := runGeminiHandler(t, "/v1beta/models/gemini-3-pro-image:generateContent", types.RelayFormatGemini, payload, GeminiTextGenerationHandler)

	assert.Equal(t, 0, c.GetInt("gemini_image_output_tokens"), "a text refusal is not image output")
	assert.Equal(t, 48, c.GetInt("gemini_text_output_tokens"))
	assert.Equal(t, 48, usage.CompletionTokens)
}

func TestGeminiFinishAuditPreservesUsageAndExcludesCandidateContent(t *testing.T) {
	payload := `{"candidates":[{"content":{"role":"model","parts":[{"text":"private generated content"}]},"finishReason":"IMAGE_PROHIBITED_CONTENT","finishMessage":"filtered by policy"}],"usageMetadata":{"promptTokenCount":58,"candidatesTokenCount":1120,"totalTokenCount":1178,"vendorField":"preserved"}}`
	var response dto.GeminiChatResponse
	require.NoError(t, common.Unmarshal([]byte(payload), &response))
	info := &relaycommon.RelayInfo{}

	recordGeminiFinishAudit(info, &response)

	metadata, ok := info.UpstreamResponses["usageMetadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(58), metadata["promptTokenCount"])
	assert.Equal(t, "preserved", metadata["vendorField"])
	assert.Equal(t, "IMAGE_PROHIBITED_CONTENT", metadata["candidates"].([]any)[0].(map[string]any)["finishReason"])
	assert.Equal(t, "filtered by policy", metadata["candidates"].([]any)[0].(map[string]any)["finishMessage"])
	assert.NotContains(t, common.MapToJsonStr(metadata), "private generated content")
	assert.Equal(t, 1120, response.UsageMetadata.CandidatesTokenCount, "audit metadata must not alter billing usage")
}

func TestGeminiFinishAuditOmitsStopWithoutFinishMessage(t *testing.T) {
	stop := "STOP"
	response := dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{{FinishReason: &stop}},
		UsageMetadata: dto.GeminiUsageMetadata{
			PromptTokenCount: 58,
			TotalTokenCount:  58,
		},
	}
	info := &relaycommon.RelayInfo{}

	recordGeminiFinishAudit(info, &response)

	assert.Empty(t, info.UpstreamResponses)
}

func TestGeminiPromptFeedbackAuditPreservesBlockedRequestMetadata(t *testing.T) {
	payload := `{"promptFeedback":{"blockReason":"IMAGE_SAFETY","blockReasonMessage":"The input image contains content that has been blocked by your current PersonGeneration setting. If you think this was an error, send feedback. Support code: 17301594."},"usageMetadata":{"trafficType":"ON_DEMAND"},"modelVersion":"gemini-3.1-flash-image","responseId":"K7nZaoztMPSN88EPpMTOYA"}`
	var response dto.GeminiChatResponse
	require.NoError(t, common.Unmarshal([]byte(payload), &response))
	info := &relaycommon.RelayInfo{}

	recordGeminiFinishAudit(info, &response)

	metadata, ok := info.UpstreamResponses["usageMetadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ON_DEMAND", metadata["trafficType"])
	feedback, ok := metadata["promptFeedback"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "IMAGE_SAFETY", feedback["blockReason"])
	assert.Contains(t, feedback["blockReasonMessage"], "PersonGeneration")
}
