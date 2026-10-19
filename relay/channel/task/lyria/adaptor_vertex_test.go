package lyria

import (
	"io"
	"net/http"
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

func TestBuildVertexInteractionsURLUsesExpressModeForAPIKey(t *testing.T) {
	for _, baseURL := range []string{"https://aiplatform.googleapis.com", "https://us-central1-aiplatform.googleapis.com"} {
		url, err := buildVertexInteractionsURL(baseURL, "express-api-key")
		require.NoError(t, err)
		require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/locations/global/interactions", url)
	}
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

func TestBuildVertexGenerateContentURLUsesExpressModeForAPIKey(t *testing.T) {
	for _, baseURL := range []string{"", "https://aiplatform.googleapis.com", "https://us-central1-aiplatform.googleapis.com", "https://generativelanguage.googleapis.com"} {
		url, err := buildVertexGenerateContentURL(baseURL, "express-api-key", "gemini-2.5-flash")
		require.NoError(t, err)
		require.Equal(t, "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:generateContent", url)
	}
}

func TestVertexExpressModeUsesAPIKeyHeader(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi, ApiKey: "express-api-key"},
		OriginModelName:    "gemini-2.5-flash",
		NativeInteractions: true,
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	request := httptest.NewRequest("POST", "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:generateContent", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, adaptor.BuildRequestHeader(c, request, info))
	require.Equal(t, "express-api-key", request.Header.Get("x-goog-api-key"))
	require.Empty(t, request.Header.Get("Authorization"))
}

func TestVertexInteractionsExpressModeUsesAPIKeyHeader(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi, ChannelBaseUrl: "https://aiplatform.googleapis.com", ApiKey: "express-api-key"},
		OriginModelName:    relaycommon.GeminiOmniFlashPreview,
		NativeInteractions: true,
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	request := httptest.NewRequest("POST", "https://aiplatform.googleapis.com/v1beta1/locations/global/interactions", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.NoError(t, adaptor.BuildRequestHeader(c, request, info))
	require.Equal(t, "express-api-key", request.Header.Get("x-goog-api-key"))
	require.Empty(t, request.Header.Get("Authorization"))
}

func TestBuildVertexInteractionsPollURLUsesExpressModePath(t *testing.T) {
	url, useVertex, err := buildLyriaPollURL("https://aiplatform.googleapis.com", "express-api-key", "interaction-1", true)
	require.NoError(t, err)
	require.True(t, useVertex)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/locations/global/interactions/interaction-1", url)
}

func TestVertexExpressModePollUsesAPIKeyHeader(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		_, _ = w.Write([]byte(`{"id":"interaction-1","status":"completed"}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{channelType: constant.ChannelTypeVertexAi}
	response, err := adaptor.FetchTask(server.URL, "express-api-key", map[string]any{"task_id": "interaction-1"}, "")
	require.NoError(t, err)
	defer response.Body.Close()
	received := <-requests
	require.Equal(t, "/v1beta1/locations/global/interactions/interaction-1", received.URL.Path)
	require.Equal(t, "express-api-key", received.Header.Get("x-goog-api-key"))
	require.Empty(t, received.Header.Get("Authorization"))
}

func TestReadVertexInteractionStreamForwardsEventsAndReturnsFinalInteraction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	var forwarded strings.Builder
	c.Set("native_interactions_sse_writer", func(event []byte) { _, _ = forwarded.Write(event) })
	stream := "event: interaction.created\ndata: {\"interaction\":{\"id\":\"interaction-1\",\"object\":\"interaction\",\"status\":\"in_progress\"}}\n\n" +
		"event: interaction.completed\ndata: {\"interaction\":{\"id\":\"interaction-1\",\"object\":\"interaction\",\"status\":\"completed\",\"outputs\":[{\"type\":\"text\",\"text\":\"done\"}]}}\n\n"
	final, err := readVertexInteractionStream(c, strings.NewReader(stream))
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"interaction-1","object":"interaction","status":"completed","outputs":[{"type":"text","text":"done"}]}`, string(final))
	require.Contains(t, forwarded.String(), "event: interaction.created")
	require.Contains(t, forwarded.String(), "event: interaction.completed")
}

func TestReadVertexInteractionStreamReturnsStatelessCompletedInteractionWithoutID(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_model", ProModelName)
	stream := "event: interaction.start\ndata: {\"interaction\":{\"id\":\"interaction-provisional\",\"status\":\"in_progress\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\"}}\n\n" +
		"event: interaction.completed\ndata: {\"interaction\":{\"id\":\"\",\"status\":\"completed\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\",\"outputs\":[{\"type\":\"audio\",\"data\":\"YXVkaW8=\"}]}}\n\n"

	final, err := readVertexInteractionStream(c, strings.NewReader(stream))
	require.NoError(t, err)
	var interaction map[string]any
	require.NoError(t, common.Unmarshal(final, &interaction))
	require.Empty(t, interaction["id"])
	require.Equal(t, "completed", interaction["status"])
	require.Equal(t, []any{map[string]any{"type": "audio", "data": "YXVkaW8="}}, interaction["outputs"])
}

func TestReadVertexInteractionStreamPreservesLyriaAudioStepDeltas(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_model", ProModelName)
	stream := "event: interaction.created\ndata: {\"interaction\":{\"id\":\"interaction-lyria\",\"status\":\"in_progress\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\"}}\n\n" +
		"event: step.start\ndata: {\"event_type\":\"step.start\",\"index\":0,\"step\":{\"type\":\"model_output\"}}\n\n" +
		"event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"audio\",\"mime_type\":\"audio/mpeg\",\"data\":\"YXVkaW8=\"}}\n\n" +
		"event: step.delta\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"audio\",\"data\":\"IHN0cmVhbQ==\"}}\n\n" +
		"event: interaction.completed\ndata: {\"interaction\":{\"id\":\"interaction-lyria\",\"status\":\"completed\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\"}}\n\n"

	final, err := readVertexInteractionStream(c, strings.NewReader(stream))
	require.NoError(t, err)
	result, err := parseInteractionResult(final, ProModelName)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "data:audio/mpeg;base64,YXVkaW8gc3RyZWFt", result.Url)
}

func TestReadVertexInteractionStreamPreservesLyriaContentDeltaAudio(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_model", ProModelName)
	stream := "event: interaction.start\ndata: {\"interaction\":{\"id\":\"interaction-lyria-content\",\"status\":\"in_progress\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\"},\"event_type\":\"interaction.start\"}\n\n" +
		"event: content.start\ndata: {\"index\":0,\"content\":{\"type\":\"audio\"},\"event_type\":\"content.start\"}\n\n" +
		"event: content.delta\ndata: {\"index\":0,\"delta\":{\"type\":\"audio\",\"mime_type\":\"audio/mpeg\",\"data\":\"YXVkaW8=\"},\"event_type\":\"content.delta\"}\n\n" +
		"event: interaction.completed\ndata: {\"id\":\"interaction-lyria-content\",\"status\":\"completed\",\"object\":\"interaction\",\"model\":\"lyria-3-pro-preview\"}\n\n"

	final, err := readVertexInteractionStream(c, strings.NewReader(stream))
	require.NoError(t, err)
	result, err := parseInteractionResult(final, ProModelName)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "data:audio/mpeg;base64,YXVkaW8=", result.Url)
}

