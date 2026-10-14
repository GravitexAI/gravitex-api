package vertex

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
