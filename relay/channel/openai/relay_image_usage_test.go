package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
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

// svg_usage is captured for billing; reasoning reported only through
// total_tokens is counted as authoring output.
func TestOpenaiImageHandlerCapturesSvgAuthoringUsage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{"created":1,"data":[{"b64_json":"PHN2Zy8+"}],"output_format":"svg",` +
			`"usage":{"input_tokens":10,"output_tokens":1000,"total_tokens":1010},` +
			`"svg_usage":{"model":"svg-author","input_tokens":300,"output_tokens":200,"reasoning_tokens":150,"total_tokens":650}}`)),
	}

	_, apiErr := OpenaiImageHandler(c, info, resp)

	require.Nil(t, apiErr)
	value, exists := c.Get(service.SvgAuthoringUsageContextKey)
	require.True(t, exists)
	require.Equal(t, service.SvgAuthoringUsage{Model: "svg-author", InputTokens: 300, OutputTokens: 350}, value)
}
