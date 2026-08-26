package lyria

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoResponseForwardsFailedLyriaCreateVerbatim(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	raw := `{"id":"interaction-failed","status":"failed","error":{"code":"invalid_request","message":"bad prompt"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: ProModelName,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	_, body, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	require.Equal(t, raw, string(body))
	require.Equal(t, raw, recorder.Body.String())
}

func TestDoResponseForwardsNonTerminalLyriaCreateVerbatim(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	raw := `{"id":"interaction-pending","status":"in_progress"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: ProModelName,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	_, body, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	require.Equal(t, raw, string(body))
	require.Equal(t, raw, recorder.Body.String())
}

func TestDoResponseForwardsCompletedLyriaCreateWithoutAudioVerbatim(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	raw := `{"id":"interaction-empty","status":"completed","outputs":[]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: ProModelName,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	_, body, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	require.Equal(t, raw, string(body))
	require.Equal(t, raw, recorder.Body.String())
}

func TestBuildRequestBodyForwardsOriginalNativeLyriaJSON(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	raw := []byte(`{ "model": "lyria-3-pro-preview", "input": [{"type":"text","text":"song"}], "future_parameter": 7 }`)
	c.Set("lyria_raw_request_body", raw)

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, raw, forwarded)
}

func TestBuildRequestBodyConvertsGoogleTextInputForVertex(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("lyria_raw_request_body", []byte(`{"model":"lyria-3-pro-preview","input":"A piano song"}`))
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    ProModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    41,
			ChannelBaseUrl: "https://aiplatform.googleapis.com/v1beta1/projects/demo/locations/global",
			ApiKey:         `{"project_id":"demo"}`,
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(forwarded, &decoded))
	require.Equal(t, ProModelName, decoded["model"])
	require.Equal(t, []any{map[string]any{"type": "text", "text": "A piano song"}}, decoded["input"])
}

func TestBuildRequestBodyConvertsGoogleAsyncFlagsForVertexLyria(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("lyria_raw_request_body", []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":true,"store":true}`))
	c.Set("native_interactions_original_path", "/v1beta/interactions")
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    ProModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    41,
			ChannelBaseUrl: "https://aiplatform.googleapis.com/v1beta1/projects/demo/locations/global",
			ApiKey:         `{"project_id":"demo"}`,
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(forwarded, &decoded))
	require.Equal(t, false, decoded["background"])
	require.Equal(t, false, decoded["store"])
	require.Equal(t, []any{map[string]any{"type": "text", "text": "A piano song"}}, decoded["input"])
}

func TestBuildRequestBodyKeepsAsyncFlagsForNonTargetEndpoint(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":true,"store":true}`)
	c.Set("lyria_raw_request_body", raw)
	c.Set("native_interactions_original_path", "/v1/video/generations")
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    ProModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    1,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         "AIza-test",
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, string(raw), string(forwarded))
}

func TestBuildRequestBodyKeepsGoogleTextInputForGoogleChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	raw := []byte(`{"model":"lyria-3-pro-preview","input":"A piano song"}`)
	c.Set("lyria_raw_request_body", raw)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    1,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         "AIza-test",
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, raw, forwarded)
}

func TestBuildRequestBodyConvertsGoogleAsyncFlagsForGoogleLyria(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("lyria_raw_request_body", []byte(`{"model":"lyria-3-pro-preview","input":"A piano song","background":true,"store":true}`))
	c.Set("native_interactions_original_path", "/v1beta/interactions")
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    ProModelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    1,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         "AIza-test",
		},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	forwarded, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(forwarded, &decoded))
	require.Equal(t, ProModelName, decoded["model"])
	require.Equal(t, "A piano song", decoded["input"])
	require.Equal(t, false, decoded["background"])
	require.Equal(t, false, decoded["store"])
}

func TestParseInteractionResultReturnsAudioAndLyrics(t *testing.T) {
	body := []byte(`{
		"id":"interaction-1",
		"status":"completed",
		"model":"lyria-3-pro-preview",
		"steps":[{"type":"model_output","content":[
			{"type":"text","text":"[Verse] Hello"},
			{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}
		]}]
	}`)

	result, err := parseInteractionResult(body)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "data:audio/mpeg;base64,SUQz", result.Url)
	require.Equal(t, "100%", result.Progress)
	require.Equal(t, "[Verse] Hello", result.Metadata["lyrics"])
}

