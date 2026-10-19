package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestNativeInteractionResponseUsesPublicTaskIDForLyriaTask(t *testing.T) {
	raw := []byte(`{"data":{"id":1691,"task_id":"fVaOav3tM6O47PkPx_z90A8","platform":"lyria","status":"SUCCESS","fail_reason":""}}`)

	result, err := nativeInteractionResponse(raw, "")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, "fVaOav3tM6O47PkPx_z90A8", response["id"])
	require.Equal(t, "completed", response["status"])
}

func TestSuccessfulTerminalInteractionsTaskIsSettledAtSubmit(t *testing.T) {
	result := &relay.TaskSubmitResult{
		Platform:        constant.TaskPlatformVertexInteractions,
		InitialTaskInfo: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess},
	}

	require.True(t, isSuccessfulTerminalNativeInteractionTask(true, constant.ChannelTypeVertexAi, result))
	require.False(t, isSuccessfulTerminalNativeInteractionTask(false, constant.ChannelTypeVertexAi, result))
	require.False(t, isSuccessfulTerminalNativeInteractionTask(true, constant.ChannelTypeGemini, result))
	result.InitialTaskInfo.Status = model.TaskStatusInProgress
	require.False(t, isSuccessfulTerminalNativeInteractionTask(true, constant.ChannelTypeVertexAi, result))
}

func TestNativeInteractionResponseKeepsDatabaseIDForOtherTaskPlatforms(t *testing.T) {
	raw := []byte(`{"data":{"id":1691,"task_id":"video-task","platform":"video","status":"SUCCESS"}}`)

	result, err := nativeInteractionResponse(raw, "")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, float64(1691), response["id"])
}

func TestMergeLyriaTaskSnapshotUpdatesWrappedTaskDataForCLIQuery(t *testing.T) {
	task := &model.Task{
		TaskID:     "interaction-1",
		Platform:   constant.TaskPlatformLyria,
		Status:     model.TaskStatusFailure,
		FailReason: "invalid_argument: bad prompt",
	}
	raw := []byte(`{"data":{"id":1691,"task_id":"interaction-1","platform":"lyria","status":"SUCCESS"}}`)
	provider := []byte(`{"id":"interaction-1","status":"FAILED","errors":[{"code":"invalid_argument","message":"bad prompt"}]}`)

	merged := mergeLyriaTaskSnapshot(raw, provider, task)
	require.NotEmpty(t, merged)
	converted, err := nativeInteractionResponse(merged, "")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(converted, &response))
	require.Equal(t, "interaction-1", response["id"])
	require.Equal(t, "failed", response["status"])
	require.Equal(t, "invalid_argument: bad prompt", response["fail_reason"])
	require.Len(t, response["errors"], 1)
}

func TestNativeInteractionResponseReturnsUsageFromVideoMetadata(t *testing.T) {
	body, err := nativeInteractionResponse([]byte(`{
		"data": {
			"id": "video-1",
			"model": "gemini-omni-flash-preview",
			"status": "completed",
			"metadata": {
				"usage": {
					"total_input_tokens": 21,
					"total_output_tokens": 17788,
					"input_tokens_by_modality": [{"modality":"text","tokens":21}],
					"output_tokens_by_modality": [{"modality":"video","tokens":17376},{"modality":"thought","tokens":412}]
				}
			}
		}
	}`), "")
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, float64(21), response["usage"].(map[string]any)["total_input_tokens"])
	require.Equal(t, float64(17376), response["usage"].(map[string]any)["output_tokens_by_modality"].([]any)[0].(map[string]any)["tokens"])
}

func TestShouldWaitNativeInteractionSkipsLyriaForegroundWait(t *testing.T) {
	require.False(t, shouldWaitNativeInteraction("lyria-3-pro-preview", false, false, constant.ChannelTypeVertexAi))
	require.False(t, shouldWaitNativeInteraction("lyria-3-clip-preview", false, false, constant.ChannelTypeVertexAi))
	require.True(t, shouldWaitNativeInteraction("gemini-omni-flash-preview", false, false, constant.ChannelTypeVertexAi))
	require.False(t, shouldWaitNativeInteraction("gemini-omni-flash-preview", true, false, constant.ChannelTypeVertexAi))
	require.True(t, shouldWaitNativeInteraction("gemini-omni-flash-preview", true, true, constant.ChannelTypeVertexAi))
	require.True(t, shouldWaitNativeInteraction("veo-3.1-generate-001", false, true, constant.ChannelTypeVertexAi))
	require.False(t, shouldWaitNativeInteraction("gemini-2.5-flash", false, true, constant.ChannelTypeVertexAi))
	require.False(t, shouldWaitNativeInteraction("gemini-future-model", false, false, constant.ChannelTypeVertexAi))
	require.False(t, shouldWaitNativeInteraction("gemini-future-model", false, false, constant.ChannelTypeGemini))
}

