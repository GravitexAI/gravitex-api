package model

import "time"

// OperLogPushJobLog 操作日志推送任务执行历史。
// Java 端定时推送任务写入，Go 端 AutoMigrate 建表。
type OperLogPushJobLog struct {
	Id          int       `json:"id"`
	TriggerType string    `json:"trigger_type" gorm:"type:varchar(32)"` // SCHEDULED / MANUAL
	PushedCount int       `json:"pushed_count" gorm:"default:0"`         // 本次推送条数
	Status      string    `json:"status" gorm:"type:varchar(32)"`        // RUNNING / SUCCESS / FAILED / SKIPPED
	Message     string    `json:"message" gorm:"type:text"`
	StartTime   time.Time `json:"start_time"`
	FinishTime  time.Time `json:"finish_time"`
	DurationMs  int64     `json:"duration_ms" gorm:"default:0"`
}

func (OperLogPushJobLog) TableName() string {
	return "t_oper_log_push_job_log"
}
