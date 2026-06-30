package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withAPIOnlyDomains forces a deterministic domain set for the test and restores
// the previous one on cleanup. It bypasses sync.Once so each test can pick its
// own configuration.
func withAPIOnlyDomains(t *testing.T, hosts ...string) {
	t.Helper()
	prev := apiOnlyDomains
	apiOnlyDomainsOnce.Do(func() {})

	if len(hosts) == 0 {
		apiOnlyDomains = nil
	} else {
		apiOnlyDomains = make(map[string]struct{}, len(hosts))
		for _, h := range hosts {
			apiOnlyDomains[h] = struct{}{}
		}
	}
	t.Cleanup(func() { apiOnlyDomains = prev })
}

func runRestrictRequest(t *testing.T, host, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RestrictAPIDomains())
	r.GET("/*any", func(c *gin.Context) {
		c.String(http.StatusOK, "backend-reached")
	})

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRestrictAPIDomains_BlocksFrontendPaths(t *testing.T) {
	withAPIOnlyDomains(t, "api.gravitex.ai", "api.tennda.ai")

	cases := []struct {
		host string
		path string
	}{
		{"api.gravitex.ai", "/"},
		{"api.gravitex.ai", "/dashboard"},
		{"api.gravitex.ai", "/assets/index.js"},
		{"api.tennda.ai", "/login"},
		{"API.gravitex.ai", "/"},          // case-insensitive
		{"api.gravitex.ai:8080", "/"},     // port stripped
	}
	for _, tc := range cases {
		w := runRestrictRequest(t, tc.host, tc.path)
		require.Equalf(t, http.StatusOK, w.Code, "host=%s path=%s", tc.host, tc.path)
		assert.Containsf(t, w.Body.String(), "<!doctype html>", "host=%s path=%s", tc.host, tc.path)
		assert.NotContainsf(t, w.Body.String(), "backend-reached", "host=%s path=%s should not reach backend", tc.host, tc.path)
		assert.Equalf(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"), "host=%s path=%s", tc.host, tc.path)
	}
}

func TestRestrictAPIDomains_AllowsAPIPaths(t *testing.T) {
	withAPIOnlyDomains(t, "api.gravitex.ai")

	apiPaths := []string{
		"/api/user/self",
		"/v1/chat/completions",
		"/v1beta/models",
		"/mj/submit/imagine",
		"/pg/something",
		"/asset-validate-callback.html",
		"/asset-validate-callback.html?state=abc",
	}
	for _, path := range apiPaths {
		w := runRestrictRequest(t, "api.gravitex.ai", path)
		require.Equalf(t, http.StatusOK, w.Code, "path=%s", path)
		assert.Equalf(t, "backend-reached", w.Body.String(), "path=%s should reach backend", path)
	}
}

func TestRestrictAPIDomains_UnrestrictedHostUnaffected(t *testing.T) {
	withAPIOnlyDomains(t, "api.gravitex.ai")

	w := runRestrictRequest(t, "console.gravitex.ai", "/")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "backend-reached", w.Body.String())
}

func TestRestrictAPIDomains_NoEnvIsNoOp(t *testing.T) {
	withAPIOnlyDomains(t) // empty set

	w := runRestrictRequest(t, "api.gravitex.ai", "/")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "backend-reached", w.Body.String())
}

func TestRestrictAPIDomains_DefaultListAppliesWhenEnvAbsent(t *testing.T) {
	t.Setenv(apiOnlyDomainsEnv, "")
	prev := apiOnlyDomains
	apiOnlyDomains = nil
	loadAPIOnlyDomains()
	defer func() { apiOnlyDomains = prev }()

	for _, host := range apiOnlyDomainsDefault {
		_, ok := apiOnlyDomains[host]
		assert.Truef(t, ok, "default host %s should be active when env is empty", host)
	}
}