func TestNativeInteractionResponsePreservesLyriaOutputs(t *testing.T) {
	raw := []byte(`{
		"id":"interaction-1",
		"status":"completed",
		"model":"lyria-3-pro-preview",
		"outputs":[{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}]
	}`)

	body, err := nativeInteractionResponse(raw, "")
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(body, &response))
	require.Len(t, response["outputs"], 1)
}

func TestNativeInteractionResponseReturnsLyriaSubmitBodyVerbatim(t *testing.T) {
	raw := []byte(`{ "status":"completed", "outputs":[], "provider_future_field":{"enabled":true} }`)

	body, err := nativeInteractionResponse(raw, "lyria-3-pro-preview")
	require.NoError(t, err)
	require.Equal(t, raw, body)
}

func TestNativeInteractionSSEErrorHasTerminalErrorEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeNativeSSEErrorToWriter(recorder, []byte(`{"code":"invalid_request","message":"bad request"}`))
	require.Equal(t, "event: error\ndata: {\"code\":\"invalid_request\",\"message\":\"bad request\"}\n\n", recorder.Body.String())
}

func TestNativeInteractionResponsePreservesGeminiInteractionEnvelope(t *testing.T) {
	raw := []byte(`{"id":"interaction-1","object":"interaction","status":"completed","model":"gemini-future-model","outputs":[{"type":"text","text":"hello"}],"usage":{"total_input_tokens":3},"provider_future_field":true}`)

	body, err := nativeInteractionResponse(raw, "gemini-future-model")

	require.NoError(t, err)
	require.Equal(t, raw, body)
}

func TestWriteNativeInteractionSSEUsesTerminalEventFromInteractionStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeNativeInteractionSSE(recorder, []byte(`{"id":"interaction-1","object":"interaction","status":"completed"}`))

	require.Equal(t, "event: interaction.completed\ndata: {\"id\":\"interaction-1\",\"object\":\"interaction\",\"status\":\"completed\"}\n\n", recorder.Body.String())
}

func TestNativeInteractionTerminalSSEEventIsNotForwardedBeforePersistence(t *testing.T) {
	require.True(t, isNativeInteractionTerminalSSEEvent([]byte("event: interaction.completed\ndata: {\"id\":\"i-1\",\"status\":\"completed\"}")))
	require.True(t, isNativeInteractionTerminalSSEEvent([]byte("event: interaction.failed\ndata: {\"id\":\"i-1\",\"status\":\"failed\"}")))
	require.False(t, isNativeInteractionTerminalSSEEvent([]byte("event: interaction.delta\ndata: {\"text\":\"hello\"}")))
}

func TestNativeInteractionStreamAffinityRequiresSuccessfulTerminalResult(t *testing.T) {
	require.True(t, nativeInteractionStreamSucceeded([]byte(`{"status":"completed"}`)))
	require.False(t, nativeInteractionStreamSucceeded([]byte(`{"status":"failed"}`)))
	require.False(t, nativeInteractionStreamSucceeded([]byte(`{"status":"in_progress"}`)))
}

func TestNativeInteractionBusinessAffinityRejectsNonSuccessfulTerminalStatuses(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "incomplete", "unknown"} {
		require.False(t, nativeInteractionBusinessSucceeded([]byte(`{"status":"`+status+`"}`)), status)
	}
	for _, status := range []string{"completed", "queued", "in_progress"} {
		require.True(t, nativeInteractionBusinessSucceeded([]byte(`{"status":"`+status+`"}`)), status)
	}
}

func TestConvertVertexLyriaResponseToGoogleShape(t *testing.T) {
	raw := []byte(`{
		"id":"interaction-1",
		"status":"completed",
		"model":"lyria-3-pro-preview",
		"outputs":[
			{"type":"text","text":"[Verse] Hello"},
			{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}
		],
		"steps":[{"type":"model_output","content":[{"type":"text","text":"[Verse] Hello"}]}]
	}`)

	body, err := convertVertexLyriaResponseToGoogle(raw)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(body, &response))
	require.Equal(t, "[Verse] Hello", response["output_text"])
	audio := response["output_audio"].(map[string]any)
	require.Equal(t, "audio/mpeg", audio["mime_type"])
	require.Equal(t, "SUQz", audio["data"])
	require.Len(t, response["outputs"], 2)
	require.Len(t, response["steps"], 1)
}

func TestConvertVertexLyriaResponseLeavesNonVertexResponseUnchanged(t *testing.T) {
	raw := []byte(`{"status":"completed","outputs":[{"type":"audio","data":"SUQz"}]}`)
	body, err := convertVertexLyriaResponse(raw, false)
	require.NoError(t, err)
	require.Equal(t, raw, body)
}

func TestNativeInteractionStatusHandlesPersistedTaskStatus(t *testing.T) {
	require.Equal(t, "completed", nativeInteractionStatus(model.TaskStatus(model.TaskStatusSuccess)))
	require.Equal(t, "failed", nativeInteractionStatus(model.TaskStatus(model.TaskStatusFailure)))
	require.Equal(t, "completed", nativeInteractionStatus("SUCCESS"))
}