func TestReadVertexInteractionStreamRejectsEmptyStream(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	final, err := readVertexInteractionStream(c, strings.NewReader(""))
	require.ErrorContains(t, err, "did not contain an interaction")
	require.Nil(t, final)
}

func TestReadVertexInteractionStreamReturnsUpstreamErrorDetails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	stream := "event: error\ndata: {\"event_type\":\"error\",\"error\":{\"code\":\"invalid_argument\",\"message\":\"sample_rate is unsupported\"}}\n\n"

	final, err := readVertexInteractionStream(c, strings.NewReader(stream))
	require.ErrorContains(t, err, "sample_rate is unsupported")
	require.Nil(t, final)
}

func TestUncataloguedVertexInteractionUsesGenerateContentFallback(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi, ChannelBaseUrl: "https://aiplatform.googleapis.com", ApiKey: `{"project_id":"demo-project"}`},
		OriginModelName:    "gemini-future-model",
		NativeInteractions: true,
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	upstreamURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/publishers/google/models/gemini-future-model:generateContent", upstreamURL)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.KeyLyriaRawRequestBody, []byte(`{"model":"gemini-future-model","input":"hello"}`))
	bodyReader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(bodyReader)
	require.NoError(t, err)
	require.JSONEq(t, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"responseModalities":["TEXT"]}}`, string(body))
	require.True(t, IsVertexGenerateContentModelName("gemini-future-model"))
	require.False(t, IsNativeInteractionGenerateContentModel("gemini-future-model"))
}

func TestVertexGenerateContentFallbackLeavesDedicatedModelsAlone(t *testing.T) {
	for _, modelName := range []string{"lyria-3-pro-preview", "gemini-omni-flash-preview", "veo-3.1-generate-001"} {
		require.False(t, IsVertexGenerateContentModelName(modelName), modelName)
	}
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

func TestBuildRequestBodyRemovesVertexLyriaAudioResponseFormat(t *testing.T) {
	for _, modelName := range []string{ProModelName, ClipModelName} {
		t.Run(modelName, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set(common.KeyLyriaRawRequestBody, []byte(`{
				"model":"`+modelName+`",
				"input":"A gentle piano melody",
				"stream":true,
				"store":false,
				"response_format":[{"type":"audio","delivery":"inline","mime_type":"audio/mp3","sample_rate":24000}]
			}`))
			info := &relaycommon.RelayInfo{
				NativeInteractions: true,
				OriginModelName:    modelName,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    constant.ChannelTypeVertexAi,
					ChannelBaseUrl: "https://aiplatform.googleapis.com",
					ApiKey:         `{"project_id":"demo"}`,
				},
			}

			body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
			require.NoError(t, err)
			forwarded, err := io.ReadAll(body)
			require.NoError(t, err)

			var request map[string]any
			require.NoError(t, common.Unmarshal(forwarded, &request))
			require.NotContains(t, request, "response_format")
			require.Equal(t, true, request["stream"])
			require.Equal(t, false, request["store"])
			require.Equal(t, []any{map[string]any{"type": "text", "text": "A gentle piano melody"}}, request["input"])
		})
	}
}

func TestBuildRequestBodyKeepsGoogleLyriaRequestUnchanged(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A gentle piano melody","stream":true,"store":false,"response_format":[{"type":"audio","delivery":"inline","mime_type":"audio/mp3","sample_rate":24000}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.KeyLyriaRawRequestBody, raw)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    ProModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeGemini,
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, raw, forwarded)
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
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
	require.Equal(t, map[string]any{
		"aspectRatio": "16:9",
		"imageSize":   "2K",
	}, config["imageConfig"])
}

func TestGenerateContentGemini25FlashImageUsesSupportedImageConfig(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-2.5-flash-image",
		"input":"draw a blue square",
		"response_format":[{"type":"image","aspect_ratio":"1:1","delivery":"inline","image_size":"512","mime_type":"image/jpeg"}]
	}`)
	converted, err := convertNativeInteractionToGenerateContent(nil, raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	config := request["generationConfig"].(map[string]any)
	require.Equal(t, []any{"TEXT", "IMAGE"}, config["responseModalities"])
	require.Equal(t, map[string]any{"aspectRatio": "1:1"}, config["imageConfig"])
}

