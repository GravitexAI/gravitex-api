package jsplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RelayTaskSubmit calls the adaptor in this order: ValidateRequestAndSetAction
// (step 1) -> ModelMappedHelper (step 2.5) -> BuildRequestBody (step 8). The
// jsplugin adaptor builds and caches the upstream submit descriptor during
// ValidateRequestAndSetAction, so a channel-type plugin must still honor the
// channel model_mapping applied afterwards. Regression test for doubao video
// channels (channelTypes 54/45) hijacked from the native taskdoubao adaptor,
// which applied info.UpstreamModelName in BuildRequestBody.
func TestDoubaoPluginAppliesChannelModelMappingInProductionOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source, err := builtinplugins.Source("doubao")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{Key: "doubao"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://ark.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "client-model",
	}
	info.UpstreamModelName = "client-model"
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"client-model","prompt":"a cat","seconds":"5"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("model_mapping", `{"client-model":"ep-2026xxxx-video"}`)

	// Step 1 of RelayTaskSubmit: validate (builds and caches the submit descriptor).
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	// Step 2.5 of RelayTaskSubmit: apply the channel model_mapping.
	require.NoError(t, helper.ModelMappedHelper(c, info, nil))
	require.Equal(t, "ep-2026xxxx-video", info.UpstreamModelName)
	// Step 8 of RelayTaskSubmit: build the upstream request body.
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(raw, &decoded))
	assert.Equal(t, "ep-2026xxxx-video", decoded["model"], "channel model_mapping must be applied to the upstream body")
}
