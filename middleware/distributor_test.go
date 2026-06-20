package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newModelLimitedDistributorRouter(t *testing.T) *gin.Engine {
	t.Helper()
	require.NoError(t, appI18n.Init())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{
			"seedance-2-0-NSFW": true,
		})
	})
	return router
}

func TestDistributeVideoFetchSkipsTokenModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	paths := []string{
		"/v1/video/generations/task-123",
		"/v1/videos/task-123",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			router := newModelLimitedDistributorRouter(t)
			router.GET(path, Distribute(), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestDistributeVideoSubmitStillEnforcesTokenModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newModelLimitedDistributorRouter(t)
	router.POST("/v1/video/generations", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(`{"model":"forbidden-model"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestDistributeVideoRemixStillEnforcesTokenModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newModelLimitedDistributorRouter(t)
	router.POST("/v1/videos/:video_id/remix", Distribute(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/videos/video-123/remix", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}
