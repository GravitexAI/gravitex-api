package controller

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// assetValidateCallbackHTML is the gateway-hosted landing page that BytePlus redirects
// the end user to after the H5 liveness verification flow. The page extracts URL
// query parameters (state, bytedToken, resultCode), POSTs to /v1/visual-validate/result
// to materialize the new asset group, and notifies the parent window via postMessage.
//
//go:embed asset_validate_callback.html
var assetValidateCallbackHTML []byte

// ServeVisualValidateCallbackPage serves the embedded callback HTML. It is registered
// without TokenAuth — authority comes from the HMAC-signed `state` query parameter,
// which the page forwards to /v1/visual-validate/result.
func ServeVisualValidateCallbackPage(c *gin.Context) {
	// Cache for a short time so reloads after BytePlus redirect remain snappy, but never
	// allow long-lived caching that could outlast a `state` token's TTL.
	c.Header("Cache-Control", "private, max-age=60")
	c.Data(http.StatusOK, "text/html; charset=utf-8", assetValidateCallbackHTML)
}
