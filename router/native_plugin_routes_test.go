package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeAndPluginRoutesCoexist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() {
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetVideoRouter(engine)
		SetTaskRouter(engine)
	})
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"POST /v1/responses",
		"POST /v1/videos",
		"GET /v1/videos/:task_id",
		"GET /v1/videos/:task_id/content",
		"POST /v1/video/generations",
		"POST /v1beta/models/*path",
		"POST /v1beta1/models/*path",
		"POST /v1beta/interactions",
		"POST /v1beta1/projects/:project/locations/:location/interactions",
		"POST /suno/submit/:action",
		"POST /kling/v1/videos/text2video",
		"POST /jimeng/",
		"POST /api/v3/contents/generations/tasks",
		"DELETE /api/v3/contents/generations/tasks/:id",
	} {
		assert.True(t, routes[route], "missing route: %s", route)
	}
}
