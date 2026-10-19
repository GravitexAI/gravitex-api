package gemini

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiFetchTaskPreservesLegacyRawInteractionID(t *testing.T) {
	const interactionID = "v1_legacy-interaction"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1beta/interactions/"+interactionID, r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"v1_legacy-interaction","status":"completed"}`))
	}))
	defer server.Close()

	response, err := (&TaskAdaptor{}).FetchTask(server.URL, "test-key", map[string]any{"task_id": interactionID}, "")
	require.NoError(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func TestReadOmniStreamFetchesFullResultWhenCompletionOnlyContainsID(t *testing.T) {
	const fullResult = `{"id":"v1_stream-interaction","status":"completed","usage":{"total_input_tokens":7,"total_output_tokens":3},"outputs":[{"type":"video","uri":"gs://example/video.mp4"}]}`
	stream := "event: interaction.completed\ndata: {\"id\":\"v1_stream-interaction\"}\n\n"
	result, err := ReadOmniStream(strings.NewReader(stream), nil, func(id string) (*http.Response, error) {
		assert.Equal(t, "v1_stream-interaction", id)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(fullResult))}, nil
	})

	require.NoError(t, err)
	assert.JSONEq(t, fullResult, string(result))
}

func TestReadOmniStreamFetchesFullResultWhenNestedCompletionOnlyContainsID(t *testing.T) {
	const fullResult = `{"id":"v1_nested-interaction","status":"completed","usage":{"total_input_tokens":5,"total_output_tokens":2},"outputs":[{"type":"image","data":"aGVsbG8="}]}`
	stream := "event: interaction.completed\ndata: {\"interaction\":{\"id\":\"v1_nested-interaction\"}}\n\n"
	result, err := ReadOmniStream(strings.NewReader(stream), nil, func(id string) (*http.Response, error) {
		assert.Equal(t, "v1_nested-interaction", id)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(fullResult))}, nil
	})

	require.NoError(t, err)
	assert.JSONEq(t, fullResult, string(result))
}

func TestConvertToOpenAIVideoDoesNotSetCompletedAtWhileOmniIsInProgress(t *testing.T) {
	adaptor := &TaskAdaptor{}
	data, err := adaptor.ConvertToOpenAIVideo(&model.Task{
		TaskID:    "omni:interaction-1",
		Status:    model.TaskStatusInProgress,
		Progress:  "50%",
		CreatedAt: 100,
		UpdatedAt: 200,
		Properties: model.Properties{
			OriginModelName: omniModelName,
		},
	})
	require.NoError(t, err)
	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &response))
	_, hasCompletedAt := response["completed_at"]
	require.False(t, hasCompletedAt)
}

func TestGeminiInteractionsPassesThroughUnknownModelRequest(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		OriginModelName:    "gemini-future-model",
		NativeInteractions: true,
	}
	adaptor := &TaskAdaptor{baseURL: "https://generativelanguage.googleapis.com", apiKey: "test-key"}
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/interactions", url)

	raw := []byte(`{"model":"gemini-future-model","input":[{"type":"audio","data":"YWJj"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(common.KeyLyriaRawRequestBody, raw)
	bodyReader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(bodyReader)
	require.NoError(t, err)
	require.Equal(t, raw, body)
}

func TestParseNativeInteractionTaskResultUsesGenericOutputUsage(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-1",
		"status":"completed",
		"usage":{"total_input_tokens":7,"total_output_tokens":5,"total_tokens":12}
	}`), "gemini-3.8-flash")
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 7, result.InputTokens)
	require.Equal(t, 5, result.CompletionTokens)
	require.Equal(t, 12, result.TotalTokens)
}

func TestParseNativeInteractionTaskResultClassifiesAggregateTextOutput(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-text-output-without-modality-breakdown",
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

func TestParseNativeInteractionTaskResultClassifiesAggregateOutputByReturnedModality(t *testing.T) {
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
			body := []byte(`{"id":"interaction-output-modality","status":"completed",` + test.output + `,"usage":{"total_input_tokens":7,"total_output_tokens":264,"total_tokens":271}}`)
			result, err := ParseNativeInteractionTaskResult(body, test.model)
			require.NoError(t, err)
			require.True(t, result.UsageAvailable)
			require.Equal(t, 264, result.CompletionTokens)
			require.Equal(t, 264, result.UsageFacts[test.factName])
		})
	}
}

