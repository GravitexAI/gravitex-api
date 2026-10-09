package lyria

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildVertexInteractionsURLUsesProjectAndGlobalLocation(t *testing.T) {
	key := `{"project_id":"demo-project"}`

	url, err := buildVertexInteractionsURL("https://aiplatform.googleapis.com", key)

	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", url)
}

func TestBuildVertexInteractionsURLDoesNotDuplicateInteractionsPath(t *testing.T) {
	key := `{"project_id":"demo-project"}`

	url, err := buildVertexInteractionsURL("https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", key)

	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", url)
}

func TestBuildVertexGenerateContentURLUsesPublisherModelPath(t *testing.T) {
	url, err := buildVertexGenerateContentURL("https://aiplatform.googleapis.com", `{"project_id":"demo-project"}`, "gemini-nano-banana-2.1")
	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/publishers/google/models/gemini-nano-banana-2.1:generateContent", url)
}

func TestLyriaEndpointKindDetectsVertexByEndpoint(t *testing.T) {
	require.True(t, isVertexInteractionsEndpoint("https://aiplatform.googleapis.com"))
	require.True(t, isVertexInteractionsEndpoint("https://us-central1-aiplatform.googleapis.com"))
	require.False(t, isVertexInteractionsEndpoint("https://generativelanguage.googleapis.com"))
}

func TestVertexLyriaInteractionRequiresChannelEndpointAndModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		OriginModelName:    "lyria-3-pro-preview",
		RequestURLPath:     "/v1/video/generations",
		NativeInteractions: true,
	}

	require.True(t, isVertexLyriaInteraction(info))

	info.NativeInteractions = false
	require.False(t, isVertexLyriaInteraction(info))

	info.NativeInteractions = true
	info.OriginModelName = "lyria-3-experimental"
	require.False(t, isVertexLyriaInteraction(info))
}

func TestVertexNativeInteractionModelAllowlist(t *testing.T) {
	for _, model := range []string{
		"gemini-2.5-flash-image",
		"gemini-3-flash-preview",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image-preview",
		"gemini-3.5-flash",
		"gemini-3.1-flash-image",
		"gemini-3-pro-image",
		"gemini-3.1-flash-lite-image",
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-2.5-flash-lite",
		"gemini-nano-banana-2.1",
	} {
		require.True(t, IsNativeInteractionModel(model), model)
	}
	for _, model := range []string{
		"gemini-3.1-pro-preview",
		"gemini-3.1-flash-lite",
		"gemini-omni-flash-preview",
		"gemini-3.6-flash",
		"gemini-3.7-flash",
		"gemini-3.8-flash",
		"gemini-3.5-flash-lite",
		"lyria-3-pro-preview",
		"gemini-unknown",
		"veo-3.1-generate-001",
	} {
		require.False(t, IsNativeInteractionModel(model), model)
	}
}

func TestBuildRequestBodyConvertsSupportedVertexInteractionModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.KeyLyriaRawRequestBody, []byte(`{"model":"gemini-nano-banana-2.1","input":"draw a cat"}`))
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "gemini-nano-banana-2.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVertexAi,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         `{"project_id":"demo"}`,
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(forwarded, &decoded))
	require.Equal(t, []any{map[string]any{
		"role":  "user",
		"parts": []any{map[string]any{"text": "draw a cat"}},
	}}, decoded["contents"])
	config := decoded["generationConfig"].(map[string]any)
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
}

