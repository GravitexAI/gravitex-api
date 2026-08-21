package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

// errNotAccessJWT 表示 Authorization 头不是本体系（default 前端）的 access_token JWT，
// 调用方应回退到旧的 access_token（char32 系统管理令牌）校验。
var errNotAccessJWT = errors.New("not an access-token jwt")

// tryAccessTokenJWTAuth 尝试把 Authorization Bearer 当作 default 前端的 access_token JWT 校验。
//
// 返回值语义：
//   - (user, "", nil)          校验通过，user 为对应用户，第二个返回值是所属登录会话 sid；
//   - (nil, "", errNotAccessJWT) 非本体系 JWT（含 RuoYi/系统管理令牌），调用方回退旧方案；
//   - (nil, "", 其他 err)       是本体系 JWT 但会话失效/用户异常，应直接拒绝。
func tryAccessTokenJWTAuth(c *gin.Context) (*model.User, string, error) {
	authHeader := c.Request.Header.Get("Authorization")
	if authHeader == "" {
		return nil, "", errNotAccessJWT
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	// 本体系 access_token 是三段 HS256 JWT；系统管理令牌是 char(32) 定长串，不含点。
	if strings.Count(token, ".") != 2 {
		return nil, "", errNotAccessJWT
	}
	uid, sid, err := service.ParseAccessToken(token)
	if err != nil {
		// 验签/subject 不符：可能是 RuoYi JWT 或其他，交回退方案处理。
		return nil, "", errNotAccessJWT
	}
	loginSession, err := model.GetActiveLoginSession(sid)
	if err != nil {
		return nil, "", err
	}
	if loginSession == nil || loginSession.UserId != uid {
		// JWT 合法但会话已被撤销/过期/不匹配：明确拒绝，不再回退。
		return nil, "", fmt.Errorf("access token session revoked or expired")
	}
	user, err := model.GetUserById(uid, false)
	if err != nil {
		return nil, "", err
	}
	if user == nil || user.Username == "" {
		return nil, "", fmt.Errorf("access token user not found")
	}
	return user, sid, nil
}

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	var username, role, id, status, group interface{}
	useAccessToken := false
	fromRuoYi := false
	fromAccessJWT := false
	jwtSid := ""
	// access_token JWT 必须最优先判定——先于 cookie 会话、也先于 RuoYi：
	//  1) default 前端登录后浏览器同源会自动带上 setupLogin 写入的 cookie；若 cookie 层抢先接管，
	//     而 default 前端并不发送 New-Api-User 头，就会误撞该头校验直接 401（登录后接口全 401、无限刷新）。
	//  2) 它用 SessionSecret 签名并带唯一 subject，RuoYi 层会把任意三段 JWT 当成自己的、验签失败即拒绝。
	// classic 的 cookie 请求 / char32 系统管理令牌不是本体系 JWT，会返回 errNotAccessJWT 正常回退。
	if jwtUser, sid, jwtErr := tryAccessTokenJWTAuth(c); jwtErr == nil {
		if !validUserInfo(jwtUser.Username, jwtUser.Role) {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
			})
			c.Abort()
			return
		}
		username = jwtUser.Username
		role = jwtUser.Role
		id = jwtUser.Id
		status = jwtUser.Status
		group = jwtUser.Group
		jwtSid = sid
		fromAccessJWT = true
	} else if !errors.Is(jwtErr, errNotAccessJWT) {
		// 是本体系 JWT 但会话失效/用户异常：直接拒绝，不回退。
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
		})
		c.Abort()
		return
	}
	if !fromAccessJWT {
		// 非 access_token 请求（classic cookie / RuoYi / char32 系统令牌）走原有会话与回退链路。
		username = session.Get("username")
		role = session.Get("role")
		id = session.Get("id")
		status = session.Get("status")
		group = session.Get("group")
	}
	if username == nil {
		// 尝试 RuoYi JWT 鉴权
		if common.RuoYiAuthEnabled {
			user, err := tryRuoYiJWTAuth(c)
			if err == nil && user != nil {
				if !validUserInfo(user.Username, user.Role) {
					c.JSON(http.StatusOK, gin.H{
						"success": false,
						"message": "Unauthorized, invalid user info",
					})
					c.Abort()
					return
				}
				username = user.Username
				role = user.Role
				id = user.Id
				status = user.Status
				group = user.Group
				fromRuoYi = true
			} else if err != nil && err != errNoRuoYiJWT {
				// JWT 格式正确但验签/解析失败 -> 直接拒绝
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "Unauthorized, invalid token",
				})
				c.Abort()
				return
			}
		}
	}
	if username == nil {
		// 回退到原有的 access token 方案
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
			})
			c.Abort()
			return
		}
		// 系统管理令牌（char32）校验：access_token JWT 与 RuoYi JWT 均已在前两层判定过。
		user, authErr := model.ValidateAccessToken(accessToken)
		if authErr != nil {
			if errors.Is(authErr, model.ErrDatabase) {
				common.SysLog("ValidateAccessToken database error: " + authErr.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
				})
			}
			c.Abort()
			return
		}
		if user != nil && user.Username != "" {
			if !validUserInfo(user.Username, user.Role) {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
				})
				c.Abort()
				return
			}
			// Token is valid
			username = user.Username
			role = user.Role
			id = user.Id
			status = user.Status
			useAccessToken = true
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
			})
			c.Abort()
			return
		}
	}
	// RuoYi JWT 模式与 default 前端 access_token JWT 模式下不强制要求 New-Api-User 头
	if !fromRuoYi && !fromAccessJWT {
		// get header New-Api-User
		apiUserIdStr := c.Request.Header.Get("New-Api-User")
		if apiUserIdStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserIdNotProvided),
			})
			c.Abort()
			return
		}
		apiUserId, err := strconv.Atoi(apiUserIdStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserIdFormatError),
			})
			c.Abort()
			return

		}
		if id != apiUserId {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserIdMismatch),
			})
			c.Abort()
			return
		}
	}
	if status.(int) == common.UserStatusDisabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
		})
		c.Abort()
		return
	}
	if role.(int) < minRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
		return
	}
	if !validUserInfo(username.(string), role.(int)) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
		})
		c.Abort()
		return
	}
	// 防止不同newapi版本冲突，导致数据不通用
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", username)
	c.Set("role", role)
	c.Set("id", id)
	c.Set("group", group)
	c.Set("user_group", group)
	c.Set("use_access_token", useAccessToken)
	// 记录当前请求所属登录会话 sid（default 前端多设备管理 / 远程下线用）：
	// access_token JWT 来源直接携带；cookie 登录来源从 session 取。
	if jwtSid != "" {
		c.Set("sid", jwtSid)
	} else if sid, ok := session.Get("sid").(string); ok && sid != "" {
		c.Set("sid", sid)
	}

	// 管理/root 写操作审计兜底：内聚在鉴权链路里，保证任何经过 AdminAuth/RootAuth
	// 的写接口都会自动留痕（无需在路由上单独挂审计中间件，避免漏挂）。
	// handler 内手动埋点者会设置 ContextKeyAuditLogged，finishAdminAudit 据此跳过。
	var auditWriter *auditResponseWriter
	if minRole >= common.RoleAdminUser {
		auditWriter = beginAdminAudit(c)
	}

	c.Next()

	finishAdminAudit(c, auditWriter)
}

func authTokenHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	username := session.Get("username")
	role := session.Get("role")
	id := session.Get("id")
	status := session.Get("status")
	useAccessToken := false
	group := session.Get("group")
	if username == nil {
		// 优先尝试 RuoYi JWT 鉴权
		if common.RuoYiAuthEnabled {
			user, err := tryRuoYiJWTAuth(c)
			if err == nil && user != nil {
				if !validUserInfo(user.Username, user.Role) {
					c.JSON(http.StatusOK, gin.H{
						"success": false,
						"message": "Unauthorized, invalid user info",
					})
					c.Abort()
					return
				}
				username = user.Username
				role = user.Role
				id = user.Id
				status = user.Status
				group = user.Group
			} else if err != nil && err != errNoRuoYiJWT {
				// JWT 格式正确但验签/解析失败 -> 直接拒绝
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "Unauthorized, invalid token",
				})
				c.Abort()
				return
			}
		}
	}
	if username == nil {
		// 回退到原有的 access token 方案
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
			})
			c.Abort()
			return
		}
		user, authErr := model.ValidateAccessToken(accessToken)
		if authErr != nil {
			if errors.Is(authErr, model.ErrDatabase) {
				common.SysLog("ValidateAccessToken database error: " + authErr.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			} else {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
				})
			}
			c.Abort()
			return
		}
		if user != nil && user.Username != "" {
			if !validUserInfo(user.Username, user.Role) {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
				})
				c.Abort()
				return
			}
			// Token is valid
			username = user.Username
			role = user.Role
			id = user.Id
			status = user.Status
			useAccessToken = true
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthAccessTokenInvalid),
			})
			c.Abort()
			return
		}
	}
	// access_token 访问 /api/token 也需按平台隔离：令牌属主平台必须等于请求平台。
	// 仅对 access_token 生效：session 为内部登录、RuoYi JWT 已在解析时按平台锁定。
	if useAccessToken && !validatePlatformOrAbort(c, id.(int), false) {
		return
	}
	if status.(int) == common.UserStatusDisabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
		})
		c.Abort()
		return
	}
	if role.(int) < minRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
		return
	}
	if !validUserInfo(username.(string), role.(int)) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
		})
		c.Abort()
		return
	}
	// 防止不同newapi版本冲突，导致数据不通用
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", username)
	c.Set("role", role)
	c.Set("id", id)
	c.Set("group", group)
	c.Set("user_group", group)
	c.Set("use_access_token", useAccessToken)

	// 管理/root 写操作审计兜底：内聚在鉴权链路里，保证任何经过 AdminAuth/RootAuth
	// 的写接口都会自动留痕（无需在路由上单独挂审计中间件，避免漏挂）。
	// handler 内手动埋点者会设置 ContextKeyAuditLogged，finishAdminAudit 据此跳过。
	var auditWriter *auditResponseWriter
	if minRole >= common.RoleAdminUser {
		auditWriter = beginAdminAudit(c)
	}

	c.Next()

	finishAdminAudit(c, auditWriter)
}