func TestParseNativeInteractionTaskResultRejectsUnsplitAudioOutputUsage(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-mixed-audio-output-without-modality-usage",
		"status":"completed",
		"outputs":[{"type":"text","text":"transcript"},{"type":"audio","data":"aGVsbG8="}],
		"usage":{"total_input_tokens":7,"total_output_tokens":264,"total_tokens":271}
	}`), "gemini-omni-flash-preview")
	require.NoError(t, err)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestParseNativeInteractionTaskResultDerivesZeroAggregateOutputByReturnedModality(t *testing.T) {
	for _, test := range []struct {
		name     string
		output   string
		factName string
	}{
		{name: "text", output: `"output_text":"answer"`, factName: "text_output_tokens"},
		{name: "image", output: `"outputs":[{"type":"image","data":"aGVsbG8="}]`, factName: "image_output_tokens"},
		{name: "audio", output: `"output_audio":{"type":"audio","data":"aGVsbG8="}`, factName: "audio_output_tokens"},
		{name: "video", output: `"outputs":[{"type":"video","data":"aGVsbG8="}]`, factName: "video_output_tokens"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"id":"interaction-zero-output-aggregate","status":"completed",` + test.output + `,"usage":{"total_input_tokens":7,"total_output_tokens":0,"total_tokens":271}}`)
			result, err := ParseNativeInteractionTaskResult(body, "gemini-omni-flash-preview")
			require.NoError(t, err)
			require.True(t, result.UsageAvailable)
			require.Equal(t, 264, result.CompletionTokens)
			require.Equal(t, 264, result.UsageFacts[test.factName])
		})
	}
}

func TestParseNativeInteractionTaskResultDerivesTextOutputFromAggregateTokens(t *testing.T) {
	for _, modelName := range []string{"gemini-3.6-flash", "gemini-3.7-flash", "gemini-3.8-flash"} {
		t.Run(modelName, func(t *testing.T) {
			result, err := ParseNativeInteractionTaskResult([]byte(`{
				"id":"interaction-text-aggregate-usage",
				"status":"completed",
				"outputs":[{"type":"text","text":"answer"}],
				"usage":{"total_input_tokens":7,"total_tokens":12}
			}`), modelName)
			require.NoError(t, err)
			require.True(t, result.UsageAvailable)
			require.Equal(t, 7, result.InputTokens)
			require.Equal(t, 5, result.CompletionTokens)
			require.Equal(t, 12, result.TotalTokens)
		})
	}
}

func TestParseNativeInteractionTaskResultDoesNotTreatInputOnlyAsComplete(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-input-only-usage",
		"status":"completed",
		"outputs":[{"type":"text","text":"answer"}],
		"usage":{"total_input_tokens":7}
	}`), "gemini-3.8-flash")
	require.NoError(t, err)
	require.False(t, result.UsageAvailable)
}

func TestParseNativeInteractionTaskResultDerivesImageOutputFromAggregateTokens(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-image-aggregate-usage",
		"status":"completed",
		"outputs":[{"type":"image","data":"aGVsbG8="}],
		"usage":{"total_input_tokens":13,"total_tokens":1133}
	}`), "gemini-3-pro-image")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 13, result.InputTokens)
	require.Equal(t, 1120, result.CompletionTokens)
	require.Equal(t, 1120, result.ImageOutputTokens)
}

func TestParseNativeInteractionTaskResultUsesScalarModalityUsage(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-scalar-modality-usage",
		"status":"completed",
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

func TestParseNativeInteractionTaskResultModalityUsageOverridesZeroAggregate(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-zero-aggregate-with-image-detail",
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

func TestParseNativeInteractionTaskResultDoesNotTreatImageOutputAsZeroUsage(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-image-output-without-output-usage",
		"status":"completed",
		"outputs":[{"type":"text","text":"caption"},{"type":"image","data":"aGVsbG8="}],
		"usage":{"total_input_tokens":13,"total_output_tokens":0,"total_tokens":13,"text_input_tokens":13}
	}`), "gemini-3.1-flash-image")
	require.NoError(t, err)
	require.False(t, result.UsageAvailable)
	require.Empty(t, result.UsageFacts)
}

func TestParseNativeInteractionTaskResultUsesModalitiesAndPreservesExplicitZeroUsage(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-2",
		"status":"completed",
		"usage":{
			"total_input_tokens":0,
			"total_output_tokens":0,
			"total_tokens":0,
			"input_tokens_by_modality":[{"modality":"text","tokens":0}],
			"output_tokens_by_modality":[{"modality":"text","tokens":0}]
		}
	}`), "gemini-3.8-flash")
	require.NoError(t, err)
	require.True(t, result.UsageAvailable)
	require.Zero(t, result.InputTokens)
	require.Zero(t, result.CompletionTokens)
	require.Zero(t, result.TotalTokens)
}

