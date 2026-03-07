package model

import (
	"github.com/QuantumNous/new-api/common"
)

// OEM账户变动日志类型
const (
	OemAccountLogTypeProfit  = 1 // 盈利
	OemAccountLogTypeSubsidy = 2 // 补贴
	OemAccountLogTypeTopUp   = 3 // 充值
	OemAccountLogTypeDeduct  = 4 // 扣费
)

// OemAccountLog OEM账户变动日志
type OemAccountLog struct {
	Id             int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	OemId          int64  `json:"oem_id" gorm:"index"`
	OemAdminUserId int    `json:"oem_admin_user_id" gorm:"index"`
	CreatedAt      int64  `json:"created_at" gorm:"index"`
	Type           int    `json:"type"` // 1-盈利，2-补贴，3-充值，4-扣费
	Amount         int64  `json:"amount"`
	BalanceBefore  int64  `json:"balance_before"`
	BalanceAfter   int64  `json:"balance_after"`
	RelatedLogId   *int   `json:"related_log_id,omitempty"`
	RelatedUserId  *int   `json:"related_user_id,omitempty"`
	Content        string `json:"content"`
}

func (OemAccountLog) TableName() string {
	return "oem_account_logs"
}

// RecordOemAccountLog 记录OEM账户变动日志
func RecordOemAccountLog(log *OemAccountLog) error {
	if log.CreatedAt == 0 {
		log.CreatedAt = common.GetTimestamp()
	}
	return LOG_DB.Create(log).Error
}

// GetOemAccountLogs 获取OEM账户变动日志列表
func GetOemAccountLogs(oemId int64, page, pageSize int) ([]OemAccountLog, int64, error) {
	var logs []OemAccountLog
	var total int64

	query := LOG_DB.Model(&OemAccountLog{}).Where("oem_id = ?", oemId)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}

// GetOemAccountLogsByType 按类型获取OEM账户变动日志
func GetOemAccountLogsByType(oemId int64, logType int, page, pageSize int) ([]OemAccountLog, int64, error) {
	var logs []OemAccountLog
	var total int64

	query := LOG_DB.Model(&OemAccountLog{}).Where("oem_id = ? AND type = ?", oemId, logType)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err = query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	return logs, total, nil
}