func TestGenerateContentImageSizeIsRestrictedToModelSupport(t *testing.T) {
	for _, test := range []struct {
		name         string
		imageSize    string
		wantImageCfg map[string]any
	}{
		{name: "512 unavailable on lite image", imageSize: "512", wantImageCfg: map[string]any{"aspectRatio": "1:1"}},
		{name: "canonicalize supported resolution", imageSize: "1k", wantImageCfg: map[string]any{"aspectRatio": "1:1", "imageSize": "1K"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"gemini-3.1-flash-lite-image","input":"draw","response_format":{"type":"image","aspect_ratio":"1:1","image_size":"` + test.imageSize + `"}}`)
			converted, err := convertNativeInteractionToGenerateContent(nil, raw)
			require.NoError(t, err)
			var request map[string]any
			require.NoError(t, common.Unmarshal(converted, &request))
			config := request["generationConfig"].(map[string]any)
			require.Equal(t, test.wantImageCfg, config["imageConfig"])
		})
	}
}

func TestGenerateContentTaskActionIsNotLegacyVideoAction(t *testing.T) {
	require.True(t, IsNativeInteractionGenerateContentModel("gemini-nano-banana-2.1"))
	require.Equal(t, constant.TaskActionGenerateContent, nativeInteractionTaskAction(&relaycommon.RelayInfo{OriginModelName: "gemini-nano-banana-2.1"}))
	require.NotEqual(t, constant.TaskActionTextToVideo, constant.NormalizeTaskAction(nativeInteractionTaskAction(&relaycommon.RelayInfo{OriginModelName: "gemini-nano-banana-2.1"})))
	require.Equal(t, "song", nativeInteractionTaskAction(&relaycommon.RelayInfo{OriginModelName: ProModelName}))
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

func TestConvertNativeInteractionToGenerateContentMapsTTSRequest(t *testing.T) {
	tests := []struct {
		name          string
		request       string
		wantText      string
		wantSpeech    map[string]any
		wantPartExtra map[string]any
	}{
		{
			name:       "3.1 single speaker",
			request:    `{"model":"gemini-3.1-flash-tts-preview","input":"Hello there","generation_config":{"speech_config":[{"voice":"Kore"}]},"response_format":{"type":"audio"}}`,
			wantText:   "Hello there",
			wantSpeech: map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": "Kore"}}},
		},
		{
			name:          "3.8 structured input with speech style",
			request:       `{"model":"gemini-3.8-flash-tts","input":[{"type":"user_input","content":[{"type":"text","text":"Hello there","annotations":[{"type":"speech_metadata","style":"cheerful"}]}]}],"generation_config":{"speech_config":[{"voice":"Kore"}]},"response_format":{"type":"audio"}}`,
			wantText:      "Hello there",
			wantSpeech:    map[string]any{"voiceConfig": map[string]any{"voice": "Kore"}},
			wantPartExtra: map[string]any{"speechMetadata": map[string]any{"style": "cheerful"}},
		},
		{
			name:     "3.8 multi speaker",
			request:  `{"model":"gemini-3.8-flash-tts","input":[{"type":"user_input","content":[{"type":"text","text":"Joe: Hello. Jane: Hi."}]}],"generation_config":{"speech_config":{"speakers":[{"speaker":"Joe","voice":"Puck"},{"speaker":"Jane","voice":"Kore"}]}},"response_format":{"type":"audio"}}`,
			wantText: "Joe: Hello. Jane: Hi.",
			wantSpeech: map[string]any{"multiSpeakerVoiceConfig": map[string]any{"speakerVoiceConfigs": []any{
				map[string]any{"speaker": "Joe", "voiceConfig": map[string]any{"voice": "Puck"}},
				map[string]any{"speaker": "Jane", "voiceConfig": map[string]any{"voice": "Kore"}},
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := convertNativeInteractionToGenerateContent(nil, []byte(test.request))
			require.NoError(t, err)
			var request map[string]any
			require.NoError(t, common.Unmarshal(converted, &request))
			config := request["generationConfig"].(map[string]any)
			require.Equal(t, []any{"AUDIO"}, config["responseModalities"])
			require.Equal(t, test.wantSpeech, config["speechConfig"])
			contents := request["contents"].([]any)
			parts := contents[0].(map[string]any)["parts"].([]any)
			part := parts[0].(map[string]any)
			require.Equal(t, test.wantText, part["text"])
			if test.wantPartExtra != nil {
				require.Equal(t, test.wantPartExtra, map[string]any{"speechMetadata": part["speechMetadata"]})
			}
		})
	}
}

func TestConvertGenerateContentAudioResponsePreservesAudioUsageForBilling(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"aGVsbG8="}}]}}],
		"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":264,"totalTokenCount":271,"promptTokensDetails":[{"modality":"TEXT","tokenCount":7}],"candidatesTokensDetails":[{"modality":"AUDIO","tokenCount":264}]}
	}`), "gemini-3.8-flash-tts", nil)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(converted, &response))
	require.Equal(t, "audio", response["outputs"].([]any)[0].(map[string]any)["type"])
	result, err := parseInteractionResult(converted, "gemini-3.8-flash-tts")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 7, result.InputTokens)
	require.Equal(t, 264, result.CompletionTokens)
	require.Equal(t, 264, result.AudioOutputTokens)
	require.Equal(t, 264, result.UsageFacts["audio_output_tokens"])
}

