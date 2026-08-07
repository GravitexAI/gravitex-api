package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/golang-jwt/jwt/v5"
)

// 默认前端（web/default）的令牌体系：
//   - access_token：短寿命、无状态的 HS256 JWT，随每个请求经 Authorization: Bearer 携带；
//     claims 载 uid + sid（sid 关联持久化的 login_session，用于远程下线与多设备管理）。
//     与 user.access_token 数据库列（长期系统管理令牌，char(32)）完全独立，互不复用。
//   - flow_token：2FA 登录中转令牌，仅载 pending_uid，寿命极短，用于在无可用 cookie 的
//     默认前端流程里安全地承接“已验密码、待验二次因子”的状态。
//
// 两者均使用 common.SessionSecret 签名，与 RuoYi JWT（RuoYiJWTSecret）分属不同密钥域。

const (
	// AccessTokenTTL 是 access_token 的有效期（秒）。短寿命 + 刷新令牌轮换。
	AccessTokenTTL int64 = 15 * 60 // 15 分钟
	// FlowTokenTTL 是 2FA 中转令牌的有效期（秒）。
	FlowTokenTTL int64 = 5 * 60 // 5 分钟

	accessTokenSubject = "new-api-access"
	flowTokenSubject   = "new-api-2fa-flow"
)

var (
	// ErrInvalidAccessToken 表示 access_token 解析/验签/语义校验失败。
	ErrInvalidAccessToken = errors.New("invalid access token")
	// ErrInvalidFlowToken 表示 2FA flow_token 解析/验签/语义校验失败。
	ErrInvalidFlowToken = errors.New("invalid flow token")
)

// AccessClaims 是 access_token 的自定义载荷。
type AccessClaims struct {
	Uid int    `json:"uid"`
	Sid string `json:"sid"`
	jwt.RegisteredClaims
}

// IssueAccessToken 为 (uid, sid) 签发一枚 access_token，返回令牌串与其绝对过期时间（Unix 秒）。
func IssueAccessToken(uid int, sid string) (token string, expiresAt int64, err error) {
	now := common.GetTimestamp()
	expiresAt = now + AccessTokenTTL
	claims := AccessClaims{
		Uid: uid,
		Sid: sid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accessTokenSubject,
			IssuedAt:  jwt.NewNumericDate(time.Unix(now, 0)),
			ExpiresAt: jwt.NewNumericDate(time.Unix(expiresAt, 0)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(common.SessionSecret))
	if err != nil {
		return "", 0, err
	}
	return signed, expiresAt, nil
}

// ParseAccessToken 解析并校验 access_token，成功返回 (uid, sid)。
func ParseAccessToken(token string) (uid int, sid string, err error) {
	claims := &AccessClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, sessionSecretKeyFunc, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return 0, "", ErrInvalidAccessToken
	}
	if claims.Subject != accessTokenSubject || claims.Uid <= 0 || claims.Sid == "" {
		return 0, "", ErrInvalidAccessToken
	}
	return claims.Uid, claims.Sid, nil
}

// FlowClaims 是 2FA flow_token 的自定义载荷。
type FlowClaims struct {
	PendingUid int `json:"pending_uid"`
	jwt.RegisteredClaims
}

// IssueFlowToken 为待完成 2FA 的用户签发一枚短寿命中转令牌。
func IssueFlowToken(pendingUid int) (string, error) {
	now := common.GetTimestamp()
	claims := FlowClaims{
		PendingUid: pendingUid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   flowTokenSubject,
			IssuedAt:  jwt.NewNumericDate(time.Unix(now, 0)),
			ExpiresAt: jwt.NewNumericDate(time.Unix(now+FlowTokenTTL, 0)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(common.SessionSecret))
}

// ParseFlowToken 解析并校验 2FA flow_token，成功返回 pending user id。
func ParseFlowToken(token string) (int, error) {
	claims := &FlowClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, sessionSecretKeyFunc, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return 0, ErrInvalidFlowToken
	}
	if claims.Subject != flowTokenSubject || claims.PendingUid <= 0 {
		return 0, ErrInvalidFlowToken
	}
	return claims.PendingUid, nil
}

func sessionSecretKeyFunc(t *jwt.Token) (interface{}, error) {
	if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
	}
	return []byte(common.SessionSecret), nil
}
