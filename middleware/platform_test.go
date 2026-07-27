package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformContextResolvesIGAHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(PlatformContext())
	router.GET("/", func(c *gin.Context) {
		c.String(200, "%d", RequestPlatformID(c))
	})

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "gravitex", header: "1", want: "1"},
		{name: "tennda", header: " 2 ", want: "2"},
		{name: "missing defaults to gravitex", want: "1"},
		{name: "invalid defaults to gravitex", header: "invalid", want: "1"},
		{name: "zero defaults to gravitex", header: "0", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.header != "" {
				req.Header.Set(platformHeader, tt.header)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			require.Equal(t, 200, res.Code)
			assert.Equal(t, tt.want, res.Body.String())
		})
	}
}