func TestParseInteractionResultPreservesUsageForBilling(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-1",
		"status":"completed",
		"usage":{"total_input_tokens":12,"total_output_tokens":8,"total_tokens":20,"audio_input_tokens":3,"audio_output_tokens":4}
	}`))
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 12, result.InputTokens)
	require.Equal(t, 8, result.CompletionTokens)
	require.Equal(t, 20, result.TotalTokens)
	require.Equal(t, 12, result.UsageFacts["total_input_tokens"])
	require.Equal(t, 8, result.UsageFacts["total_output_tokens"])
	require.Equal(t, 20, result.UsageFacts["total_tokens"])
	require.Equal(t, 3, result.AudioInputTokens)
	require.Equal(t, 4, result.AudioOutputTokens)
	require.Equal(t, 3, result.UsageFacts["audio_input_tokens"])
	require.Equal(t, 4, result.UsageFacts["audio_output_tokens"])
	require.Equal(t, 0, result.UsageFacts["image_input_tokens"])
}

func TestParseInteractionResultReadsUsageModalitiesForImageOutput(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-image-usage",
		"status":"COMPLETED",
		"usage":{
			"total_input_tokens":13,
			"total_output_tokens":2615,
			"total_tokens":2628,
			"input_tokens_by_modality":[{"modality":"text","tokens":13}],
			"output_tokens_by_modality":[{"modality":"image","tokens":2615}]
		}
	}`))
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 13, result.InputTokens)
	require.Equal(t, 2615, result.CompletionTokens)
	require.Equal(t, 2615, result.ImageOutputTokens)
	require.Zero(t, result.VideoOutputTokens)
	require.Equal(t, 2615, result.UsageFacts["image_output_tokens"])
	require.Zero(t, result.UsageFacts["video_output_tokens"])
}

