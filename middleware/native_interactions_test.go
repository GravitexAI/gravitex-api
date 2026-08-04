package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestConvertNativeInteractionRequestPreservesMediaAndExecutionMode(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-omni-flash-preview",
		"input":[
			{"type":"text","text":"edit the video"},
			{"type":"image","mime_type":"image/png","data":"YWJj"},
			{"type":"image","mime_type":"image/jpeg","uri":"https://example.com/ref.jpg"},
			{"type":"video","mime_type":"video/mp4","uri":"gs://bucket/source.mp4"}
		],
		"generation_config":{"video_config":{"task":"edit"}},
		"background":false,
		"stream":true
	}`)

	converted, model, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	require.Equal(t, "gemini-omni-flash-preview", model)

	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, "edit", metadata["task"])
	// Native SSE uses the upstream foreground stream; the completed interaction
	// is still fetched after the stream for task persistence and billing.
	require.Equal(t, false, metadata["background"])
	require.Equal(t, true, metadata["stream"])
	require.Len(t, metadata["images"], 2)
	require.Equal(t, "gs://bucket/source.mp4", metadata["video"])
}

func TestConvertNativeInteractionRequestReadsOfficialResponseFormatArray(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-omni-flash-preview",
		"input":[{"type":"text","text":"make a video"}],
		"response_format":[{"type":"video","delivery":"inline","aspect_ratio":"16:9","duration":"4s"}]
	}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)

	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, "16:9", metadata["aspectRatio"])
	require.Equal(t, float64(4), metadata["durationSeconds"])
}

func TestConvertNativeInteractionRequestPreservesPreviousInteraction(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-omni-flash-preview",
		"input":[{"type":"text","text":"change the style"}],
		"previous_interaction_id":"video-previous",
		"response_format":[{"type":"video","delivery":"uri","gcs_uri":"gs://bucket/output/"}]
	}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, "uri", metadata["delivery"])
	require.Equal(t, "gs://bucket/output/", metadata["gcs_uri"])
	require.Equal(t, "video-previous", metadata["previous_interaction_id"])
}

func TestConvertNativeInteractionRequestRejectsAudioInput(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-omni-flash-preview",
		"input":[{"type":"text","text":"use this soundtrack"},{"type":"audio","mime_type":"audio/mpeg","data":"YWJj"}]
	}`)
	_, _, err := convertNativeInteractionRequest(raw)
	require.EqualError(t, err, "audio input is not supported for gemini-omni-flash-preview video tasks")
}