func TestParseNativeInteractionTaskResultSeparatesThoughtAndImageOutput(t *testing.T) {
	result, err := ParseNativeInteractionTaskResult([]byte(`{
		"id":"interaction-3",
		"status":"completed",
		"usage":{
			"total_input_tokens":7,
			"total_output_tokens":5,
			"total_tokens":12,
			"total_thought_tokens":2,
			"output_tokens_by_modality":[
				{"modality":"text","tokens":2},
				{"modality":"image","tokens":1},
				{"modality":"thought","tokens":2}
			]
		}
	}`), "gemini-3-pro-image")
	require.NoError(t, err)
	require.Equal(t, 3, result.CompletionTokens)
	require.Equal(t, 2, result.ThoughtTokens)
	require.Equal(t, 2, result.TextOutputTokens)
	require.Equal(t, 1, result.ImageOutputTokens)
}

func TestGeminiInteractionTaskIDMarkerRoundTripsWithoutChangingPublicID(t *testing.T) {
	marked := markInteractionTaskID("interaction-123")
	require.Equal(t, "interaction-123", unmarkInteractionTaskID(marked))
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/interactions/interaction-123",
		omniInteractionURL("https://generativelanguage.googleapis.com", unmarkInteractionTaskID(marked)))
}

func TestGeminiInteractionsPersistsUpstreamUsageForLogs(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		OriginModelName:    "gemini-future-model",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"id":"interaction-1","object":"interaction","status":"completed","usage":{"total_input_tokens":12,"provider_extension":7}}`)),
	}

	taskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "interaction:interaction-1", taskID)
	require.Equal(t, 12.0, info.UpstreamResponses["usage"].(map[string]any)["total_input_tokens"])
	require.Equal(t, 7.0, info.UpstreamResponses["usage"].(map[string]any)["provider_extension"])
}

func TestGeminiInteractionsStreamsAndSettlesFromFinalInteraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		_, _ = w.Write([]byte(`{"id":"interaction-stream-1","object":"interaction","status":"completed","usage":{"total_input_tokens":4,"total_output_tokens":2}}`))
	}))
	defer server.Close()

	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		OriginModelName:    "gemini-3.6-flash",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{},
	}
	var forwarded []byte
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_stream", true)
	c.Set("native_interactions_sse_writer", func(event []byte) { forwarded = append(forwarded, event...) })
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: interaction.created\ndata: {\"interaction_id\":\"interaction-stream-1\"}\n\nevent: interaction.completed\ndata: {\"interaction_id\":\"interaction-stream-1\"}\n\n")),
	}
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "test-key"}

	taskID, _, taskErr := adaptor.DoResponse(c, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "interaction:interaction-stream-1", taskID)
	require.Contains(t, string(forwarded), "event: interaction.completed")
	require.Equal(t, 4.0, info.UpstreamResponses["usage"].(map[string]any)["total_input_tokens"])
}

func TestGeminiInteractionsStreamRequestsAcceptSSE(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		OriginModelName:    "gemini-3.6-flash",
		NativeInteractions: true,
	}
	request := httptest.NewRequest(http.MethodPost, "https://generativelanguage.googleapis.com/v1beta/interactions", nil)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_stream", true)

	err := (&TaskAdaptor{apiKey: "test-key"}).BuildRequestHeader(c, request, info)

	require.NoError(t, err)
	require.Equal(t, "text/event-stream", request.Header.Get("Accept"))
}

func TestGeminiOmniStreamUsesCompletionEventWhenFetchBodyIsNotJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("event: message\ndata: {\"code\":\"invalid_response\",\"message\":\"upstream GET did not return JSON\"}\n\n"))
	}))
	defer server.Close()

	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini},
		OriginModelName:    relaycommon.GeminiOmni11FlashPreview,
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{},
	}
	var forwarded []byte
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_stream", true)
	c.Set("native_interactions_sse_writer", func(event []byte) { forwarded = append(forwarded, event...) })
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"interaction\":{\"id\":\"omni-1\",\"status\":\"completed\",\"usage\":{\"total_input_tokens\":5}},\"event_type\":\"interaction.completed\"}\n\n")),
	}
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "test-key"}

	taskID, _, taskErr := adaptor.DoResponse(c, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "omni:omni-1", taskID)
	require.Contains(t, string(forwarded), "interaction.completed")
	usageBytes, err := common.Marshal(info.UpstreamResponses["usage"])
	require.NoError(t, err)
	var usage map[string]any
	require.NoError(t, common.Unmarshal(usageBytes, &usage))
	require.Equal(t, float64(5), usage["total_input_tokens"])
}

func TestReadOmniStreamRejectsEmptyStream(t *testing.T) {
	_, err := ReadOmniStream(strings.NewReader(""), nil, nil)
	require.ErrorContains(t, err, "did not contain interaction id")
}

func TestBuildOmniRequestBodySupportsReferenceImagesAndEditVideo(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt: "edit this scene",
		Images: []string{
			"data:image/png;base64,YWJj",
			"data:image/jpeg;base64,ZGVm",
		},
		Metadata: map[string]interface{}{
			"task":       "reference_to_video",
			"video":      "gs://bucket/source.mp4",
			"background": false,
		},
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, false, payload["background"])
	videoConfig := payload["generation_config"].(map[string]interface{})["video_config"].(map[string]interface{})
	require.Equal(t, "reference_to_video", videoConfig["task"])
	contents := payload["input"].([]interface{})
	require.Len(t, contents, 4) // text + two reference images + edit source video
	require.Equal(t, "video", contents[3].(map[string]interface{})["type"])
	require.Equal(t, "gs://bucket/source.mp4", contents[3].(map[string]interface{})["uri"])
}

func TestBuildOmniRequestBodyUsesEditTaskForVideoInput(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt:         "remove the backpack",
		InputReference: "gs://bucket/source.mp4",
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	videoConfig := payload["generation_config"].(map[string]interface{})["video_config"].(map[string]interface{})
	require.Equal(t, "edit", videoConfig["task"])
	responseFormats := payload["response_format"].([]interface{})
	responseFormat := responseFormats[0].(map[string]interface{})
	_, hasAspectRatio := responseFormat["aspect_ratio"]
	require.False(t, hasAspectRatio)
}

func TestBuildOmniRequestBodyTreatsImageInputReferenceAsImage(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt:         "generate from image",
		InputReference: "data:image/jpeg;base64,YWJj",
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))

	contents := payload["input"].([]interface{})
	require.Len(t, contents, 2)
	image := contents[1].(map[string]interface{})
	require.Equal(t, "image", image["type"])
	require.Equal(t, "image/jpeg", image["mime_type"])
	videoConfig := payload["generation_config"].(map[string]interface{})["video_config"].(map[string]interface{})
	require.Equal(t, "image_to_video", videoConfig["task"])
}

func TestBuildOmniRequestBodyDeduplicatesImageInputReference(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt:         "generate from image",
		Images:         []string{"data:image/jpeg;base64,YWJj"},
		InputReference: "data:image/jpeg;base64,YWJj",
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["input"].([]interface{}), 2)
}

func TestBuildOmniRequestBodyUsesInlineDelivery(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{Prompt: "test"})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	responseFormats, ok := payload["response_format"].([]interface{})
	require.True(t, ok)
	responseFormat := responseFormats[0].(map[string]interface{})
	require.Equal(t, "inline", responseFormat["delivery"])
}

func TestBuildOmniRequestBodyPreservesModelAndNewResolution(t *testing.T) {
	for _, tc := range []struct {
		model          string
		resolution     string
		wantResolution bool
	}{
		{omniModelName, "720p", false},
		{"gemini-omni-1.1-flash-preview", "360p", true},
		{"gemini-omni-1.1-flash-preview", "720p", true},
		{"gemini-omni-1.1-flash-preview", "1080p", true},
		{"gemini-omni-1.1-flash-preview", "4k", true},
		{"gemini-omni-1.1-flash-preview", "bogus", false},
	} {
		t.Run(tc.model+"/"+tc.resolution, func(t *testing.T) {
			body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
				Model: tc.model, Prompt: "make a video",
				Metadata: map[string]interface{}{"resolution": tc.resolution},
			})
			require.NoError(t, err)
			var payload map[string]interface{}
			require.NoError(t, common.Unmarshal(body, &payload))
			require.Equal(t, tc.model, payload["model"])
			format := payload["response_format"].([]interface{})[0].(map[string]interface{})
			if tc.wantResolution {
				require.Equal(t, tc.resolution, format["resolution"])
			} else {
				_, exists := format["resolution"]
				require.False(t, exists)
			}
		})
	}
}

func TestGeminiOmni11UsesInteractionsEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://generativelanguage.googleapis.com"}
	url, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{OriginModelName: relaycommon.GeminiOmni11FlashPreview})
	require.NoError(t, err)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1beta/interactions", url)
}

func TestBuildOmniRequestBodyUsesUpstreamStreamForNativeSSE(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt: "stream a video",
		Metadata: map[string]interface{}{
			"stream":     true,
			"background": true,
		},
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, false, payload["background"])
	require.Equal(t, true, payload["stream"])
}

func TestBuildOmniRequestBodySupportsGCSDelivery(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt: "make a video",
		Metadata: map[string]interface{}{
			"delivery": "uri",
			"gcs_uri":  "gs://bucket/output/",
		},
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	contents := payload["input"].([]interface{})
	require.Len(t, contents, 1)
	responseFormat := payload["response_format"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "uri", responseFormat["delivery"])
	require.Equal(t, "gs://bucket/output/", responseFormat["gcs_uri"])
}

func TestBuildOmniRequestBodySupportsPreviousInteraction(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{
		Prompt: "change the style",
		Metadata: map[string]interface{}{
			"previous_interaction_id": "video-previous",
		},
	})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "video-previous", payload["previous_interaction_id"])
}

func TestParseOmniTaskResultUsesStepError(t *testing.T) {
	result, err := ParseOmniTaskResult([]byte(`{
		"id":"v1_test",
		"status":"failed",
		"steps":[{"type":"model_output","error":{"code":3,"message":"unsupported input uri type"}}]
	}`))

	require.NoError(t, err)
	require.Equal(t, "FAILURE", result.Status)
	require.Equal(t, "unsupported input uri type", result.Reason)
}

func TestOmniTaskIDMarkerIsInternalOnly(t *testing.T) {
	const interactionID = "video-test"
	marked := markOmniTaskID(interactionID)
	require.Equal(t, "omni:video-test", marked)
	require.Equal(t, interactionID, unmarkOmniTaskID(marked))
}

func TestParseOmniTaskResultExtractsVideoDataAndUsage(t *testing.T) {
	result, err := ParseOmniTaskResult([]byte(`{
		"id":"v1_test",
		"status":"completed",
		"steps":[{"type":"model_output","content":[{"type":"video","mime_type":"video/mp4","data":"YWJj"}]}],
		"usage":{
			"total_input_tokens":12,
			"total_output_tokens":34,
			"total_tokens":46,
			"input_tokens_by_modality":[{"modality":"text","tokens":5},{"modality":"image","tokens":7}],
			"output_tokens_by_modality":[{"modality":"video","tokens":34}]
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "data:video/mp4;base64,YWJj", result.Url)
	require.Equal(t, 12, result.InputTokens)
	require.Equal(t, 5, result.TextInputTokens)
	require.Equal(t, 7, result.ImageInputTokens)
	require.Zero(t, result.VideoInputTokens)
	require.Equal(t, 34, result.VideoOutputTokens)
	require.Equal(t, 34, result.CompletionTokens)
	require.Equal(t, 46, result.TotalTokens)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 12, result.UsageFacts["input_tokens"])
	require.Equal(t, 34, result.UsageFacts["output_tokens"])
}

func TestParseOmniTaskResultSeparatesThoughtTokensFromVideoTokens(t *testing.T) {
	result, err := ParseOmniTaskResult([]byte(`{
		"id":"v1_test",
		"status":"completed",
		"usage":{
			"total_input_tokens":12,
			"total_output_tokens":100,
			"total_tokens":112,
			"total_thought_tokens":10
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, 10, result.TextOutputTokens)
	require.Equal(t, 90, result.VideoOutputTokens)
	require.Equal(t, 90, result.CompletionTokens)
	require.Equal(t, 112, result.TotalTokens)
	require.True(t, result.UsageAvailable)
	require.Equal(t, 10, result.ThoughtTokens)
}

func TestParseOmniTaskResultKeepsURIWithoutOSSConversion(t *testing.T) {
	result, err := ParseOmniTaskResult([]byte(`{
		"id":"v1_test",
		"status":"completed",
		"steps":[{"type":"model_output","content":[{"type":"video","uri":"gs://bucket/video.mp4"}]}]
	}`))

	require.NoError(t, err)
	require.Equal(t, "gs://bucket/video.mp4", result.RemoteUrl)
	require.Empty(t, result.Url)
}