func TestParseInteractionResultDoesNotTreatInputOnlyUsageAsFinalUsage(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-partial-usage",
		"status":"COMPLETED",
		"usage":{"total_input_tokens":7}
	}`))

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestParseInteractionResultDerivesTextOutputFromAggregateTokens(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-text-aggregate-usage",
		"status":"completed",
		"outputs":[{"type":"text","text":"answer"}],
		"usage":{"total_input_tokens":7,"total_tokens":12}
	}`), "gemini-3.8-flash")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 7, result.InputTokens)
	require.Equal(t, 5, result.CompletionTokens)
	require.Equal(t, 12, result.TotalTokens)
}

func TestParseInteractionResultDerivesImageOutputFromAggregateTokens(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-image-aggregate-usage",
		"status":"completed",
		"outputs":[{"type":"image","data":"aGVsbG8="}],
		"usage":{"text_input_tokens":13,"total_tokens":1133}
	}`), "gemini-3-pro-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 13, result.InputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
	require.Equal(t, 1133, result.TotalTokens)
}

func TestParseInteractionResultAcceptsExplicitZeroOutputUsage(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-zero-output",
		"status":"COMPLETED",
		"usage":{"total_input_tokens":7,"total_output_tokens":0,"total_tokens":7}
	}`))

	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 0, result.CompletionTokens)
}

func TestParseInteractionResultDerivesTokenTotalsFromModalities(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-modality-only-usage",
		"status":"COMPLETED",
		"usage":{
			"input_tokens_by_modality":[{"modality":"text","tokens":7}],
			"output_tokens_by_modality":[{"modality":"text","tokens":3}]
		}
	}`))

	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 7, result.InputTokens)
	require.Equal(t, 3, result.CompletionTokens)
	require.Equal(t, 3, result.TextOutputTokens)
}

func TestParseInteractionResultDerivesTokenTotalsFromScalarModalities(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-scalar-modality-usage",
		"status":"COMPLETED",
		"outputs":[{"type":"image","data":"aGVsbG8="}],
		"usage":{
			"text_input_tokens":40,"image_input_tokens":60,
			"text_output_tokens":5,"image_output_tokens":10
		}
	}`), "gemini-3-pro-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 100, result.InputTokens)
	require.Equal(t, 15, result.CompletionTokens)
	require.Equal(t, 40, result.TextInputTokens)
	require.Equal(t, 60, result.ImageInputTokens)
	require.Equal(t, 5, result.TextOutputTokens)
	require.Equal(t, 10, result.ImageOutputTokens)
	require.Equal(t, 115, result.TotalTokens)
}

