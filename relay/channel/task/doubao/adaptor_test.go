package doubao

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeRawMirrorBillingFields_TextOnly(t *testing.T) {
	raw := []byte(`{"model":"seedance-2-0","content":[{"type":"text","text":"a cat"}],"resolution":"720p"}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.False(t, hasVideo)
	assert.Equal(t, "720p", resolution)
}

func TestProbeRawMirrorBillingFields_WithVideoInput(t *testing.T) {
	raw := []byte(`{
		"model":"seedance-2-0",
		"content":[
			{"type":"text","text":"dance like this"},
			{"type":"video_url","video_url":{"url":"https://example.com/ref.mp4"},"role":"reference_video"}
		]
	}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.True(t, hasVideo)
	assert.Equal(t, "", resolution)
}

func TestProbeRawMirrorBillingFields_UnknownTopLevelFieldsPreserved(t *testing.T) {
	// execution_expires_after/service_tier/safety_identifier are official fields
	// TaskSubmitReq would silently drop; probing must not choke on them.
	raw := []byte(`{
		"model":"seedance-2-0",
		"content":[{"type":"text","text":"x"}],
		"execution_expires_after": 172800,
		"service_tier": "default",
		"safety_identifier": "user-hash-abc"
	}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.False(t, hasVideo)
	assert.Equal(t, "", resolution)
}

func TestExtractAssetVirtualIdsFromRaw(t *testing.T) {
	raw := []byte(`{
		"content":[
			{"type":"text","text":"x"},
			{"type":"image_url","image_url":{"url":"asset://asset-img-1"},"role":"reference_image"},
			{"type":"video_url","video_url":{"url":"asset://asset-vid-1"},"role":"reference_video"},
			{"type":"audio_url","audio_url":{"url":"https://example.com/plain.mp3"},"role":"reference_audio"}
		]
	}`)
	ids := extractAssetVirtualIdsFromRaw(raw)
	assert.ElementsMatch(t, []string{"asset-img-1", "asset-vid-1"}, ids)
}

func TestExtractAssetVirtualIdsFromRaw_NoAssets(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"plain text only"}]}`)
	ids := extractAssetVirtualIdsFromRaw(raw)
	assert.Empty(t, ids)
}

func TestApplyUpstreamModelName_RewritesModelOnly(t *testing.T) {
	raw := []byte(`{"model":"seedance-2-0-fast","content":[{"type":"text","text":"a cat"}],"resolution":"720p","execution_expires_after":172800}`)
	rewritten, err := applyUpstreamModelName(raw, "doubao-seedance-2-0-fast-260128")
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(rewritten, &got))
	assert.Equal(t, "doubao-seedance-2-0-fast-260128", got["model"])
	assert.Equal(t, "720p", got["resolution"])
	assert.EqualValues(t, 172800, got["execution_expires_after"])
	assert.Len(t, got["content"], 1)
}

func TestBuildRawMirrorRequestBody_AppliesChannelModelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"model":"seedance-2-0-fast","content":[{"type":"text","text":"a cat"}]}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(string(raw)))
	c.Set(common.KeyRequestBody, raw)

	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "seedance-2-0-fast",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "doubao-seedance-2-0-fast-260128"},
	}

	adaptor := &TaskAdaptor{}
	reader, err := adaptor.buildRawMirrorRequestBody(c, info)
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &got))
	assert.Equal(t, "doubao-seedance-2-0-fast-260128", got["model"])
}

func TestBuildRawMirrorRequestBody_NoMappingKeepsOriginalModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"model":"seedance-2-0","content":[{"type":"text","text":"a cat"}]}`)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", strings.NewReader(string(raw)))
	c.Set(common.KeyRequestBody, raw)

	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "seedance-2-0",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "seedance-2-0"},
	}

	adaptor := &TaskAdaptor{}
	reader, err := adaptor.buildRawMirrorRequestBody(c, info)
	require.NoError(t, err)

	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(body))
}

func TestDoResponse_RawMirror_WritesUpstreamBodyVerbatim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := `{"id":"cgt-20260708094649-mxfjc","model":"seedance-2-0","status":"queued","created_at":1783475210,"updated_at":1783475210}`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(common.KeySeedanceRawMirror, true)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
	}

	adaptor := &TaskAdaptor{}
	taskID, taskData, taskErr := adaptor.DoResponse(c, resp, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}})

	require.Nil(t, taskErr)
	assert.Equal(t, "cgt-20260708094649-mxfjc", taskID)
	assert.JSONEq(t, upstreamBody, string(taskData))
	assert.JSONEq(t, upstreamBody, w.Body.String())
}

func TestCancelTask_SendsDeleteWithBearerAuth(t *testing.T) {
	service.InitHttpClient()

	var gotMethod, gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	resp, err := adaptor.CancelTask(server.URL, "test-api-key", "cgt-20260708094649-mxfjc", "")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/api/v3/contents/generations/tasks/cgt-20260708094649-mxfjc", gotPath)
	assert.Equal(t, "Bearer test-api-key", gotAuth)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
