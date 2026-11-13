package model

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

// 审计日志：移植自官方 audit_logs（579573ac7），alpha 调整：
//   - 表建在主库 DB（不进 ByteHouse / ADB 日志库），other 列用 text 保证三库兼容；
//   - 管理操作仍照写 logs 表 type=3，本表是额外的结构化副本（Java 读 logs 不受影响）；
//   - 只记录 Go 能看到的两类：operation（管理操作）、security（API key 操作等安全敏感操作）。
const (
	AuditCategorySecurity  = "security"
	AuditCategoryOperation = "operation"
)

// AuditLog 与官方字段保持一致，前端可共用同一套类型。
type AuditLog struct {
	Id         int        `json:"id"`
	EventId    string     `json:"event_id" gorm:"type:varchar(64);uniqueIndex;index:idx_audit_time_event,priority:2"`
	UserId     int        `json:"user_id" gorm:"index:idx_audit_user_time,priority:1"`
	Username   string     `json:"username" gorm:"type:varchar(64);index"`
	ActorRole  int        `json:"actor_role"` // 操作发生时操作者的角色快照
	CreatedAt  int64      `json:"created_at" gorm:"type:bigint;index:idx_audit_user_time,priority:2;index:idx_audit_token_time,priority:2;index;index:idx_audit_time_event,priority:1"`
	Category   string     `json:"category" gorm:"type:varchar(24);index"`
	Action     string     `json:"action" gorm:"type:varchar(128)"`
	TokenRef   string     `json:"token_ref" gorm:"type:varchar(64);index:idx_audit_token_time,priority:1"`
	AuthMethod string     `json:"auth_method" gorm:"type:varchar(24)"`
	Ip         string     `json:"ip" gorm:"type:varchar(64)"`
	UserAgent  string     `json:"user_agent" gorm:"type:varchar(512)"`
	Method     string     `json:"method" gorm:"type:varchar(16)"`
	Route      string     `json:"route" gorm:"type:varchar(255)"`
	Status     int        `json:"status"`
	Success    bool       `json:"success"`
	RequestId  string     `json:"request_id" gorm:"type:varchar(64);index"`
	Content    string     `json:"content" gorm:"type:text"`
	Other      AuditOther `json:"other" gorm:"type:text"`
}

// AuditOther 是审计事件的结构化明细；特权字段单独存放，按查看者角色裁剪。
type AuditOther struct {
	Op        *AuditOperation   `json:"op,omitempty"`
	AdminInfo *AuditAdminInfo   `json:"admin_info,omitempty"`
	AuditInfo *AuditRequestInfo `json:"audit_info,omitempty"`
	RootInfo  AuditFields       `json:"root_info,omitempty"`
}

type AuditOperation struct {
	Action string      `json:"action"`
	Params AuditFields `json:"params,omitempty"`
}

type AuditAdminInfo struct {
	AdminID       int    `json:"admin_id,omitempty"`
	AdminUsername string `json:"admin_username,omitempty"`
	AdminRole     int    `json:"admin_role,omitempty"`
	AuthMethod    string `json:"auth_method,omitempty"`
}

type AuditRequestInfo struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Path    string            `json:"path"`
	Status  int               `json:"status"`
	Success bool              `json:"success"`
	Params  map[string]string `json:"params,omitempty"`
}

// AuditFields 保存各 action 的参数；读取时保留原始 JSON，避免大整数经 float64 丢精度。
type AuditFields map[string]any

func (fields *AuditFields) UnmarshalJSON(data []byte) error {
	var values map[string]json.RawMessage
	if err := common.Unmarshal(data, &values); err != nil {
		return err
	}
	if values == nil {
		*fields = nil
		return nil
	}
	decoded := make(AuditFields, len(values))
	for key, value := range values {
		decoded[key] = value
	}
	*fields = decoded
	return nil
}

func (other AuditOther) Value() (driver.Value, error) {
	data, err := common.Marshal(other)
	if err != nil {
		return nil, err
	}
	// PostgreSQL simple protocol 需要 string 而不是 bytea 参数。
	return string(data), nil
}

