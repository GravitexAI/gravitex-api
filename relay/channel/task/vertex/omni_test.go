package vertex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	taskgemini "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVertexOmni11UsesGlobalInteractionsEndpointAndModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: relaycommon.GeminiOmni11FlashPreview,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: `{"project_id":"example-project"}`,
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/example-project/locations/global/interactions", url)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    relaycommon.GeminiOmni11FlashPreview,
		Prompt:   "make a video",
		Metadata: map[string]interface{}{"resolution": "1080p"},
	})
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Equal(t, relaycommon.GeminiOmni11FlashPreview, payload["model"])
	format := payload["response_format"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "1080p", format["resolution"])
}

func TestVertexOmniStreamParsesCompletedInteractionEvent(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: relaycommon.GeminiOmni11FlashPreview,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://aiplatform.googleapis.com",
			ApiKey:         "test-key",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("native_interactions_stream", true)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: interaction.completed\ndata: {\"interaction\":{\"id\":\"omni-1\",\"status\":\"completed\",\"steps\":[{\"type\":\"model_output\",\"content\":[{\"type\":\"video\",\"uri\":\"gs://bucket/video.mp4\"}]}]}}\n\n")),
	}

	taskID, taskData, taskErr := adaptor.DoResponse(c, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "omni:omni-1", taskID)
	require.Contains(t, string(taskData), "gs://bucket/video.mp4")
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestVertexOmniStreamRequestsSSE(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: relaycommon.GeminiOmni11FlashPreview,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "test-key",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_stream", true)
	req := httptest.NewRequest(http.MethodPost, "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/interactions", nil)

	err := adaptor.BuildRequestHeader(c, req, info)

	require.NoError(t, err)
	require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
}

func TestOmniStreamUsesFullInteractionFromCompletedPayload(t *testing.T) {
	calledFetch := false
	body := strings.NewReader("data: {\"event_type\":\"interaction.completed\",\"id\":\"omni-2\",\"status\":\"completed\",\"steps\":[]}\n\n")

	interaction, err := taskgemini.ReadOmniStream(body, nil, func(string) (*http.Response, error) {
		calledFetch = true
		return nil, nil
	})

	require.NoError(t, err)
	require.Contains(t, string(interaction), `"id":"omni-2"`)
	require.False(t, calledFetch)
}

func TestVertexOmniStreamFetchesInteractionWhenCompletionOnlyHasID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1beta/interactions/omni-3", r.URL.Path)
		require.Equal(t, "test-key", r.Header.Get("x-goog-api-key"))
		_, _ = w.Write([]byte(`{"id":"omni-3","status":"completed","steps":[]}`))
	}))
	defer server.Close()

	info := &relaycommon.RelayInfo{
		OriginModelName: relaycommon.GeminiOmni11FlashPreview,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "test-key",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("native_interactions_stream", true)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: interaction.created\ndata: {\"interaction_id\":\"omni-3\"}\n\nevent: interaction.completed\ndata: {\"interaction_id\":\"omni-3\"}\n\n")),
	}

	taskID, taskData, taskErr := adaptor.DoResponse(c, response, info)

	require.Nil(t, taskErr)
	require.Equal(t, "omni:omni-3", taskID)
	require.Contains(t, string(taskData), `"status":"completed"`)
}