func TestParseInteractionResultReadsGoogleConvenienceOutputFields(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{"id":"i-2","status":"completed","output_text":"Lyrics","output_audio":{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "data:audio/mpeg;base64,SUQz", result.Url)
	require.Equal(t, "Lyrics", result.Metadata["lyrics"])
}

func TestParseInteractionResultSupportsLyriaOutputsShape(t *testing.T) {
	body := []byte(`{
		"id":"interaction-2",
		"status":"completed",
		"outputs":[
			{"type":"text","text":"[Chorus] Home"},
			{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}
		]
	}`)

	result, err := parseInteractionResult(body)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "data:audio/mpeg;base64,SUQz", result.Url)
	require.Equal(t, "[Chorus] Home", result.Metadata["lyrics"])
}

func TestParseInteractionResultClassifiesEveryVertexTerminalAndNonTerminalStatus(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus string
		wantReason string
	}{
		{name: "failed errors array", body: `{"id":"i-1","status":"FAILED","errors":[{"code":"invalid_argument","message":"bad prompt"}]}`, wantStatus: string(model.TaskStatusFailure), wantReason: "invalid_argument: bad prompt"},
		{name: "cancelled", body: `{"id":"i-1","status":"CANCELLED"}`, wantStatus: string(model.TaskStatusCancelled), wantReason: "cancelled: Vertex interaction was cancelled"},
		{name: "incomplete", body: `{"id":"i-1","status":"INCOMPLETE"}`, wantReason: "incomplete: Vertex returned incomplete results"},
		{name: "budget exceeded", body: `{"id":"i-1","status":"BUDGET_EXCEEDED"}`, wantReason: "budget_exceeded: Vertex interaction budget was exceeded"},
		{name: "requires action", body: `{"id":"i-1","status":"REQUIRES_ACTION"}`, wantReason: "requires_action: Vertex interaction requires unsupported user action"},
		{name: "in progress", body: `{"id":"i-1","status":"IN_PROGRESS"}`, wantReason: "non_terminal_response_not_retrievable: Vertex returned IN_PROGRESS while store=false"},
		{name: "queued", body: `{"id":"i-1","status":"QUEUED"}`, wantReason: "non_terminal_response_not_retrievable: Vertex returned QUEUED while store=false"},
		{name: "unspecified", body: `{"id":"i-1","status":"UNSPECIFIED"}`, wantReason: "invalid_response: Vertex returned UNSPECIFIED interaction status"},
		{name: "missing", body: `{"id":"i-1"}`, wantReason: "invalid_response: Vertex response is missing interaction status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInteractionResult([]byte(tt.body))
			require.NoError(t, err)
			wantStatus := tt.wantStatus
			if wantStatus == "" {
				wantStatus = string(model.TaskStatusFailure)
			}
			require.Equal(t, wantStatus, result.Status)
			require.Equal(t, "100%", result.Progress)
			require.Equal(t, tt.wantReason, result.Reason)
		})
	}
}

func TestParseInteractionResultRejectsCompletedResponseWithoutAudio(t *testing.T) {
	result, err := parseInteractionResult([]byte(`{"id":"interaction-empty","status":"COMPLETED","outputs":[]}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	require.Equal(t, "completed_without_audio: Vertex returned COMPLETED without audio output", result.Reason)
	require.Equal(t, "100%", result.Progress)
}

func TestParseInteractionResultTreatsErrorEnvelopeAsFailure(t *testing.T) {
	body := []byte(`{"error":{"message":"Request contains an invalid argument.","code":"invalid_request"}}`)

	result, err := parseInteractionResult(body)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	require.Equal(t, "100%", result.Progress)
	require.Equal(t, "invalid_request: Request contains an invalid argument.", result.Reason)
}

func TestParseVertexHTTPFailureMapsOfficialVertexCodes(t *testing.T) {
	tests := map[int]string{
		400: "invalid_argument", 401: "unauthenticated", 403: "permission_denied",
		404: "not_found", 429: "resource_exhausted", 499: "cancelled",
		500: "internal", 503: "unavailable", 504: "deadline_exceeded",
	}
	for status, code := range tests {
		result := ParseVertexHTTPFailure(status, nil)
		if status == 499 {
			require.Equal(t, string(model.TaskStatusCancelled), result.Status)
		} else {
			require.Equal(t, model.TaskStatusFailure, result.Status)
		}
		require.Contains(t, result.Reason, code+": HTTP")
	}
}

func TestParseVertexHTTPFailurePrefersProviderErrorEnvelope(t *testing.T) {
	result := ParseVertexHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"bad prompt"}}`))
	require.Equal(t, "invalid_request: bad prompt", result.Reason)
}