func TestParseInteractionResultKeepsCompletedResponseWhenUsageCannotBeInterpreted(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{"id":"interaction-no-usage","status":"completed","outputs":[{"type":"text","text":"ok"}],"usage":"unrecognized"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestConvertGenerateContentResponseOmitsUninterpretableUsage(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":"unrecognized"}`), "gemini-2.5-flash", nil)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(converted, &response))
	require.NotContains(t, response, "usage")
	parsed, err := parseInteractionResult(converted)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, parsed.Status)
	require.False(t, parsed.UsageAvailable)
}

func TestConvertGenerateContentResponseToInteractionOutput(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"text":"caption"},{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],
		"usageMetadata":{
			"promptTokenCount":100,
			"candidatesTokenCount":7,
			"totalTokenCount":157,
			"cachedContentTokenCount":60,
			"thoughtsTokenCount":50,
			"promptTokensDetails":[{"modality":"TEXT","tokenCount":40},{"modality":"IMAGE","tokenCount":60}],
			"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":2},{"modality":"IMAGE","tokenCount":5}]
		}
	}`), "gemini-nano-banana-2.1", nil)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(converted, &response))
	require.Equal(t, "completed", response["status"])
	require.True(t, strings.HasPrefix(response["id"].(string), "interaction_"))
	require.Len(t, response["outputs"], 2)
	usage := response["usage"].(map[string]any)
	require.Equal(t, float64(157), usage["total_tokens"])
	require.Equal(t, float64(60), usage["cached_tokens"])
	require.Equal(t, float64(50), usage["thought_tokens"])
	require.Equal(t, float64(40), usage["text_input_tokens"])
	require.Equal(t, float64(60), usage["image_input_tokens"])
	require.Equal(t, float64(2), usage["text_output_tokens"])
	require.Equal(t, float64(5), usage["image_output_tokens"])
	result, err := parseInteractionResult(converted)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, 100, result.InputTokens)
	require.Equal(t, 7, result.CompletionTokens)
	require.Equal(t, 60, result.CachedTokens)
	require.Equal(t, 50, result.ThoughtTokens)
	require.Equal(t, 40, result.TextInputTokens)
	require.Equal(t, 60, result.ImageInputTokens)
	require.Equal(t, 2, result.TextOutputTokens)
}

func TestConvertGenerateContentResponseDerivesMissingAggregateFromImageUsageDetails(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],
		"usageMetadata":{
			"promptTokenCount":13,
			"totalTokenCount":1133,
			"promptTokensDetails":[{"modality":"TEXT","tokenCount":13}],
			"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":1120}]
		}
	}`), "gemini-3.1-flash-image", nil)
	require.NoError(t, err)

	result, err := parseInteractionResult(converted, "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 13, result.InputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
	require.Equal(t, 1133, result.TotalTokens)
}

