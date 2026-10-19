package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskBillingSettlementStatus string

const (
	TaskBillingSettlementPending          TaskBillingSettlementStatus = "pending"
	TaskBillingSettlementSettled          TaskBillingSettlementStatus = "settled"
	TaskBillingSettlementUsageUnavailable TaskBillingSettlementStatus = "usage_unavailable_preconsume_retained"
)

type TaskBillingSettlement struct {
	ID                 int64                       `gorm:"primaryKey"`
	TaskKey            string                      `gorm:"type:char(64);uniqueIndex"`
	TaskID             string                      `gorm:"type:varchar(512);index"`
	Status             TaskBillingSettlementStatus `gorm:"type:varchar(50);index"`
	Payload            string                      `gorm:"type:text"`
	Outcome            string                      `gorm:"type:varchar(80)"`
	ActualQuota        int                         `gorm:"default:0"`
	UsageMissingReason string                      `gorm:"type:text"`
	MissingUsageKeys   string                      `gorm:"type:text"`
	CacheSyncPending   bool                        `gorm:"default:false;index"`
	CreatedAt          int64                       `gorm:"index"`
	UpdatedAt          int64                       `gorm:"index"`
}

func (TaskBillingSettlement) TableName() string { return "task_billing_settlements" }

type TaskBillingLogOutboxStatus string

const (
	TaskBillingLogOutboxPending    TaskBillingLogOutboxStatus = "pending"
	TaskBillingLogOutboxDelivering TaskBillingLogOutboxStatus = "delivering"
	TaskBillingLogOutboxDelivered  TaskBillingLogOutboxStatus = "delivered"
)

type TaskBillingLogOutbox struct {
	ID           int64                      `gorm:"primaryKey"`
	OperationKey string                     `gorm:"type:char(64);uniqueIndex"`
	OperationID  string                     `gorm:"type:varchar(512);index"`
	TaskID       string                     `gorm:"type:varchar(512);index"`
	Payload      string                     `gorm:"type:text"`
	Status       TaskBillingLogOutboxStatus `gorm:"type:varchar(20);index"`
	Attempts     int                        `gorm:"default:0"`
	LastError    string                     `gorm:"type:text"`
	CreatedAt    int64                      `gorm:"index"`
	UpdatedAt    int64                      `gorm:"index"`
	DeliveredAt  int64                      `gorm:"default:0"`
}

func (TaskBillingLogOutbox) TableName() string { return "task_billing_log_outbox" }

