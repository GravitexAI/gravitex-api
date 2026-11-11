package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// upstreamHeaderAllowlist lists the only upstream response headers forwarded to
// clients. Most channels are third-party relays that attach their own branding,
// rate-limit and tracing headers, so everything else is dropped to avoid
// leaking supplier identity to customers.
var upstreamHeaderAllowlist = map[string]struct{}{
	"Content-Type": {},
	// Codex clients rely on these SSE headers.
	"X-Reasoning-Included": {},
	"X-Codex-Turn-State":   {},
}

// ShouldCopyUpstreamHeader checks whether a given upstream response header
// should be copied to the client response. Only headers in
// upstreamHeaderAllowlist are copied. When the upstream header is
// X-Api-Request-Id, the value is captured into the Gin context for later
// logging, unless the relay already captured one from the channel-configured
// upstream request ID header.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	if strings.EqualFold(k, common.RequestIdKey) {
		if c != nil && len(v) > 0 && c.GetString(common.UpstreamRequestIdKey) == "" {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}
	_, ok := upstreamHeaderAllowlist[http.CanonicalHeaderKey(k)]
	return ok
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
