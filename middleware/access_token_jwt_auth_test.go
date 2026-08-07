package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAccessTokenJWTAuthPassesWithRuoYiEnabled 回归守护：RuoYi 鉴权启用时（生产常态，接了
// Java RuoYi 后端），default 前端携带 access_token 的请求必须通过 UserAuth，而不能被 RuoYi 层
// 抢先用 RuoYiJWTSecret 验签失败而直接 401。故 access_token JWT 层必须排在 RuoYi 层之前。
func TestAccessTokenJWTAuthPassesWithRuoYiEnabled(t *testing.T) {
	// --- 隔离 DB ---
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.LoginSession{}))

	// 固定 SessionSecret，模拟单实例签发=验证同密钥
	common.SessionSecret = "repro-fixed-secret-123456"
	// 复现真实环境：RuoYi 鉴权已启用，且其密钥与 SessionSecret 不同。
	// 修复前 RuoYi 层会抢先用 RuoYiJWTSecret 验签本体系 access_token 失败并直接 401；
	// 修复后 access_token JWT 层排在 RuoYi 之前，本体系令牌得以正确判定。
	common.RuoYiAuthEnabled = true
	common.RuoYiJWTSecret = "a-different-ruoyi-secret-key"
	defer func() { common.RuoYiAuthEnabled = false }()

	// --- 造用户 + 登录会话 ---
	u := &model.User{
		Username: "repro_user",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(u).Error)

	ls, err := model.CreateLoginSession(u.Id, "login", "127.0.0.1", "test-agent")
	require.NoError(t, err)

	token, expiresAt, err := service.IssueAccessToken(u.Id, ls.Sid)
	require.NoError(t, err)
	require.Greater(t, expiresAt, int64(0))
	t.Logf("issued access_token=%s... sid=%s uid=%d", token[:12], ls.Sid, u.Id)

	// 直接验证解析回路
	pUid, pSid, perr := service.ParseAccessToken(token)
	require.NoError(t, perr, "ParseAccessToken should succeed")
	require.Equal(t, u.Id, pUid)
	require.Equal(t, ls.Sid, pSid)

	// 会话应查得到
	got, gerr := model.GetActiveLoginSession(ls.Sid)
	require.NoError(t, gerr)
	require.NotNil(t, got, "login session must be active")

	// --- 起 gin 引擎 挂 UserAuth ---
	gin.SetMode(gin.TestMode)
	r := gin.New()
	store := cookie.NewStore([]byte(common.SessionSecret))
	r.Use(sessions.Sessions("session", store))
	r.GET("/api/user/models", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	// 模拟 setupLogin 写入的 cookie 会话（default 前端登录后浏览器会自动携带）。
	r.GET("/__setcookie", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("username", u.Username)
		s.Set("role", u.Role)
		s.Set("id", u.Id)
		s.Set("status", u.Status)
		s.Set("group", u.Group)
		s.Set("sid", ls.Sid)
		require.NoError(t, s.Save())
		c.String(http.StatusOK, "ok")
	})

	// 场景一：只带 Authorization: Bearer（无 cookie，模拟跨 pod / cookie 缺失）。
	req := httptest.NewRequest(http.MethodGet, "/api/user/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	t.Logf("[bearer only] status=%d body=%s", w.Code, w.Body.String())
	require.Equal(t, http.StatusOK, w.Code, "纯 access_token 请求应通过 UserAuth")

	// 取一枚有效的登录 cookie。
	cw := httptest.NewRecorder()
	r.ServeHTTP(cw, httptest.NewRequest(http.MethodGet, "/__setcookie", nil))
	loginCookie := cw.Result().Cookies()
	require.NotEmpty(t, loginCookie, "should get a session cookie")

	// 场景二（回归核心）：cookie + Bearer 且不带 New-Api-User 头——default 前端的真实请求形态。
	// 修复前 cookie 层抢先接管并要求 New-Api-User 头 → 401；修复后 access_token 层优先 → 200。
	req2 := httptest.NewRequest(http.MethodGet, "/api/user/models", nil)
	for _, ck := range loginCookie {
		req2.AddCookie(ck)
	}
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	t.Logf("[cookie+bearer, no New-Api-User] status=%d body=%s", w2.Code, w2.Body.String())
	require.Equal(t, http.StatusOK, w2.Code,
		"default 前端(带 cookie+access_token、无 New-Api-User)应通过 UserAuth")
}