func taskBillingKey(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func CreateTaskBillingSettlement(taskID, payload string) (*TaskBillingSettlement, error) {
	if taskID == "" {
		return nil, errors.New("task ID is required")
	}
	now := time.Now().Unix()
	settlement := &TaskBillingSettlement{
		TaskKey: taskBillingKey(taskID), TaskID: taskID,
		Status: TaskBillingSettlementPending, Payload: payload,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := DB.Create(settlement).Error; err != nil {
		return nil, err
	}
	return settlement, nil
}

func CreateTaskBillingLogOutbox(operationID, taskID string, payload string) (*TaskBillingLogOutbox, error) {
	if operationID == "" || taskID == "" {
		return nil, errors.New("operation ID and task ID are required")
	}
	now := time.Now().Unix()
	outbox := &TaskBillingLogOutbox{
		OperationKey: taskBillingKey(operationID), OperationID: operationID, TaskID: taskID,
		Payload: payload, Status: TaskBillingLogOutboxPending,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := DB.Create(outbox).Error; err != nil {
		return nil, err
	}
	return outbox, nil
}

// FinalizeTaskBillingSettlement must run inside the same primary-database
// transaction as the financial quota mutations. A settled row is terminal;
// usage-unavailable rows may be retried if a later poll supplies valid usage.
func FinalizeTaskBillingSettlement(tx *gorm.DB, taskID string, status TaskBillingSettlementStatus, outcome string, actualQuota int, usageReason, missingKeys string) (bool, error) {
	if tx == nil {
		return false, errors.New("task billing settlement transaction is required")
	}
	if status != TaskBillingSettlementSettled && status != TaskBillingSettlementUsageUnavailable {
		return false, errors.New("invalid task billing settlement final status")
	}
	var settlement TaskBillingSettlement
	if err := lockForUpdate(tx).Where("task_key = ?", taskBillingKey(taskID)).First(&settlement).Error; err != nil {
		return false, err
	}
	if settlement.Status == TaskBillingSettlementSettled {
		return false, nil
	}
	if settlement.Status != TaskBillingSettlementPending && settlement.Status != TaskBillingSettlementUsageUnavailable {
		return false, errors.New("invalid task billing settlement state")
	}
	updates := map[string]any{
		"status": status, "outcome": outcome, "actual_quota": actualQuota,
		"usage_missing_reason": usageReason, "missing_usage_keys": missingKeys,
		"updated_at": time.Now().Unix(),
	}
	return true, tx.Model(&TaskBillingSettlement{}).Where("id = ?", settlement.ID).Updates(updates).Error
}

func MarkTaskBillingLogOutboxDelivered(tx *gorm.DB, operationID string) error {
	if tx == nil {
		return errors.New("task billing outbox transaction is required")
	}
	return tx.Model(&TaskBillingLogOutbox{}).
		Where("operation_key = ? AND status = ?", taskBillingKey(operationID), TaskBillingLogOutboxPending).
		Updates(map[string]any{
			"status":       TaskBillingLogOutboxDelivered,
			"delivered_at": time.Now().Unix(),
			"updated_at":   time.Now().Unix(),
		}).Error
}

func GetPendingTaskBillingLogOutbox(limit int) ([]TaskBillingLogOutbox, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []TaskBillingLogOutbox
	staleBefore := time.Now().Add(-5 * time.Minute).Unix()
	err := DB.Where("status = ? OR (status = ? AND updated_at < ?)", TaskBillingLogOutboxPending, TaskBillingLogOutboxDelivering, staleBefore).
		Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func ClaimTaskBillingLogOutbox(id int64) (bool, error) {
	claimed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var row TaskBillingLogOutbox
		if err := lockForUpdate(tx).First(&row, id).Error; err != nil {
			return err
		}
		if row.Status == TaskBillingLogOutboxDelivered ||
			(row.Status == TaskBillingLogOutboxDelivering && row.UpdatedAt >= time.Now().Add(-5*time.Minute).Unix()) {
			return nil
		}
		if err := tx.Model(&TaskBillingLogOutbox{}).Where("id = ?", row.ID).Updates(map[string]any{
			"status": TaskBillingLogOutboxDelivering, "attempts": gorm.Expr("attempts + 1"),
			"updated_at": time.Now().Unix(), "last_error": "",
		}).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

func MarkTaskBillingLogOutboxFailed(id int64, deliveryErr error) error {
	if deliveryErr == nil {
		return errors.New("task billing outbox delivery error is required")
	}
	return DB.Model(&TaskBillingLogOutbox{}).Where("id = ? AND status = ?", id, TaskBillingLogOutboxDelivering).
		Updates(map[string]any{"status": TaskBillingLogOutboxPending, "last_error": deliveryErr.Error(), "updated_at": time.Now().Unix()}).Error
}

func MarkTaskBillingLogOutboxDeliveredByID(id int64) error {
	return DB.Model(&TaskBillingLogOutbox{}).Where("id = ? AND status = ?", id, TaskBillingLogOutboxDelivering).
		Updates(map[string]any{"status": TaskBillingLogOutboxDelivered, "delivered_at": time.Now().Unix(), "updated_at": time.Now().Unix()}).Error
}

type TaskBillingSettlementCommit struct {
	TaskID             string
	ActualQuota        int
	Status             TaskBillingSettlementStatus
	Outcome            string
	UsageMissingReason string
	MissingUsageKeys   string
	OutboxOperationID  string
	OutboxPayload      string
	TaskPrivateData    string
}

// CommitTaskBillingSettlement atomically applies the task's final quota delta,
// updates accounting totals, finalizes its settlement row, and creates its log
// outbox event. Cache invalidation is idempotently recoverable via the pending
// marker because Redis cannot participate in the primary database transaction.
func CommitTaskBillingSettlement(input TaskBillingSettlementCommit) (bool, error) {
	if input.TaskID == "" || input.ActualQuota < 0 || input.ActualQuota > common.MaxQuota {
		return false, errors.New("invalid task billing settlement input")
	}
	if input.Status != TaskBillingSettlementSettled && input.Status != TaskBillingSettlementUsageUnavailable {
		return false, errors.New("invalid task billing settlement status")
	}
	if input.OutboxOperationID == "" || input.OutboxPayload == "" {
		return false, errors.New("task billing settlement requires a log outbox event")
	}
	if common.BatchUpdateEnabled {
		batchUpdate()
	}

	var tokenKey string
	var before Task
	if err := DB.First(&before, "task_id = ?", input.TaskID).Error; err != nil {
		return false, err
	}
	tokenID := before.PrivateData.TokenId
	if tokenID <= 0 {
		tokenID = before.TokenId
	}
	if tokenID > 0 && common.RedisEnabled {
		var token Token
		if err := DB.Select("id", "key").First(&token, tokenID).Error; err == nil {
			tokenKey = token.Key
			if err := invalidateTokenCacheForMutation(tokenKey); err != nil {
				common.SysLog("failed to invalidate task settlement token cache before mutation: " + err.Error())
			}
		}
	}
	if common.RedisEnabled {
		if err := invalidateUserCache(before.UserId); err != nil {
			common.SysLog("failed to invalidate task settlement user cache before mutation: " + err.Error())
		}
	}

	committed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var settlement TaskBillingSettlement
		if err := lockForUpdate(tx).Where("task_key = ?", taskBillingKey(input.TaskID)).First(&settlement).Error; err != nil {
			return err
		}
		if settlement.Status == TaskBillingSettlementSettled {
			return nil
		}
		if settlement.Status != TaskBillingSettlementPending && settlement.Status != TaskBillingSettlementUsageUnavailable {
			return errors.New("invalid task billing settlement state")
		}
		var task Task
		if err := lockForUpdate(tx).Where("task_id = ?", input.TaskID).First(&task).Error; err != nil {
			return err
		}
		if task.Status != TaskStatusSuccess {
			return errors.New("only successful tasks can settle tiered usage")
		}
		if task.Quota < 0 || task.Quota > common.MaxQuota {
			return errors.New("pre-consumed task quota is outside the supported range")
		}
		actualQuota := input.ActualQuota
		if input.Status == TaskBillingSettlementUsageUnavailable {
			actualQuota = task.Quota
		}
		delta := actualQuota - task.Quota
		if delta != 0 {
			var user User
			if err := lockForUpdate(tx).Select("id", "quota", "used_quota").Where("id = ?", task.UserId).First(&user).Error; err != nil {
				return err
			}
			minQuota := -common.MaxQuota - 1
			newUsedQuota := user.UsedQuota + delta
			if newUsedQuota < minQuota || newUsedQuota > common.MaxQuota {
				return errors.New("user used quota is outside the supported range after task settlement")
			}
			userUpdates := map[string]any{"used_quota": newUsedQuota}
			if task.PrivateData.BillingSource == "subscription" && task.PrivateData.SubscriptionId > 0 {
				var sub UserSubscription
				if err := lockForUpdate(tx).Where("id = ?", task.PrivateData.SubscriptionId).First(&sub).Error; err != nil {
					return err
				}
				maxInt64 := int64(^uint64(0) >> 1)
				minInt64 := -maxInt64 - 1
				if delta > 0 && sub.AmountUsed > maxInt64-int64(delta) || delta < 0 && sub.AmountUsed < minInt64-int64(delta) {
					return errors.New("subscription quota counter overflow during task settlement")
				}
				newUsed := sub.AmountUsed + int64(delta)
				if newUsed < 0 {
					newUsed = 0
				}
				if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
					return errors.New("subscription quota is insufficient for task settlement")
				}
				if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("amount_used", newUsed).Error; err != nil {
					return err
				}
			} else {
				newQuota := user.Quota - delta
				if newQuota < minQuota || newQuota > common.MaxQuota {
					return errors.New("user quota is outside the supported range after task settlement")
				}
				userUpdates["quota"] = newQuota
			}
			if err := tx.Model(&User{}).Where("id = ?", task.UserId).Updates(userUpdates).Error; err != nil {
				return err
			}
			if task.ChannelId > 0 {
				var channel Channel
				if err := lockForUpdate(tx).Select("id", "used_quota").Where("id = ?", task.ChannelId).First(&channel).Error; err != nil {
					return err
				}
				newChannelUsed := channel.UsedQuota + int64(delta)
				if delta > 0 && newChannelUsed < channel.UsedQuota || delta < 0 && newChannelUsed > channel.UsedQuota {
					return errors.New("channel usage counter overflow during task settlement")
				}
				if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).Update("used_quota", newChannelUsed).Error; err != nil {
					return err
				}
			}
			if tokenID > 0 {
				var token Token
				if err := lockForUpdate(tx).Select("id", "remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error; err != nil {
					return err
				}
				newRemain := token.RemainQuota - delta
				newTokenUsed := token.UsedQuota + delta
				if newRemain < minQuota || newRemain > common.MaxQuota || newTokenUsed < minQuota || newTokenUsed > common.MaxQuota {
					return errors.New("token quota is outside the supported range after task settlement")
				}
				if err := tx.Model(&Token{}).Where("id = ?", tokenID).Updates(map[string]any{
					"remain_quota": newRemain, "used_quota": newTokenUsed,
					"accessed_time": common.GetTimestamp(),
				}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&Task{}).Where("id = ?", task.ID).Update("quota", actualQuota).Error; err != nil {
			return err
		}
		if input.TaskPrivateData != "" {
			if err := tx.Model(&Task{}).Where("id = ?", task.ID).Update("private_data", input.TaskPrivateData).Error; err != nil {
				return err
			}
		}
		updates := map[string]any{
			"status": input.Status, "outcome": input.Outcome, "actual_quota": actualQuota,
			"usage_missing_reason": input.UsageMissingReason, "missing_usage_keys": input.MissingUsageKeys,
			"cache_sync_pending": common.RedisEnabled, "updated_at": time.Now().Unix(),
		}
		if err := tx.Model(&TaskBillingSettlement{}).Where("id = ?", settlement.ID).Updates(updates).Error; err != nil {
			return err
		}
		outbox := TaskBillingLogOutbox{
			OperationKey: taskBillingKey(input.OutboxOperationID), OperationID: input.OutboxOperationID,
			TaskID: input.TaskID, Payload: input.OutboxPayload, Status: TaskBillingLogOutboxPending,
			CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&outbox).Error; err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !committed {
		return false, nil
	}
	if common.RedisEnabled {
		cacheSyncErr := invalidateUserCache(before.UserId)
		if tokenKey != "" {
			if tokenErr := invalidateTokenCacheForMutation(tokenKey); cacheSyncErr == nil {
				cacheSyncErr = tokenErr
			}
		}
		if cacheSyncErr != nil {
			common.SysLog("failed to invalidate task settlement caches after commit: " + cacheSyncErr.Error())
			return true, nil
		}
		if err := DB.Model(&TaskBillingSettlement{}).Where("task_key = ?", taskBillingKey(input.TaskID)).Update("cache_sync_pending", false).Error; err != nil {
			common.SysLog("failed to clear task settlement cache-sync marker: " + err.Error())
		}
	}
	return true, nil
}

func EncodeTaskBillingPayload(value any) (string, error) {
	encoded, err := common.Marshal(value)
	return string(encoded), err
}

func GetPendingTaskBillingSettlements(limit int) ([]TaskBillingSettlement, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	candidateLimit := min(limit*5, 2500)
	var candidates []TaskBillingSettlement
	usageRetryBefore := time.Now().Add(-5 * time.Minute).Unix()
	err := DB.Where("status = ? OR (status = ? AND updated_at < ?)",
		TaskBillingSettlementPending, TaskBillingSettlementUsageUnavailable, usageRetryBefore).
		Order("id asc").Limit(candidateLimit).Find(&candidates).Error
	if err != nil || len(candidates) == 0 {
		return candidates, err
	}

	// Query completed task IDs separately, then match in memory. Comparing the
	// task_id columns in a subquery can fail on existing MySQL databases where
	// the tables were created with different utf8mb4 collations.
	completedTaskIDs := make(map[string]struct{}, len(candidates))
	for start := 0; start < len(candidates); start += 100 {
		end := min(start+100, len(candidates))
		taskIDs := make([]string, 0, end-start)
		for _, candidate := range candidates[start:end] {
			taskIDs = append(taskIDs, candidate.TaskID)
		}
		var completedIDs []string
		if err := DB.Model(&Task{}).Where("status = ?", TaskStatusSuccess).
			Where("task_id IN ?", taskIDs).Pluck("task_id", &completedIDs).Error; err != nil {
			return nil, err
		}
		for _, taskID := range completedIDs {
			completedTaskIDs[taskID] = struct{}{}
		}
	}

	settlements := make([]TaskBillingSettlement, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if _, ok := completedTaskIDs[candidate.TaskID]; !ok {
			continue
		}
		settlements = append(settlements, candidate)
		if len(settlements) == limit {
			break
		}
	}
	return settlements, nil
}

func HasTaskBillingSettlement(taskID string) bool {
	var count int64
	return DB.Model(&TaskBillingSettlement{}).Where("task_key = ?", taskBillingKey(taskID)).Count(&count).Error == nil && count > 0
}

func GetPendingTaskBillingCacheSync(limit int) ([]TaskBillingSettlement, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []TaskBillingSettlement
	err := DB.Where("cache_sync_pending = ?", true).Order("id asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func SyncTaskBillingSettlementCache(taskID string) error {
	task, err := GetTaskByTaskID(taskID)
	if err != nil {
		return err
	}
	tokenID := task.PrivateData.TokenId
	if tokenID <= 0 {
		tokenID = task.TokenId
	}
	if err := invalidateUserCache(task.UserId); err != nil {
		return err
	}
	if tokenID > 0 {
		var token Token
		if err := DB.Select("id", "key").First(&token, tokenID).Error; err != nil {
			return err
		}
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			return err
		}
	}
	return DB.Model(&TaskBillingSettlement{}).Where("task_key = ?", taskBillingKey(taskID)).Update("cache_sync_pending", false).Error
}

func GetTaskByTaskID(taskID string) (*Task, error) {
	var task Task
	err := DB.Where("task_id = ?", taskID).First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}