func (other *AuditOther) Scan(value any) error {
	var data []byte
	switch value := value.(type) {
	case nil:
		*other = AuditOther{}
		return nil
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("unsupported audit metadata database type %T", value)
	}
	var decoded AuditOther
	if len(data) > 0 {
		if err := common.Unmarshal(data, &decoded); err != nil {
			return err
		}
	}
	*other = decoded
	return nil
}

type AuditLogFilter struct {
	SelfView       bool // 用户查看自己的记录时为 true，裁剪掉管理员明细
	UserId         int
	Username       string
	Category       string
	Action         string
	TokenRef       string
	RequestId      string
	StartTimestamp int64
	EndTimestamp   int64
	Success        *bool
}

// NewAuditLogFromRequest 在请求处理期间同步抓取安全的请求元数据（不含 URL query、请求体、凭证）。
// 异步写入前必须先调用它，gin.Context 在请求结束后会被复用。
func NewAuditLogFromRequest(c *gin.Context) AuditLog {
	entry := AuditLog{CreatedAt: common.GetTimestamp(), EventId: common.NewRequestId()}
	if c == nil || c.Request == nil {
		return entry
	}
	entry.UserId = c.GetInt("id")
	entry.Username = c.GetString("username")
	entry.ActorRole = c.GetInt("role")
	entry.RequestId = c.GetString(common.RequestIdKey)
	entry.Ip = c.ClientIP()
	entry.UserAgent = c.Request.UserAgent()
	entry.Method = c.Request.Method
	entry.Route = c.FullPath()
	entry.Status = c.Writer.Status()
	entry.AuthMethod = "session"
	if c.GetBool("use_access_token") {
		entry.AuthMethod = "access_token"
	}
	return entry
}

// RecordAuditLog 写入一条审计日志；失败只记系统日志，不影响业务请求。
func RecordAuditLog(entry AuditLog) {
	if entry.CreatedAt == 0 {
		entry.CreatedAt = common.GetTimestamp()
	}
	if entry.EventId == "" {
		entry.EventId = common.NewRequestId()
	}
	if entry.RequestId == "" {
		entry.RequestId = entry.EventId
	}
	switch entry.ActorRole {
	case common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser:
	default:
		entry.ActorRole = 0 // 角色未知的记录只有 root 能看到
	}
	if entry.Username == "" && entry.UserId > 0 {
		entry.Username, _ = GetUsernameById(entry.UserId, false)
	}
	if ua := []rune(entry.UserAgent); len(ua) > 512 {
		entry.UserAgent = string(ua[:512])
	}
	if err := DB.Create(&entry).Error; err != nil {
		logger.LogError(context.Background(), common.LogText("audit log write failed (request_id=%s): %v", entry.RequestId, err))
	}
}

func GetAuditLogs(filter AuditLogFilter, start, limit, viewerRole int) ([]*AuditLog, int64, error) {
	query := DB.Model(&AuditLog{})
	if viewerRole < common.RoleRootUser {
		query = query.Where("actor_role IN ?", []int{common.RoleCommonUser, common.RoleAdminUser})
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.TokenRef != "" {
		query = query.Where("token_ref = ?", filter.TokenRef)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	logs := make([]*AuditLog, 0)
	if err := query.Order("created_at DESC").Order("event_id DESC").Offset(start).Limit(limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	for _, entry := range logs {
		if filter.SelfView || viewerRole < common.RoleRootUser {
			entry.Other.RootInfo = nil
		}
		if filter.SelfView {
			entry.Other.AdminInfo = nil
			entry.Other.AuditInfo = nil
		}
	}
	return logs, total, nil
}

// DeleteOldAuditLogBatch 删除一批早于 targetTimestamp 的审计日志，返回删除行数。
func DeleteOldAuditLogBatch(targetTimestamp int64, limit int) (int64, error) {
	var ids []int
	if err := DB.Model(&AuditLog{}).Where("created_at < ?", targetTimestamp).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("id IN ?", ids).Delete(&AuditLog{})
	return result.RowsAffected, result.Error
}

func ValidAuditCategory(category string) bool {
	return category == "" || category == AuditCategorySecurity || category == AuditCategoryOperation
}
