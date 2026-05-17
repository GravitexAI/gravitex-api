package controller

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// moderationQueryRateLimiter limits per-channel calls to the upstream
// `GetModerationResult` API. BytePlus documents QPM=10 (PDF p.2). We use the
// existing in-memory limiter (single-process); multi-node deployments each
// get their own 10-QPM budget, which still stays well under the upstream cap
// in practice and is simpler than coordinating via Redis.
var (
	moderationQueryRateLimiter common.InMemoryRateLimiter
	moderationQueryLimiterOnce sync.Once
)

func ensureModerationQueryLimiterInited() {
	moderationQueryLimiterOnce.Do(func() {
		moderationQueryRateLimiter.Init(2 * time.Minute)
	})
}

// moderationQueryWindowSec is the rate-limit window. We use 60s + a 10/min
// quota to match the upstream QPM limit one-to-one.
const (
	moderationQueryWindowSec = int64(60)
	moderationQueryQPM       = 10
	// moderationQueryRetentionDays mirrors the upstream 14-day query window
	// (PDF p.2). Older tasks always return NotFound.Id from BytePlus.
	moderationQueryRetentionDays = 14
)

// taskDataModerationKey is the field inside task.Data we use to cache the
// upstream response so subsequent queries don't burn QPM. The key namespace
// is gateway-internal (prefixed with `_`) to avoid colliding with anything
// the upstream might return inside task.Data.
const taskDataModerationKey = "_moderation_result"

// cachedModerationResult is what we persist into task.Data and serve back to
// the client on subsequent queries.
type cachedModerationResult struct {
	BlockReasons []service.ByteplusModerationBlockReason `json:"block_reasons"`
	QueriedAt    int64                                   `json:"queried_at"`
}

// QueryModerationResult is the handler for
// GET /v1/video/generations/moderation-result/:task_id.
//
// It looks up a failed Seedance 2.0 task owned by the calling user, asks the
// upstream BytePlus `GetModerationResult` API why the request was blocked,
// caches the answer into task.Data, and returns it.
//
// The endpoint is opt-in per channel (`enable_moderation_query`) because
// BytePlus requires whitelist activation; calling without whitelist returns
// 404 NotFound.Id which would pollute logs.
func QueryModerationResult(c *gin.Context) {
	ensureModerationQueryLimiterInited()

	userId := c.GetInt("id")
	taskId := strings.TrimSpace(c.Param("task_id"))
	if taskId == "" {
		moderationErrorResponse(c, http.StatusBadRequest, "task_id is required", "invalid_request")
		return
	}

	task, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("QueryModerationResult: DB lookup failed: %s", err.Error()))
		moderationErrorResponse(c, http.StatusInternalServerError, "Failed to load task", "internal_error")
		return
	}
	if !exist || task == nil {
		moderationErrorResponse(c, http.StatusNotFound, "task not found", "not_found")
		return
	}

	if task.Status != model.TaskStatusFailure {
		moderationErrorResponse(c, http.StatusBadRequest,
			fmt.Sprintf("only failed tasks can be queried for moderation reasons (current status: %s)", task.Status),
			"invalid_task_status")
		return
	}

	if !isSeedance20Family(task) {
		moderationErrorResponse(c, http.StatusBadRequest,
			"moderation query is only supported for seedance-2-0 / seedance-2-0-fast models",
			"unsupported_model")
		return
	}

	// Cache hit short-circuits BEFORE the 14-day window check: the window is an
	// upstream restriction, but a locally cached answer is still authoritative
	// and should always be served regardless of task age.
	if cached := readCachedModerationResult(task); cached != nil {
		respondModerationResult(c, task.TaskID, cached.BlockReasons, true, cached.QueriedAt)
		return
	}

	// 14-day retention check (PDF p.2) — fail fast before burning a QPM slot.
	if task.CreatedAt > 0 {
		ageSec := time.Now().Unix() - task.CreatedAt
		if ageSec > int64(moderationQueryRetentionDays)*86400 {
			moderationErrorResponse(c, http.StatusBadRequest,
				fmt.Sprintf("task exceeds the %d-day moderation query window", moderationQueryRetentionDays),
				"query_window_expired")
			return
		}
	}

	ch, err := model.CacheGetChannel(task.ChannelId)
	if err != nil || ch == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("QueryModerationResult: channel %d not found: %v", task.ChannelId, err))
		moderationErrorResponse(c, http.StatusServiceUnavailable, "channel not available", "channel_not_found")
		return
	}
	other := ch.GetOtherSettings()
	if !other.IsModerationQueryEnabled() {
		moderationErrorResponse(c, http.StatusForbidden,
			"moderation query is not enabled on this channel; please contact admin to apply for BytePlus whitelist activation",
			"channel_not_whitelisted")
		return
	}

	rlKey := fmt.Sprintf("moderation_query:channel:%d", ch.Id)
	if !moderationQueryRateLimiter.Request(rlKey, moderationQueryQPM, moderationQueryWindowSec) {
		moderationErrorResponse(c, http.StatusTooManyRequests,
			fmt.Sprintf("rate limit exceeded (%d QPM per channel), please retry later", moderationQueryQPM),
			"rate_limited")
		return
	}

	cfg := getByteplusAssetConfig(ch)
	reasons, err := service.ByteplusGetModerationResult(cfg, task.TaskID, service.ByteplusModerationIdTypeTaskId)
	if err != nil {
		if service.IsByteplusNotFoundError(err) {
			// PDF p.3 lists three causes for 404 NotFound.Id; surface them
			// together because the upstream doesn't tell us which one applies.
			moderationErrorResponse(c, http.StatusNotFound,
				"no moderation result found: task may not have been blocked by moderation, the ID is invalid, or the channel is not yet whitelisted",
				"not_found")
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("QueryModerationResult: upstream error task=%s channel=%d: %s",
			task.TaskID, ch.Id, err.Error()))
		moderationErrorResponse(c, http.StatusBadGateway,
			"failed to query moderation result from upstream: "+err.Error(),
			"upstream_error")
		return
	}

	now := time.Now().Unix()
	if err := writeCachedModerationResult(task, reasons, now); err != nil {
		// 写缓存失败不阻断响应，仅记日志；下次请求会重新查 upstream。
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("QueryModerationResult: cache write failed task=%s: %s",
			task.TaskID, err.Error()))
	}

	// Audit log — opt-in endpoint, useful for ops to trace who triggered upstream calls.
	logger.LogInfo(c.Request.Context(),
		fmt.Sprintf("QueryModerationResult: upstream hit user=%d channel=%d task=%s reasons=%d",
			userId, ch.Id, task.TaskID, len(reasons)))

	respondModerationResult(c, task.TaskID, reasons, false, now)
}