func TestGenerateContentImageResponseFormatMapsImageConfig(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-nano-banana-2.1",
		"input":"draw a technology infographic",
		"response_format":{"type":"image","mime_type":"image/jpeg","aspect_ratio":"16:9","image_size":"2K"}
	}`)
	converted, err := convertNativeInteractionToGenerateContent(nil, raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	config := request["generationConfig"].(map[string]any)
	require.Equal(t, []any{"IMAGE"}, config["responseModalities"])
	require.Equal(t, map[string]any{
		"aspectRatio": "16:9",
		"imageSize":   "2K",
	}, config["imageConfig"])
}

func TestGenerateContentTaskActionIsNotLegacyVideoAction(t *testing.T) {
	require.True(t, IsNativeInteractionGenerateContentModel("gemini-nano-banana-2.1"))
	require.Equal(t, constant.TaskActionGenerateContent, nativeInteractionTaskAction("gemini-nano-banana-2.1"))
	require.NotEqual(t, constant.TaskActionTextToVideo, constant.NormalizeTaskAction(nativeInteractionTaskAction("gemini-nano-banana-2.1")))
	require.Equal(t, "song", nativeInteractionTaskAction(ProModelName))
}

func TestConvertNativeInteractionToGenerateContentMapsAdvancedParameters(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-2.5-flash",
		"input":[{"type":"text","text":"hello"},{"type":"image","data":"data:image/png;base64,aGVsbG8="}],
		"system_instruction":"be concise",
		"generation_config":{"max_output_tokens":128,"top_p":0.8,"thinking_config":{"thinking_level":"low"}},
		"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object"}}},
		"safety_settings":[{"harm_category":"HARM_CATEGORY_DANGEROUS_CONTENT","harm_block_threshold":"BLOCK_NONE"}],
		"tools":[{"type":"function","name":"lookup","description":"look up","parameters":{"type":"object"}}]
	}`)
	converted, err := convertNativeInteractionToGenerateContent(nil, raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	require.Len(t, request["contents"], 1)
	config := request["generationConfig"].(map[string]any)
	require.Equal(t, float64(128), config["maxOutputTokens"])
	require.Equal(t, float64(0.8), config["topP"])
	require.Equal(t, "low", config["thinkingConfig"].(map[string]any)["thinkingLevel"])
	require.Equal(t, "application/json", config["responseMimeType"])
	require.Equal(t, "be concise", request["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"])
	require.Len(t, request["tools"], 1)
	safety := request["safetySettings"].([]any)[0].(map[string]any)
	require.Equal(t, "HARM_CATEGORY_DANGEROUS_CONTENT", safety["category"])
}

func TestParseInteractionResultPreservesUsageForBilling(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-1",
		"status":"completed",
		"usage":{"total_input_tokens":12,"total_output_tokens":8,"total_tokens":20}
	}`))
	require.NoError(t, err)
	require.Equal(t, 12, result.InputTokens)
	require.Equal(t, 8, result.CompletionTokens)
	require.Equal(t, 20, result.TotalTokens)
	require.Equal(t, 12, result.UsageFacts["total_input_tokens"])
	require.Equal(t, 8, result.UsageFacts["total_output_tokens"])
	require.Equal(t, 20, result.UsageFacts["total_tokens"])
}

func TestConvertGenerateContentResponseToInteractionOutput(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"text":"caption"},{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],
		"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":7,"totalTokenCount":10}
	}`), "gemini-nano-banana-2.1")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(converted, &response))
	require.Equal(t, "completed", response["status"])
	require.True(t, strings.HasPrefix(response["id"].(string), "interaction_"))
	require.Len(t, response["outputs"], 2)
	require.Equal(t, float64(10), response["usage"].(map[string]any)["total_tokens"])
	result, err := parseInteractionResult(converted)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
}

func TestApplyNativeInteractionVideoBillingMetadataReadsVeoConfig(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.KeyLyriaRawRequestBody, []byte(`{
		"model":"veo-3.1-generate-001",
		"input":"cinematic shot",
		"generation_config":{"video_config":{"durationSeconds":8,"resolution":"4K","generateAudio":false}}
	}`))
	c.Set("task_request", relaycommon.TaskSubmitReq{})

	applyNativeInteractionVideoBillingMetadata(c)

	require.Equal(t, 8, c.GetInt("video_seconds"))
	require.Equal(t, "4K", c.GetString("video_resolution"))
	request, ok := c.Get("task_request")
	require.True(t, ok)
	require.Equal(t, 8, request.(relaycommon.TaskSubmitReq).Metadata["durationSeconds"])
	require.Equal(t, false, request.(relaycommon.TaskSubmitReq).Metadata["generateAudio"])
}

func TestLyriaPollingUsesExplicitVertexChannelType(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVertexAi,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         "opaque-key-for-routing-test",
		},
	})

	require.True(t, adaptor.shouldUseVertexPolling(
		"https://generativelanguage.googleapis.com",
		"opaque-key-for-routing-test",
	))
}
