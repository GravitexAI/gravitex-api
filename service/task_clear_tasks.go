package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
)

const (
	taskClearTickInterval = 1 * time.Hour
	taskClearBatchSize    = 10
	taskClearMaxRunTime   = 30 * time.Minute
	taskClearCursorKey    = "task_clear_tasks:last_id"
	taskExpiredReason     = "The task has expired"
	taskClearTTLSeconds   = 24 * 3600
)

var (
	taskClearOnce    sync.Once
	taskClearRunning atomic.Bool
)

// taskClearCandidate 只读取清理任务所需的最小字段，避免每批扫描带出大 JSON 列。
// 这里查询出来的已经是“候选集”，也就是满足时间和状态前置条件的数据。
type taskClearCandidate struct {
	ID int64
}

func StartTaskClearTask() {
	taskClearOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		if !common.RedisEnabled || common.RDB == nil {
			logger.LogWarn(context.Background(), "task clear task skipped: redis is required for cursor persistence")
			return
		}

		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("task clear task started: tick=%s batch=%d max_run=%s", taskClearTickInterval, taskClearBatchSize, taskClearMaxRunTime))
			ticker := time.NewTicker(taskClearTickInterval)
			defer ticker.Stop()

			runTaskClearTasksOnce()
			for range ticker.C {
				runTaskClearTasksOnce()
			}
		})
	})
}

func runTaskClearTasksOnce() {
	if !taskClearRunning.CompareAndSwap(false, true) {
		logger.LogInfo(context.Background(), "task clear task skipped: previous run still in progress")
		return
	}
	defer taskClearRunning.Store(false)

	ctx := context.Background()
	startAt := time.Now()
	deadline := startAt.Add(taskClearMaxRunTime)
	now := model.GetDBTimestamp()
	cutoff := now - taskClearTTLSeconds

	lastID, err := getTaskClearCursor()
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("task clear task: load cursor failed, fallback to 0: %v", err))
		lastID = 0
	}

	logger.LogInfo(ctx, fmt.Sprintf("task clear task: begin base64 candidate scan from cursor=%d cutoff=%d", lastID, cutoff))

	var (
		totalScanned int
		totalUpdated int
		loopCount    int
	)

	for {
		// 超过 30 分钟后不再开始新一轮批处理，但会让当前批次自然结束并保存游标。
		if time.Now().After(deadline) {
			logger.LogInfo(ctx, fmt.Sprintf("task clear task: reached max runtime %s, stop after current progress", taskClearMaxRunTime))
			break
		}

		loopCount++
		batch, err := loadTaskClearBatch(lastID, cutoff)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("task clear task: load batch failed at cursor=%d: %v", lastID, err))
			break
		}

		// base64 候选集扫到末尾时，将游标重置为 0。
		// 下个周期会从候选集头部重新开始，避免新进入“超过一天”窗口的旧记录被永久跳过。
		if len(batch) == 0 {
			if lastID > 0 {
				if err = setTaskClearCursor(0); err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("task clear task: reset cursor to 0 failed: %v", err))
					break
				}
				logger.LogInfo(ctx, fmt.Sprintf("task clear task: reached base64 candidate set end at cursor=%d, reset cursor to 0", lastID))
			} else {
				logger.LogInfo(ctx, "task clear task: no matched base64 candidate rows from cursor=0, skip")
			}
			break
		}

		totalScanned += len(batch)
		lastScannedID := batch[len(batch)-1].ID
		updateIDs := collectCandidateIDs(batch)

		err = model.TaskBulkUpdateByID(updateIDs, map[string]any{
			"fail_reason": taskExpiredReason,
			"updated_at":  now,
		})
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("task clear task: update failed for ids=%v: %v", updateIDs, err))
			break
		}
		totalUpdated += len(updateIDs)
		logger.LogInfo(ctx, fmt.Sprintf("task clear task: batch=%d scanned_base64_candidates=%d updated=%d cursor %d -> %d", loopCount, len(batch), len(updateIDs), lastID, lastScannedID))

		if err = setTaskClearCursor(lastScannedID); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("task clear task: save cursor failed at %d: %v", lastScannedID, err))
			break
		}
		lastID = lastScannedID
	}

	logger.LogInfo(ctx, fmt.Sprintf(
		"task clear task: finished scanned=%d updated=%d final_cursor=%d duration=%s",
		totalScanned,
		totalUpdated,
		lastID,
		time.Since(startAt).Truncate(time.Second),
	))
}

func loadTaskClearBatch(lastID int64, cutoff int64) ([]taskClearCandidate, error) {
	var batch []taskClearCandidate
	// 先用状态、大 base64 特征和“超过一天”的时间条件缩小候选集，再按 id 游标分页。
	// 这样游标推进的是“待清理的大 base64 候选数据”，不会随着整张 tasks 表增长而越来越慢。
	// 这里专门锁定 data:...;base64,... 且长度较大的结果，避免把普通短文本 fail_reason 混进巡检游标。
	err := model.DB.Model(&model.Task{}).
		Select("id").
		Where("id > ?", lastID).
		Where("status = ?", model.TaskStatusSuccess).
		Where("fail_reason IS NOT NULL").
		Where("fail_reason <> ?", "").
		Where("fail_reason <> ?", taskExpiredReason).
		Where("LOWER(fail_reason) NOT LIKE ?", "http%").
		Where("LOWER(fail_reason) LIKE ?", "data:%;base64,%").
		Where("LENGTH(fail_reason) > ?", 1000).
		Where(
			"(finish_time > 0 AND finish_time < ?) OR "+
				"(finish_time <= 0 AND submit_time > 0 AND submit_time < ?) OR "+
				"(finish_time <= 0 AND submit_time <= 0 AND created_at > 0 AND created_at < ?)",
			cutoff, cutoff, cutoff,
		).
		Order("id ASC").
		Limit(taskClearBatchSize).
		Find(&batch).Error
	return batch, err
}

func collectCandidateIDs(batch []taskClearCandidate) []int64 {
	ids := make([]int64, 0, len(batch))
	for _, item := range batch {
		ids = append(ids, item.ID)
	}
	return ids
}

func getTaskClearCursor() (int64, error) {
	raw, err := common.RedisGet(taskClearCursorKey)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func setTaskClearCursor(id int64) error {
	return common.RedisSet(taskClearCursorKey, strconv.FormatInt(id, 10), 0)
}