func TestConvertGenerateContentResponseCorrectsZeroAggregateFromImageUsageDetails(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],
		"usageMetadata":{
			"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0,
			"promptTokensDetails":[{"modality":"TEXT","tokenCount":13},{"modality":"IMAGE","tokenCount":60}],
			"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":1120}]
		}
	}`), "gemini-3.1-flash-image", nil)
	require.NoError(t, err)

	result, err := parseInteractionResult(converted, "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 73, result.InputTokens)
	require.Equal(t, 60, result.ImageInputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
	require.Equal(t, 1193, result.TotalTokens)
}

func TestConvertGenerateContentResponsePreservesImageInputAndOutputDetailsWithoutAggregates(t *testing.T) {
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]}}],
		"usageMetadata":{
			"promptTokensDetails":[{"modality":"TEXT","tokenCount":13},{"modality":"IMAGE","tokenCount":60}],
			"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":1120}]
		}
	}`), "gemini-3.1-flash-image", nil)
	require.NoError(t, err)

	result, err := parseInteractionResult(converted, "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 73, result.InputTokens)
	require.Equal(t, 60, result.ImageInputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
	require.Equal(t, 1193, result.TotalTokens)
	require.Equal(t, 60, result.UsageFacts["image_input_tokens"])
	require.Equal(t, 1120, result.UsageFacts["image_output_tokens"])
}

func TestParseInteractionResultModalityDetailsOverrideZeroAggregate(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-zero-aggregate-with-image-detail",
		"model":"gemini-3.1-flash-image",
		"status":"completed",
		"outputs":[{"type":"image","data":"aGVsbG8="}],
		"usage":{
			"total_input_tokens":0,"total_output_tokens":0,"total_tokens":13,
			"text_input_tokens":13,"image_output_tokens":1120
		}
	}`), "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 13, result.InputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
	require.Equal(t, 1133, result.TotalTokens)
}

func TestParseInteractionResultDoesNotTreatImageOutputAsZeroUsage(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-image-output-without-output-usage",
		"model":"gemini-3.1-flash-image",
		"status":"completed",
		"outputs":[{"type":"image","data":"aGVsbG8="}],
		"usage":{"total_input_tokens":13,"total_output_tokens":0,"total_tokens":13,"text_input_tokens":13}
	}`), "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestParseInteractionResultClassifiesAggregateTextOutput(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-text-output-without-modality-breakdown",
		"model":"gemini-3.5-flash",
		"status":"completed",
		"output_text":"answer",
		"usage":{"total_input_tokens":7,"total_output_tokens":264,"total_tokens":271}
	}`), "gemini-3.5-flash")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 7, result.InputTokens)
	require.Equal(t, 264, result.CompletionTokens)
	require.Equal(t, 264, result.TextOutputTokens)
	require.Equal(t, 264, result.UsageFacts["text_output_tokens"])
}

func TestParseInteractionResultClassifiesAggregateOutputByReturnedModality(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		output   string
		factName string
	}{
		{name: "text from omni model", model: "gemini-omni-flash-preview", output: `"output_text":"answer"`, factName: "text_output_tokens"},
		{name: "image from non-image-named model", model: "gemini-3.5-flash", output: `"outputs":[{"type":"image","data":"aGVsbG8="}]`, factName: "image_output_tokens"},
		{name: "audio from omni model", model: "gemini-omni-flash-preview", output: `"output_audio":{"type":"audio","data":"aGVsbG8="}`, factName: "audio_output_tokens"},
		{name: "video from omni model", model: "gemini-omni-flash-preview", output: `"outputs":[{"type":"video","data":"aGVsbG8="}]`, factName: "video_output_tokens"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"id":"interaction-output-modality","model":"` + test.model + `","status":"completed",` + test.output + `,"usage":{"total_input_tokens":7,"total_output_tokens":264,"total_tokens":271}}`)
			result, err := parseInteractionResult(body, test.model)
			require.NoError(t, err)
			require.True(t, result.UsageAvailable)
			require.Equal(t, 264, result.CompletionTokens)
			require.Equal(t, 264, result.UsageFacts[test.factName])
		})
	}
}

func TestParseInteractionResultRejectsUnsplitAudioOutputUsage(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-mixed-audio-output-without-modality-usage",
		"model":"gemini-omni-flash-preview",
		"status":"completed",
		"outputs":[{"type":"text","text":"transcript"},{"type":"audio","data":"aGVsbG8="}],
		"usage":{"total_input_tokens":7,"total_output_tokens":264,"total_tokens":271}
	}`), "gemini-omni-flash-preview")
	require.NoError(t, err)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestConvertGenerateContentResponseEchoesImageSizeParams(t *testing.T) {
	// 1x1 transparent PNG
	const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	converted, err := convertGenerateContentResponse([]byte(`{
		"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"`+onePixelPNG+`"}}]}}],
		"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":7,"totalTokenCount":10}
	}`), "gemini-nano-banana-2.1", map[string]any{"aspect_ratio": "16:9", "image_size": "2K"})
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(converted, &response))
	block := response["outputs"].([]any)[0].(map[string]any)
	require.Equal(t, "16:9", block["aspect_ratio"])
	require.Equal(t, "2K", block["image_size"])
	require.Equal(t, "1x1", block["size"])
}

func TestParseInteractionResultPrefersExplicitThoughtTokensOverDerived(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{
		"id":"interaction-3",
		"status":"completed",
		"outputs":[{"type":"text","text":"ok"}],
		"usage":{"total_input_tokens":10,"total_output_tokens":5,"total_tokens":100,"thought_tokens":50}
	}`))
	require.NoError(t, err)
	require.Equal(t, 50, result.ThoughtTokens)

	derived, err := parseInteractionResult([]byte(`{
		"id":"interaction-4",
		"status":"completed",
		"outputs":[{"type":"text","text":"ok"}],
		"usage":{"total_input_tokens":10,"total_output_tokens":5,"total_tokens":100}
	}`))
	require.NoError(t, err)
	require.Equal(t, 85, derived.ThoughtTokens)
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
