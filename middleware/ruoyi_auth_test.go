package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const ruoYiTestSecret = "ruoyi-test-secret-for-unit-test"

// setupRuoYiAuthTest 初始化内存 DB、RuoYi 鉴权开关与一个管理员用户（模拟 supplier-channel-lite-test 账号）。
func setupRuoYiAuthTest(t *testing.T) string {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB, originalLogDB := model.DB, model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}))

	originalEnabled, originalSecret, originalIsolation := common.RuoYiAuthEnabled, common.RuoYiJWTSecret, common.PlatformIsolationEnabled
	common.RuoYiAuthEnabled = true
	common.RuoYiJWTSecret = ruoYiTestSecret
	common.PlatformIsolationEnabled = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.RuoYiAuthEnabled, common.RuoYiJWTSecret, common.PlatformIsolationEnabled = originalEnabled, originalSecret, originalIsolation
	})

	username := "supplier-channel-lite-test"
	require.NoError(t, db.Create(&model.User{
		Username: username,
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
	return username
}

// signRuoYiToken 按 sa-token-jwt 的 payload 结构签发测试令牌；extra 中的字段会覆盖/追加到 claims。
func signRuoYiToken(t *testing.T, username string, extra jwt.MapClaims) string {
	t.Helper()
	claims := jwt.MapClaims{
		"loginId":  "sys_user:1",
		"userName": username,
		"rnStr":    "random",
	}
	for k, v := range extra {
		claims[k] = v
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(ruoYiTestSecret))
	require.NoError(t, err)
	return token
}

// newRuoYiProbeEngine 按生产路由结构注册 /api/channel 下的路由，处理器直接调用 tryRuoYiJWTAuth。
func newRuoYiProbeEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	probe := func(c *gin.Context) {
		user, err := tryRuoYiJWTAuth(c)
		if err != nil || user == nil {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusOK)
	}
	channel := r.Group("/api").Group("/channel")
	channel.GET("/test/:id", probe)
	channel.GET("/:id", probe)
	channel.POST("/copy/:id", probe)
	return r
}

func doRuoYiRequest(r http.Handler, method, path, token string) int {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestTryRuoYiJWTAuth_Eff(t *testing.T) {
	username := setupRuoYiAuthTest(t)
	r := newRuoYiProbeEngine()
	now := time.Now().UnixMilli()

	cases := []struct {
		name  string
		extra jwt.MapClaims
		want  int
	}{
		{name: "无 eff（普通用户令牌）放行", extra: nil, want: http.StatusOK},
		{name: "eff=-1 永不过期放行", extra: jwt.MapClaims{"eff": -1}, want: http.StatusOK},
		{name: "eff 未过期放行", extra: jwt.MapClaims{"eff": now + 60_000}, want: http.StatusOK},
		{name: "eff 已过期拒绝", extra: jwt.MapClaims{"eff": now - 1_000}, want: http.StatusUnauthorized},
		{name: "eff 类型非法拒绝", extra: jwt.MapClaims{"eff": "never"}, want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := signRuoYiToken(t, username, tc.extra)
			assert.Equal(t, tc.want, doRuoYiRequest(r, http.MethodGet, "/api/channel/12", token))
		})
	}
}

func TestTryRuoYiJWTAuth_ChannelTestServiceTokenRouteLock(t *testing.T) {
	username := setupRuoYiAuthTest(t)
	r := newRuoYiProbeEngine()
	serviceToken := signRuoYiToken(t, username, jwt.MapClaims{
		"loginType": ruoYiChannelTestServiceLoginType,
		"device":    "service",
		"eff":       time.Now().UnixMilli() + 60_000,
	})

	assert.Equal(t, http.StatusOK, doRuoYiRequest(r, http.MethodGet, "/api/channel/test/42", serviceToken))
	assert.Equal(t, http.StatusUnauthorized, doRuoYiRequest(r, http.MethodGet, "/api/channel/42", serviceToken))
	assert.Equal(t, http.StatusUnauthorized, doRuoYiRequest(r, http.MethodPost, "/api/channel/copy/42", serviceToken))

	// 普通 loginType 的令牌不受路由限制
	normalToken := signRuoYiToken(t, username, jwt.MapClaims{"loginType": "login"})
	assert.Equal(t, http.StatusOK, doRuoYiRequest(r, http.MethodGet, "/api/channel/42", normalToken))
}

// TestAdminAuth_ChannelTestServiceTokenRejectedWithoutFallback 确认经 AdminAuth 链路时，
// 服务令牌越权访问直接 401，且不会回退到系统管理令牌等其他鉴权方式放行。
func TestAdminAuth_ChannelTestServiceTokenRejectedWithoutFallback(t *testing.T) {
	username := setupRuoYiAuthTest(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("session-secret"))))
	r.GET("/api/channel/:id", AdminAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })

	serviceToken := signRuoYiToken(t, username, jwt.MapClaims{
		"loginType": ruoYiChannelTestServiceLoginType,
		"eff":       time.Now().UnixMilli() + 60_000,
	})
	require.Equal(t, http.StatusUnauthorized, doRuoYiRequest(r, http.MethodGet, "/api/channel/42", serviceToken))

	expiredToken := signRuoYiToken(t, username, jwt.MapClaims{"eff": time.Now().UnixMilli() - 1_000})
	require.Equal(t, http.StatusUnauthorized, doRuoYiRequest(r, http.MethodGet, "/api/channel/42", expiredToken))
}
