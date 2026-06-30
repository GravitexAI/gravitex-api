package middleware

import (
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// API_ONLY_DOMAINS: 逗号分隔的 Host 列表（例如 "api.gravitex.ai,api.tennda.ai"）。
// 命中这些 Host 时，仅放行 API/回调路径，其它路径返回空 HTML，避免暴露内嵌前端。
// 未配置环境变量时使用 apiOnlyDomainsDefault 兜底，避免线上漏配。
const apiOnlyDomainsEnv = "API_ONLY_DOMAINS"

var apiOnlyDomainsDefault = []string{
	"api.gravitex.cn",
	"api.gravitex.ai",
	"api.tennda.ai",
}

var apiOnlyAllowedPrefixes = []string{
	"/api/",
	"/v1",
	"/mj",
	"/pg",
	"/asset-validate-callback.html",
}

const apiOnlyBlankHTML = `<!doctype html><html><head><meta charset="utf-8"><title></title></head><body></body></html>`

var (
	apiOnlyDomains     map[string]struct{}
	apiOnlyDomainsOnce sync.Once
)

func loadAPIOnlyDomains() {
	raw := os.Getenv(apiOnlyDomainsEnv)
	var entries []string
	if raw == "" {
		entries = apiOnlyDomainsDefault
	} else {
		entries = strings.Split(raw, ",")
	}
	domains := make(map[string]struct{}, len(entries))
	for _, d := range entries {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" {
			domains[d] = struct{}{}
		}
	}
	if len(domains) > 0 {
		apiOnlyDomains = domains
	}
}

// RestrictAPIDomains returns a Gin middleware that hides the embedded frontend
// from API-only Hosts. Non-API requests on those Hosts get a blank HTML page.
// When API_ONLY_DOMAINS is empty the middleware is a no-op.
func RestrictAPIDomains() gin.HandlerFunc {
	apiOnlyDomainsOnce.Do(loadAPIOnlyDomains)
	return func(c *gin.Context) {
		if len(apiOnlyDomains) == 0 {
			c.Next()
			return
		}
		host := c.Request.Host
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		host = strings.ToLower(host)
		if _, restricted := apiOnlyDomains[host]; !restricted {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		for _, p := range apiOnlyAllowedPrefixes {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.String(http.StatusOK, apiOnlyBlankHTML)
		c.Abort()
	}
}
