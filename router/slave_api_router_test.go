package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 集群（slave）节点不得暴露管理类 /api 接口：即使凭证被伪造，也必须在鉴权之前就 404。
func TestSlaveNodeBlocksAdminAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previous })

	engine := gin.New()
	require.NotPanics(t, func() { SetApiRouter(engine) })

	blocked := []struct{ method, path string }{
		{http.MethodGet, "/api/user/self"},
		{http.MethodGet, "/api/channel/"},
		{http.MethodPost, "/api/channel/1/status"},
		{http.MethodGet, "/api/log/"},
		{http.MethodGet, "/api/option/"},
		{http.MethodPost, "/api/user/login"},
	}
	for _, tc := range blocked {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			assert.Equal(t, http.StatusNotFound, recorder.Code)
		})
	}

	// 令牌用量查询仍放行：没带 Authorization 时由 TokenAuthReadOnly 返回 401，而不是被网关挡成 404。
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/usage/token/", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

// 集群节点必须继续提供 seedance 官方镜像接口：它们挂在独立路由组上，不受 /api 白名单影响。
func TestSlaveNodeKeepsSeedanceOfficialRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() { common.IsMasterNode = previous })

	engine := gin.New()
	SetApiRouter(engine)
	SetVideoRouter(engine)

	routes := []struct{ method, path string }{
		{http.MethodPost, "/api/v3/contents/generations/tasks"},
		{http.MethodGet, "/api/v3/contents/generations/tasks/task_x"},
		{http.MethodDelete, "/api/v3/contents/generations/tasks/task_x"},
		{http.MethodPost, "/api/v3/seedance"},
	}
	for _, tc := range routes {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
			// 没带令牌时应由 TokenAuth 返回 401；若被 /api 白名单误拦会是 404。
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
}
