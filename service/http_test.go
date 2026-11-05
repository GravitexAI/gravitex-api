package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIOCopyBytesGracefullyDropsSupplierHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Header(common.RequestIdKey, "local-id")

	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                           {"application/json"},
			"Content-Length":                         {"999"},
			common.RequestIdKey:                      {"upstream-id"},
			"X-Oneapi-Request-Id":                    {"relay-id"},
			"X-New-Api-Version":                      {"v0.9.0"},
			"Anthropic-Ratelimit-Requests-Remaining": {"42"},
			"Openai-Organization":                    {"org-xxx"},
			"Set-Cookie":                             {"__cf_bm=abc"},
			"Server":                                 {"cloudflare"},
			"Content-Encoding":                       {"gzip"},
		},
	}
	body := []byte(`{"ok":true}`)

	IOCopyBytesGracefully(c, upstream, body)

	got := recorder.Header()
	assert.Equal(t, "application/json", got.Get("Content-Type"))
	assert.Equal(t, "11", got.Get("Content-Length"))
	assert.Equal(t, "local-id", got.Get(common.RequestIdKey))
	for _, name := range []string{
		"X-Oneapi-Request-Id",
		"X-New-Api-Version",
		"Anthropic-Ratelimit-Requests-Remaining",
		"Openai-Organization",
		"Set-Cookie",
		"Server",
		"Content-Encoding",
	} {
		assert.Empty(t, got.Values(name), name)
	}
	assert.Equal(t, "upstream-id", c.GetString(common.UpstreamRequestIdKey))
	require.Equal(t, string(body), recorder.Body.String())
}

func TestShouldCopyUpstreamHeaderKeepsCodexHeaders(t *testing.T) {
	assert.True(t, ShouldCopyUpstreamHeader(nil, "x-codex-turn-state", []string{"s"}))
	assert.True(t, ShouldCopyUpstreamHeader(nil, "X-Reasoning-Included", []string{"true"}))
	assert.False(t, ShouldCopyUpstreamHeader(nil, "X-Codex-Primary-Used-Percent", []string{"10"}))
}
