package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const auditLogCleanupBatchSize = 1000

var auditLogCleanupOnce sync.Once

// StartAuditLogCleanupTask 每天清理一次超过保留期的审计日志，只在 master 节点运行。
// 保留天数取环境变量 AUDIT_LOG_RETENTION_DAYS，默认 180，0 表示不清理。
func StartAuditLogCleanupTask() {
	auditLogCleanupOnce.Do(func() {
		retentionDays := common.GetEnvOrDefault("AUDIT_LOG_RETENTION_DAYS", 180)
		if !common.IsMasterNode || retentionDays <= 0 {
			return
		}
		gopool.Go(func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				cutoff := common.GetTimestamp() - int64(retentionDays)*24*3600
				var total int64
				for {
					deleted, err := model.DeleteOldAuditLogBatch(cutoff, auditLogCleanupBatchSize)
					if err != nil {
						logger.LogError(context.Background(), common.LogText("audit log cleanup failed: %v", err))
						break
					}
					total += deleted
					if deleted < auditLogCleanupBatchSize {
						break
					}
				}
				if total > 0 {
					logger.LogInfo(context.Background(), common.LogText("audit log cleanup removed %d records older than %d days", total, retentionDays))
				}
				<-ticker.C
			}
		})
	})
}
