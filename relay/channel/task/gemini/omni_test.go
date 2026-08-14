package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

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
