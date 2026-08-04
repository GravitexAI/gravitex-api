package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedanceOfficialMirror_POST_RewritesPathAndSetsFlag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SeedanceOfficialMirror())
	router.POST("/api/v3/contents/generations/tasks", func(c *gin.Context) {
		assert.True(t, c.GetBool(common.KeySeedanceRawMirror))
		assert.Equal(t, "/v1/video/generations", c.Request.URL.Path)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v3/contents/generations/tasks", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSeedanceOfficialMirror_GET_RewritesPathAndStoresTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SeedanceOfficialMirror())
	router.GET("/api/v3/contents/generations/tasks/:id", func(c *gin.Context) {
		assert.True(t, c.GetBool(common.KeySeedanceRawMirror))
		assert.Equal(t, "task-abc123", c.GetString("task_id"))
		assert.Equal(t, "/v1/video/generations/task-abc123", c.Request.URL.Path)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/contents/generations/tasks/task-abc123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSeedanceOfficialMirror_DELETE_NoPathRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SeedanceOfficialMirror())
	router.DELETE("/api/v3/contents/generations/tasks/:id", func(c *gin.Context) {
		assert.True(t, c.GetBool(common.KeySeedanceRawMirror))
		assert.Equal(t, "/api/v3/contents/generations/tasks/task-abc123", c.Request.URL.Path)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v3/contents/generations/tasks/task-abc123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