// isSeedance20Family returns true if the task's origin or upstream model name
// belongs to the seedance-2-0 series (PDF p.2 restricts the API to these
// models). We compare case-insensitively against substrings to accommodate
// both client-facing aliases (`seedance-2-0`) and the upstream IDs registered
// in [relay/channel/task/doubao/constants.go] (`doubao-seedance-2-0-260128`).
func isSeedance20Family(task *model.Task) bool {
	if task == nil {
		return false
	}
	candidates := []string{
		strings.ToLower(task.Properties.OriginModelName),
		strings.ToLower(task.Properties.UpstreamModelName),
	}
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if strings.Contains(name, "seedance-2-0") {
			return true
		}
	}
	return false
}

// readCachedModerationResult returns a cached moderation result from
// task.Data if one was previously persisted, or nil otherwise.
func readCachedModerationResult(task *model.Task) *cachedModerationResult {
	if task == nil || len(task.Data) == 0 {
		return nil
	}
	var dataMap map[string]any
	if err := common.Unmarshal(task.Data, &dataMap); err != nil {
		return nil
	}
	raw, ok := dataMap[taskDataModerationKey]
	if !ok || raw == nil {
		return nil
	}
	rawBytes, err := common.Marshal(raw)
	if err != nil {
		return nil
	}
	var cached cachedModerationResult
	if err := common.Unmarshal(rawBytes, &cached); err != nil {
		return nil
	}
	return &cached
}

// writeCachedModerationResult merges the moderation result into task.Data and
// persists only the `data` column to avoid touching `quota` (which has a
// separate atomic guard in [controller/task_video.go]).
func writeCachedModerationResult(task *model.Task, reasons []service.ByteplusModerationBlockReason, queriedAt int64) error {
	dataMap := make(map[string]any)
	if len(task.Data) > 0 {
		if err := common.Unmarshal(task.Data, &dataMap); err != nil {
			dataMap = make(map[string]any)
		}
	}
	dataMap[taskDataModerationKey] = cachedModerationResult{
		BlockReasons: reasons,
		QueriedAt:    queriedAt,
	}
	merged, err := common.Marshal(dataMap)
	if err != nil {
		return err
	}
	task.Data = merged
	return model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("data", merged).Error
}

// respondModerationResult writes the JSON envelope returned to the client.
// The shape mirrors what BytePlus returns (PDF p.3) so downstream UIs can
// reuse the same parsing logic for both gateway-cached and live responses.
func respondModerationResult(c *gin.Context, taskId string,
	reasons []service.ByteplusModerationBlockReason, cached bool, queriedAt int64) {
	if reasons == nil {
		reasons = []service.ByteplusModerationBlockReason{}
	}
	c.JSON(http.StatusOK, gin.H{
		"task_id":       taskId,
		"block_reasons": reasons,
		"cached":        cached,
		"queried_at":    queriedAt,
	})
}

// moderationErrorResponse emits a structured error consistent with other
// gateway endpoints (`error.message` + `error.type` is what existing clients
// already parse).
func moderationErrorResponse(c *gin.Context, status int, message, errType string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}