func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := session.Get("id")
		if id != nil {
			c.Set("id", id)
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func UserTokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authTokenHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

func RequirePermission(permission authz.Permission) func(c *gin.Context) {
	return func(c *gin.Context) {
		role := c.GetInt("role")
		userID := c.GetInt("id")
		if authz.Can(userID, role, permission) {
			c.Next()
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
	}
}

func WssAuth(c *gin.Context) {

}

// TokenOrUserAuth allows either session-based user auth or API token auth.
// Used for endpoints that need to be accessible from both the dashboard and API clients.
func TokenOrUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Try session auth first (dashboard users)
		session := sessions.Default(c)
		if id := session.Get("id"); id != nil {
			if status, ok := session.Get("status").(int); ok && status == common.UserStatusEnabled {
				c.Set("id", id)
				c.Next()
				return
			}
		}
		// Fall back to token auth (API clients)
		TokenAuth()(c)
	}
}

// TokenAuthReadOnly 宽松版本的令牌认证中间件，用于只读查询接口。
// 只验证令牌 key 是否存在，不检查令牌状态、过期时间和额度。
// 即使令牌已过期、已耗尽或已禁用，也允许访问。
// 仍然检查用户是否被封禁。
func TokenAuthReadOnly() func(c *gin.Context) {
	return func(c *gin.Context) {
		key := c.Request.Header.Get("Authorization")
		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgTokenNotProvided),
			})
			c.Abort()
			return
		}
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.TrimPrefix(key, "sk-")
		parts := strings.Split(key, "-")
		key = parts[0]

		token, err := model.GetTokenByKey(key, false)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgTokenInvalid),
				})
			} else {
				common.SysLog("TokenAuthReadOnly GetTokenByKey database error: " + err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"success": false,
					"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
				})
			}
			c.Abort()
			return
		}

		// TokenAuthReadOnly must keep allowing other token states to query read-only
		// data, such as token usage logs; only explicitly disabled tokens are denied.
		if token.Status == common.TokenStatusDisabled {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgTokenStatusUnavailable),
			})
			c.Abort()
			return
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuthReadOnly GetUserCache error for user %d: %v", token.UserId, err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
			})
			c.Abort()
			return
		}
		if userCache.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
			})
			c.Abort()
			return
		}
		if !validatePlatformOrAbort(c, token.UserId, false) {
			return
		}

		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Set("token_key", token.Key)
		c.Next()
	}
}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 先检测是否为ws
		if c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
			// Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1
			// read sk from Sec-WebSocket-Protocol
			key := c.Request.Header.Get("Sec-WebSocket-Protocol")
			parts := strings.Split(key, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "openai-insecure-api-key") {
					key = strings.TrimPrefix(part, "openai-insecure-api-key.")
					break
				}
			}
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// 检查path包含/v1/messages 或 /v1/models
		if strings.Contains(c.Request.URL.Path, "/v1/messages") || strings.Contains(c.Request.URL.Path, "/v1/models") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			if anthropicKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
			}
		}
		// gemini api 从query中获取key
		if c.Request.URL.Path == "/v1/models" ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
			}
			// 从x-goog-api-key header中获取key
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
			}
		}
		key := c.Request.Header.Get("Authorization")
		parts := make([]string, 0)
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		if key == "" || key == "midjourney-proxy" {
			key = c.Request.Header.Get("mj-api-secret")
			if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
				key = strings.TrimSpace(key[7:])
			}
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		} else {
			key = strings.TrimPrefix(key, "sk-")
			parts = strings.Split(key, "-")
			key = parts[0]
		}
		token, err := model.ValidateUserToken(key)
		if token != nil {
			id := c.GetInt("id")
			if id == 0 {
				c.Set("id", token.UserId)
			}
		}
		if err != nil {
			if errors.Is(err, model.ErrDatabase) {
				common.SysLog("TokenAuth ValidateUserToken database error: " + err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError,
					common.TranslateMessage(c, i18n.MsgDatabaseError))
			} else {
				abortWithOpenAiMessage(c, http.StatusUnauthorized,
					common.TranslateMessage(c, i18n.MsgTokenInvalid))
			}
			return
		}

		allowIps := token.GetIpLimits()
		if len(allowIps) > 0 {
			clientIp := c.ClientIP()
			logger.LogDebug(c, "Token has IP restrictions, checking client IP %s", clientIp)
			ip := net.ParseIP(clientIp)
			if ip == nil {
				abortWithOpenAiMessage(c, http.StatusForbidden, "Unable to resolve the client's IP address")
				return
			}
			if common.IsIpInCIDRList(ip, allowIps) == false {
				abortWithOpenAiMessage(c, http.StatusForbidden, "Your IP is not in the list of allowed IPs for this token", types.ErrorCodeAccessDenied)
				return
			}
			logger.LogDebug(c, "Client IP %s passed the token IP restrictions check", clientIp)
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			common.SysLog(fmt.Sprintf("TokenAuth GetUserCache error for user %d: %v", token.UserId, err))
			abortWithOpenAiMessage(c, http.StatusInternalServerError,
				common.TranslateMessage(c, i18n.MsgDatabaseError))
			return
		}
		userEnabled := userCache.Status == common.UserStatusEnabled
		if !userEnabled {
			abortWithOpenAiMessage(c, http.StatusForbidden, common.TranslateMessage(c, i18n.MsgAuthUserBanned))
			return
		}
		if !validatePlatformOrAbort(c, token.UserId, true) {
			return
		}

		userCache.WriteContext(c)

		userGroup := userCache.Group
		tokenGroup := token.Group
		if tokenGroup != "" {
			// check common.UserUsableGroups[userGroup]
			if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("Unauthorized to access group %s", tokenGroup))
				return
			}
			// check group in common.GroupRatio
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("Group %s has been deprecated", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

		err = SetupContextForToken(c, token, parts...)
		if err != nil {
			return
		}
		c.Next()
	}
}

