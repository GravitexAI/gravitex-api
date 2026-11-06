package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var errNoRuoYiJWT = errors.New("no ruoyi jwt token")

// ruoYiChannelTestServiceLoginType 是 Java ChannelLiteTestService.mintServiceToken 签发内部测试令牌时
// 传给 SaJwtUtil.createToken 的 loginType，该令牌只允许调用渠道测试接口。
const ruoYiChannelTestServiceLoginType = "channel-lite-test-service"

// ruoYiChannelTestServiceRoute 是内部测试令牌唯一允许访问的路由（gin FullPath）。
const ruoYiChannelTestServiceRoute = "/api/channel/test/:id"

// validateRuoYiClaims 在验签通过后校验 sa-token-jwt 的业务字段：
//  1. eff：过期时间（毫秒时间戳，-1 表示永不过期）；存在且已过期则拒绝。普通用户令牌无此字段，不受影响。
//  2. loginType 为内部测试服务令牌时，仅允许 GET /api/channel/test/:id，其他路由一律拒绝。
func validateRuoYiClaims(c *gin.Context, claims jwt.MapClaims) error {
	if rawEff, ok := claims["eff"]; ok {
		var eff int64
		switch v := rawEff.(type) {
		case float64:
			eff = int64(v)
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return fmt.Errorf("ruoyi jwt eff 非法: %v", rawEff)
			}
			eff = n
		case int64:
			eff = v
		case int:
			eff = int64(v)
		default:
			return fmt.Errorf("ruoyi jwt eff 非法: %v", rawEff)
		}
		if eff != -1 && eff < time.Now().UnixMilli() {
			return fmt.Errorf("ruoyi jwt 已过期")
		}
	}

	if loginType, _ := claims["loginType"].(string); loginType == ruoYiChannelTestServiceLoginType {
		if c.Request.Method != http.MethodGet || c.FullPath() != ruoYiChannelTestServiceRoute {
			return fmt.Errorf("渠道测试服务令牌不允许访问 %s %s", c.Request.Method, c.Request.URL.Path)
		}
	}
	return nil
}

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
	if err := validateRuoYiClaims(c, claims); err != nil {
		logger.LogError(c, fmt.Sprintf("RuoYi JWT 校验失败: %s", err.Error()))
		return nil, err
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

	var user *model.User
	if PlatformIsolationEnabled() {
		user, err = model.GetUserByUsernameAndPlatform(username, RequestPlatformID(c))
	} else {
		user, err = model.GetUserByUsername(username)
	}
	if err != nil {
		logger.LogError(c, fmt.Sprintf("根据 RuoYi JWT 中的 username(%s) 查询用户失败: %s", username, err.Error()))
		return nil, err
	}

	return user, nil
}
