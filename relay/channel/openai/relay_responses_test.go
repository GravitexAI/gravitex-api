package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlerMapsInputTokenDetails(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{
		"id": "resp_test",
		"usage": {
			"input_tokens": 162,
			"output_tokens": 1065,
			"total_tokens": 1227,
			"input_tokens_details": {
				"cached_tokens": 3,
				"cache_write_tokens": 7,
				"text_tokens": 63,
				"audio_tokens": 96,
				"image_tokens": 0
			}
		}
	}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 162, usage.PromptTokens)
	assert.Equal(t, 1065, usage.CompletionTokens)
	assert.Equal(t, 1227, usage.TotalTokens)
	assert.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 7, usage.PromptTokensDetails.CacheWriteTokens)
	assert.Equal(t, 63, usage.PromptTokensDetails.TextTokens)
	assert.Equal(t, 96, usage.PromptTokensDetails.AudioTokens)
	assert.Equal(t, 0, usage.PromptTokensDetails.ImageTokens)
	assert.Equal(t, "resp_test", info.UpstreamResponseId)
}