func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	if token == nil {
		return fmt.Errorf("token is nil")
	}
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit_enabled", true)
		c.Set("token_model_limit", token.GetModelLimitsMap())
	} else {
		c.Set("token_model_limit_enabled", false)
	}
	if token.VendorLimits != "" {
		common.SetContextKey(c, constant.ContextKeyTokenVendorLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenVendorLimit, token.GetVendorLimitsMap())
	} else {
		common.SetContextKey(c, constant.ContextKeyTokenVendorLimitEnabled, false)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)
	if token.AutoGroups != "" {
		autoGroups, err := token.GetAutoGroups()
		if err != nil {
			common.SysError(fmt.Sprintf("failed to parse auto groups for token %d: %v", token.Id, err))
			autoGroups = []string{}
			common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, autoGroups)
		} else if len(autoGroups) > 0 {
			common.SetContextKey(c, constant.ContextKeyTokenAutoGroups, autoGroups)
		}
	}
	if len(parts) > 1 {
		if model.IsAdmin(token.UserId) {
			c.Set("specific_channel_id", parts[1])
		} else {
			c.Header("specific_channel_version", "701e3ae1dc3f7975556d354e0675168d004891c8")
			abortWithOpenAiMessage(c, http.StatusForbidden, "specified channels are not supported for regular users")
			return fmt.Errorf("specified channels are not supported for regular users")
		}
	}
	return nil
}
