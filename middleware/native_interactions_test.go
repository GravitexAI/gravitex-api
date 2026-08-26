package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
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

func TestConvertNativeInteractionRequestForLyriaPreservesAudioInputShape(t *testing.T) {
	raw := []byte(`{
		"model":"lyria-3-pro-preview",
		"input":[
			{"type":"text","text":"A cinematic piano song"},
			{"type":"image","mime_type":"image/jpeg","data":"YWJj"}
		],
		"response_format":{"type":"audio"},
		"background":false
	}`)

	converted, model, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	require.Equal(t, "lyria-3-pro-preview", model)

	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	require.Equal(t, "lyria-3-pro-preview", request["model"])
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, "audio", metadata["response_format"].(map[string]any)["type"])
	require.Len(t, metadata["input"], 2)
	require.NotContains(t, metadata, "video")
	require.Equal(t, false, metadata["background"])
}

func TestConvertNativeInteractionRequestForLyriaClipUsesSameBranch(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-clip-preview","input":"30-second instrumental loop"}`)
	converted, model, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	require.Equal(t, "lyria-3-clip-preview", model)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	require.Equal(t, "lyria-3-clip-preview", request["model"])
}

func TestConvertNativeInteractionRequestForLyriaPreservesForegroundMode(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":false}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, false, metadata["background"])
}

func TestConvertNativeInteractionRequestForLyriaForwardsBackgroundTrue(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":true}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, true, metadata["background"])
}

func TestConvertNativeInteractionRequestForLyriaDoesNotInjectBackground(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-clip-preview","input":"A short instrumental loop"}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.NotContains(t, metadata, "background")
	require.NotContains(t, metadata, "store")
}

func TestConvertNativeInteractionRequestForLyriaAcceptsStoreFalse(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","store":false}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, false, metadata["store"])
}

func TestConvertNativeInteractionRequestForLyriaForwardsStoreTrue(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","store":true}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, true, metadata["store"])
}

func TestConvertNativeInteractionRequestForLyriaDoesNotValidateOptionalParameterTypes(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":"provider-value","store":"provider-value"}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.Equal(t, "provider-value", metadata["background"])
	require.Equal(t, "provider-value", metadata["store"])
}

func TestConvertNativeInteractionRequestForLyriaLeavesParameterValidationToUpstream(t *testing.T) {
	raw := []byte(`{"model":"lyria-3-pro-preview","future_parameter":{"enabled":true}}`)

	_, model, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	require.Equal(t, "lyria-3-pro-preview", model)
}

func TestNativeInteractionsPreservesOriginalLyriaRequestBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{ "model": "lyria-3-pro-preview", "input": [{"type":"text","text":"song"}], "future_parameter": 7 }`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.KeyBodyStorage, mustNativeInteractionBodyStorage(t, raw))

	NativeInteractions()(c)

	stored, exists := c.Get("lyria_raw_request_body")
	require.True(t, exists)
	require.Equal(t, raw, stored)
}

func mustNativeInteractionBodyStorage(t *testing.T, raw []byte) common.BodyStorage {
	t.Helper()
	storage, err := common.CreateBodyStorage(raw)
	require.NoError(t, err)
	_, err = storage.Seek(0, io.SeekStart)
	require.NoError(t, err)
	return storage
}

func TestConvertNativeInteractionRequestKeepsOmniVideoBranch(t *testing.T) {
	raw := []byte(`{
		"model":"gemini-omni-flash-preview",
		"input":[{"type":"text","text":"make a video"}]
	}`)

	converted, _, err := convertNativeInteractionRequest(raw)
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, common.Unmarshal(converted, &request))
	metadata := request["metadata"].(map[string]any)
	require.NotContains(t, metadata, "input")
}

func TestShouldUseLyriaNativeAdapterRequiresExactGatewayPath(t *testing.T) {
	require.True(t, shouldUseLyriaNativeAdapter("/v1beta/interactions", "lyria-3-pro-preview"))
	require.False(t, shouldUseLyriaNativeAdapter("/v1beta1/projects/demo/locations/global/interactions", "lyria-3-pro-preview"))
	require.False(t, shouldUseLyriaNativeAdapter("/v1beta/interactions", "gemini-omni-flash-preview"))
}
