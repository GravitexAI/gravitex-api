package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v1beta 是 Gemini Developer API 的 preview 版本名，v1beta1 是 Vertex AI 对同一 preview
// 面的叫法。两种写法都必须能进到 relay 链路，否则用 Vertex 风格 URL 的客户端会在网关层拿 404。
// 同时 /v1beta1/projects/.../interactions（视频路由）必须继续可达 —— 两者都挂在 /v1beta1 下，
// 注册顺序或通配符写错会让 gin 在启动时 panic。
func TestGeminiRelayAcceptsBothPreviewVersionPaths(t *testing.T) {
	setupRelayRouterTestDB(t)

	engine := gin.New()
	require.NotPanics(t, func() {
		SetRelayRouter(engine)
		SetVideoRouter(engine)
	})

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	assert.True(t, routes["POST /v1beta/models/*path"], "native gemini route missing")
	assert.True(t, routes["POST /v1beta1/models/*path"], "vertex-style gemini alias missing")
	assert.True(t, routes["POST /v1beta1/projects/:project/locations/:location/interactions"],
		"vertex interactions route must survive alongside the alias")

	for _, path := range []string{
		"/v1beta/models/gemini-3.5-flash:generateContent",
		"/v1beta1/models/gemini-3.5-flash:generateContent",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"contents":[]}`))
			request.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(recorder, request)

			// 无凭证请求会被 TokenAuth 拒掉，这里只断言路由命中（不是 404）。
			assert.NotEqual(t, http.StatusNotFound, recorder.Code)
		})
	}
}
