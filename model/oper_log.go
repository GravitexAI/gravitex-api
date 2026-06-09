package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// OperLog 操作日志，记录模型价格/用户分组/渠道配置等变更操作。
// Go 端写入，Java 管理端只读，两侧通过共享 MySQL 交换数据。
type OperLog struct {
	Id        int    `json:"id"`
	OperType  string `json:"oper_type" gorm:"type:varchar(64);index"`    // 模型价格 / 用户分组 / 渠道配置
	Content   string `json:"content" gorm:"type:text"`                   // 本次改动摘要（系统自动填写）
	Remark    string `json:"remark" gorm:"type:text"`                    // 运维备注（人工填写，可空）
	Operator  string `json:"operator" gorm:"type:varchar(255);index"`    // 操作人（登录用户名）
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
	Pushed    bool   `json:"pushed" gorm:"default:false;index"`          // 是否已推送飞书，供定时推送任务使用
}

// CreateOperLog 写入一条操作日志。
func CreateOperLog(log *OperLog) error {
	log.CreatedAt = common.GetTimestamp()
	return DB.Create(log).Error
}

// GetOperLogsPaged 分页查询操作日志，operType 为空则查全部类型。
func GetOperLogsPaged(operType string, page, pageSize int) ([]*OperLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	query := DB.Model(&OperLog{})
	if operType != "" {
		query = query.Where("oper_type = ?", operType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*OperLog
	if err := query.Order("id desc").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// GetUnpushedOperLogs 获取尚未推送过的操作日志（pushed = false）。
func GetUnpushedOperLogs(limit int) ([]*OperLog, error) {
	var logs []*OperLog
	err := DB.Model(&OperLog{}).
		Where("pushed = ?", false).
		Order("id asc").
		Limit(limit).
		Find(&logs).Error
	return logs, err
}

// MarkOperLogsPushed 将指定 id 列表的日志标记为已推送。
func MarkOperLogsPushed(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	return DB.Model(&OperLog{}).
		Where("id IN ?", ids).
		Update("pushed", true).Error
}

// UpdateOperLog 仅在需要更新单条日志时使用（保留 gorm 约束）。
func UpdateOperLog(log *OperLog) error {
	return DB.Save(log).Error
}

// DeleteOperLog 删除指定 id 的操作日志。
func DeleteOperLog(id int) error {
	return DB.Where("id = ?", id).Delete(&OperLog{}).Error
}

// Ensure OperLog satisfies GORM tabler interface for table name inference.
var _ interface{ TableName() string } = (*OperLog)(nil)

func (OperLog) TableName() string {
	return "oper_logs"
}

// Ensure gorm.DB recognises the model (compile-time import guard).
var _ *gorm.DB
