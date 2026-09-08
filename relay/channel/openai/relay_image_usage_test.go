package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenaiImageHandlerStoresImageTokenUsage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"created":1788771766,"data":[{"url":"https://example.com/image.jpeg"}],"usage":{"input_images":1,"generated_images":1,"output_tokens":4096,"total_tokens":4096}}`)),
	}

	_, apiErr := OpenaiImageHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, info.UpstreamResponses)
	require.NotNil(t, info.UpstreamResponses["usage"])
}

func TestOpenaiImageHandlerPreservesTargetImageRawUsageBytes(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: "dola-seedream-5-0-pro-260628",
		RelayMode:       relayconstant.RelayModeImagesGenerations,
	}
	rawUsage := `{"completion_tokens":16384, "generated_images":1, "input_images":2, "output_tokens":16384}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"created":1788771766,"data":[{"url":"https://example.com/image.jpeg"}],"usage":` + rawUsage + `}`)),
	}

	_, apiErr := OpenaiImageHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.Equal(t, []byte(rawUsage), info.RawUpstreamUsage)
}
