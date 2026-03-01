package middleware

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var errNoRuoYiJWT = errors.New("no ruoyi jwt token")

// tryRuoYiJWTAuth 尝试使用 RuoYi sa-token JWT 进行鉴权，返回对应的 Go 用户
func tryRuoYiJWTAuth(c *gin.Context) (*model.User, error) {
	if !common.RuoYiAuthEnabled {
		return nil, errNoRuoYiJWT
	}
	if strings.TrimSpace(common.RuoYiJWTSecret) == "" {
		common.SysError("RuoYiAuthEnabled 已开启，但 RUOYI_JWT_SECRET 为空")
		return nil, fmt.Errorf("ruoyi jwt secret is empty")
	}

	authHeader := c.Request.Header.Get("Authorization")
	if authHeader == "" {
		return nil, errNoRuoYiJWT
	}

	// 支持 "Bearer xxx" 或直接传 JWT
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		authHeader = strings.TrimSpace(authHeader[7:])
	}
	tokenString := strings.TrimSpace(authHeader)

	// 简单判断是否为 JWT（三段、由 . 分隔）
	if strings.Count(tokenString, ".") != 2 {
		return nil, errNoRuoYiJWT
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(common.RuoYiJWTSecret), nil
	})
	if err != nil {
		logger.LogError(c, fmt.Sprintf("解析 RuoYi JWT 失败: %s", err.Error()))
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("ruoyi jwt token is invalid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("ruoyi jwt claims 类型不正确")
	}

	// sa-token 默认 extra 中的用户名 key 为 userName
	usernameClaim, ok := claims["userName"]
	if !ok {
		// 兼容可能的 username 命名
		usernameClaim, ok = claims["username"]
		if !ok {
			return nil, fmt.Errorf("ruoyi jwt 中未找到 userName/username 字段")
		}
	}

	username, ok := usernameClaim.(string)
	if !ok || strings.TrimSpace(username) == "" {
		return nil, fmt.Errorf("ruoyi jwt 中 username 非法")
	}

	user, err := model.GetUserByUsername(username)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("根据 RuoYi JWT 中的 username(%s) 查询用户失败: %s", username, err.Error()))
		return nil, err
	}

	return user, nil
}