func TestParseVertexHTTPFailureSupportsGoogleNumericErrorCode(t *testing.T) {
	result := ParseVertexHTTPFailure(400, []byte(`{"error":{"code":400,"message":"Request contains an invalid argument.","status":"INVALID_ARGUMENT"}}`))
	require.Equal(t, "invalid_argument: Request contains an invalid argument.", result.Reason)
}

func TestBuildLyriaRequestBodyUsesInteractionsInput(t *testing.T) {
	request := map[string]any{
		"model":  "lyria-3-pro-preview",
		"prompt": "A piano song",
		"metadata": map[string]any{
			"input":           []any{map[string]any{"type": "text", "text": "A piano song"}},
			"response_format": map[string]any{"type": "audio"},
			"background":      true,
		},
	}

	body, err := buildLyriaRequestBody(request)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Equal(t, "lyria-3-pro-preview", decoded["model"])
	require.Equal(t, "audio", decoded["response_format"].(map[string]any)["type"])
	require.Equal(t, true, decoded["background"])
	require.Len(t, decoded["input"], 1)
}

func TestBuildLyriaRequestBodyKeepsBackground(t *testing.T) {
	request := map[string]any{
		"model":  "lyria-3-pro-preview",
		"prompt": "A piano song",
		"metadata": map[string]any{
			"input":      "A piano song",
			"background": true,
		},
	}

	body, err := buildLyriaRequestBody(request)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Equal(t, true, decoded["background"])
}

func TestBuildLyriaRequestBodyForwardsStoreFalse(t *testing.T) {
	request := map[string]any{
		"model":  "lyria-3-pro-preview",
		"prompt": "A piano song",
		"metadata": map[string]any{
			"input": "A piano song",
			"store": false,
		},
	}

	body, err := buildLyriaRequestBody(request)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	require.Equal(t, false, decoded["store"])
}

func TestBuildLyriaPublicResponsePreservesCompletedOutputs(t *testing.T) {
	body := []byte(`{"id":"interaction-1","status":"completed","steps":[{"type":"model_output","content":[{"type":"audio","mime_type":"audio/mpeg","data":"SUQz"}]}]}`)

	public, err := buildLyriaPublicResponse(body, "lyria-3-pro-preview")
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(public, &decoded))
	require.Equal(t, "interaction-1", decoded["id"])
	require.Equal(t, "interaction", decoded["object"])
	require.Equal(t, "completed", decoded["status"])
	require.Len(t, decoded["steps"], 1)
}

func TestBuildLyriaPollURLUsesVertexInteractionResource(t *testing.T) {
	key := `{"project_id":"cxx15113062111-01"}`

	url, isVertex, err := buildLyriaPollURL(
		"https://aiplatform.googleapis.com/v1beta1/projects/cxx15113062111-01/locations/global/interactions",
		key,
		"pmKNar3tCsGFoLAP2JTGwQs",
		true,
	)

	require.NoError(t, err)
	require.True(t, isVertex)
	require.Equal(t,
		"https://aiplatform.googleapis.com/v1beta1/projects/cxx15113062111-01/locations/global/interactions/pmKNar3tCsGFoLAP2JTGwQs",
		url,
	)
}

func TestBuildVertexInteractionsURLDoesNotReuseGeminiDeveloperHost(t *testing.T) {
	key := `{"project_id":"cxx15113062111-01"}`

	url, err := buildVertexInteractionsURL("https://generativelanguage.googleapis.com", key)

	require.NoError(t, err)
	require.Equal(t,
		"https://aiplatform.googleapis.com/v1beta1/projects/cxx15113062111-01/locations/global/interactions",
		url,
	)
}
