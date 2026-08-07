package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// resolveRefreshSid 解析刷新请求的目标 sid：
// 优先取 default 前端携带的 X-Auth-Session 头（其本地持有的会话 sid），
// 回退到 cookie session 中的 sid（首次加载 / classic 升级场景）。
func resolveRefreshSid(c *gin.Context) string {
	if headerSid := c.GetHeader("X-Auth-Session"); headerSid != "" {
		return headerSid
	}
	session := sessions.Default(c)
	if sid, ok := session.Get("sid").(string); ok {
		return sid
	}
	return ""
}

// RefreshAuth 刷新 default 前端的 access_token：校验 sid 对应的登录会话有效后，
// 续期会话活跃时间并复签一枚新 access_token，返回完整 bundle。
// 无有效会话时返回 401，前端据此转匿名态。
func RefreshAuth(c *gin.Context) {
	sid := resolveRefreshSid(c)
	loginSession, err := model.GetActiveLoginSession(sid)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if loginSession == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "not logged in",
		})
		return
	}
	user, err := model.GetUserById(loginSession.UserId, false)
	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "not logged in",
		})
		return
	}
	if user.Status == common.UserStatusDisabled {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "user banned",
		})
		return
	}

	if err := model.TouchLoginSession(sid); err != nil {
		common.SysLog("failed to touch login session: " + err.Error())
	}
	loginSession.LastActiveAt = common.GetTimestamp()

	data, err := buildAuthBundle(user, loginSession, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

// LogoutAuth 是 default 前端的登出：撤销当前登录会话并清空 cookie。
func LogoutAuth(c *gin.Context) {
	session := sessions.Default(c)
	userId := c.GetInt("id")
	sid := ""
	if v, ok := session.Get("sid").(string); ok {
		sid = v
	}
	if headerSid := c.GetHeader("X-Auth-Session"); headerSid != "" {
		sid = headerSid
	}
	if sid != "" && userId > 0 {
		if err := model.RevokeLoginSession(userId, sid); err != nil {
			common.SysLog("failed to revoke login session on logout: " + err.Error())
		}
	}
	session.Clear()
	if err := session.Save(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// GetLoginSessions 列出当前用户所有有效登录会话，标记当前会话。
func GetLoginSessions(c *gin.Context) {
	userId := c.GetInt("id")
	currentSid := currentSessionSid(c)
	sessionsList, err := model.ListActiveLoginSessions(userId, currentSid)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    sessionsList,
	})
}

// RevokeLoginSessionBySid 远程下线指定会话（限本人）。
func RevokeLoginSessionBySid(c *gin.Context) {
	userId := c.GetInt("id")
	sid := c.Param("sid")
	if err := model.RevokeLoginSession(userId, sid); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// RevokeOtherLoginSessions 下线当前用户除本会话外的所有其他设备。
func RevokeOtherLoginSessions(c *gin.Context) {
	userId := c.GetInt("id")
	currentSid := currentSessionSid(c)
	if err := model.RevokeOtherLoginSessions(userId, currentSid); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// currentSessionSid 从鉴权上下文或 cookie 中取当前请求所属的 sid。
func currentSessionSid(c *gin.Context) string {
	if sid, ok := c.Get("sid"); ok {
		if s, ok := sid.(string); ok && s != "" {
			return s
		}
	}
	if headerSid := c.GetHeader("X-Auth-Session"); headerSid != "" {
		return headerSid
	}
	session := sessions.Default(c)
	if sid, ok := session.Get("sid").(string); ok {
		return sid
	}
	return ""
}
