package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// LoginSession 记录一次登录会话（默认前端多设备管理 + 远程下线的持久化载体）。
// 时间字段统一使用 Unix 秒，与前端 auth-session（Math.floor(Date.now()/1000)）契约对齐。
// RevokedAt == 0 表示会话有效；> 0 表示已被撤销（远程下线 / 登出）。
// current 字段由查询侧根据当前请求携带的 sid 派生，不落库。
type LoginSession struct {
	Id           int    `json:"-" gorm:"primaryKey"`
	Sid          string `json:"sid" gorm:"type:varchar(64);uniqueIndex;column:sid"`
	UserId       int    `json:"-" gorm:"index;column:user_id"`
	LoginMethod  string `json:"login_method" gorm:"type:varchar(32);column:login_method"`
	Ip           string `json:"ip" gorm:"type:varchar(64);column:ip"`
	UserAgent    string `json:"user_agent" gorm:"type:varchar(512);column:user_agent"`
	CreatedAt    int64  `json:"created_at" gorm:"column:created_at"`
	LastActiveAt int64  `json:"last_active_at" gorm:"column:last_active_at"`
	ExpiresAt    int64  `json:"expires_at" gorm:"column:expires_at"`
	RevokedAt    int64  `json:"-" gorm:"default:0;column:revoked_at"`

	Current bool `json:"current" gorm:"-:all"`
}

// LoginSessionTTL 是登录会话的持久有效期（秒）。access_token 的短寿命由 JWT exp 单独控制，
// 会话本身作为「可刷新」的长期凭证存在，过期后需要重新登录。
const LoginSessionTTL int64 = 30 * 24 * 60 * 60 // 30 天

// CreateLoginSession 新建一条登录会话并返回其指针。
func CreateLoginSession(userId int, loginMethod, ip, userAgent string) (*LoginSession, error) {
	now := common.GetTimestamp()
	s := &LoginSession{
		Sid:          common.GetUUID(),
		UserId:       userId,
		LoginMethod:  loginMethod,
		Ip:           ip,
		UserAgent:    userAgent,
		CreatedAt:    now,
		LastActiveAt: now,
		ExpiresAt:    now + LoginSessionTTL,
	}
	if err := DB.Create(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

// GetActiveLoginSession 按 sid 取出未撤销、未过期的登录会话；不存在返回 (nil, nil)。
func GetActiveLoginSession(sid string) (*LoginSession, error) {
	if sid == "" {
		return nil, nil
	}
	s := &LoginSession{}
	err := DB.Where("sid = ? AND revoked_at = ? AND expires_at > ?", sid, 0, common.GetTimestamp()).First(s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// TouchLoginSession 刷新会话活跃时间（用于刷新令牌时的续期心跳）。best-effort，忽略未命中。
func TouchLoginSession(sid string) error {
	if sid == "" {
		return nil
	}
	return DB.Model(&LoginSession{}).
		Where("sid = ? AND revoked_at = ?", sid, 0).
		Update("last_active_at", common.GetTimestamp()).Error
}

// RevokeLoginSession 撤销指定 sid 的会话，限定归属 userId，避免越权下线他人会话。
func RevokeLoginSession(userId int, sid string) error {
	if sid == "" {
		return nil
	}
	return DB.Model(&LoginSession{}).
		Where("sid = ? AND user_id = ? AND revoked_at = ?", sid, userId, 0).
		Update("revoked_at", common.GetTimestamp()).Error
}

// RevokeOtherLoginSessions 撤销该用户除 keepSid 外的所有有效会话（“下线其他设备”）。
func RevokeOtherLoginSessions(userId int, keepSid string) error {
	return DB.Model(&LoginSession{}).
		Where("user_id = ? AND sid <> ? AND revoked_at = ?", userId, keepSid, 0).
		Update("revoked_at", common.GetTimestamp()).Error
}

// ListActiveLoginSessions 列出该用户所有未撤销、未过期的会话，按最近活跃降序。
// currentSid 命中的条目会被标记 current=true。
func ListActiveLoginSessions(userId int, currentSid string) ([]*LoginSession, error) {
	var sessions []*LoginSession
	err := DB.Where("user_id = ? AND revoked_at = ? AND expires_at > ?", userId, 0, common.GetTimestamp()).
		Order("last_active_at desc").
		Find(&sessions).Error
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		if s.Sid == currentSid {
			s.Current = true
		}
	}
	return sessions, nil
}
