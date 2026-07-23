package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBuildOmniRequestBodyUsesInlineDelivery(t *testing.T) {
	body, err := buildOmniRequestBody(relaycommon.TaskSubmitReq{Prompt: "test"})
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	responseFormat, ok := payload["response_format"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "inline", responseFormat["delivery"])
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
			"output_tokens_by_modality":[{"modality":"video","tokens":34}]
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "data:video/mp4;base64,YWJj", result.Url)
	require.Equal(t, 12, result.InputTokens)
	require.Equal(t, 34, result.VideoOutputTokens)
	require.Equal(t, 34, result.CompletionTokens)
	require.Equal(t, 46, result.TotalTokens)
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
