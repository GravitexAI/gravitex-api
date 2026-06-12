package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const (
	quotaDataStreamName      = "stream:quota_data:v1"
	quotaDataStreamGroup     = "quota_data_workers"
	quotaDataStreamDLQ       = "stream:quota_data:dlq:v1"
	quotaDataBackfillLockKey = "quota_data:stream:backfill:lock"
	quotaDataBackfillCursor  = "quota_data:stream:last_scan_log_id"
)

var quotaStreamStartOnce sync.Once
var quotaStreamAutoClaimUnsupported sync.Once

// quotaStreamEvent 是 Redis Stream 内部使用的消费事件。
// 默认优先使用应用侧生成的 event_id 做幂等；
// 对历史数据或非 ClickHouse 场景，再回退到 log id。
type quotaStreamEvent struct {
	EventID     string
	LogID       int
	UserID      int
	Username    string
	ModelName   string
	Quota       int
	TokenUsed   int
	BucketTS    int64
	RequestID   string
	CreatedAt   int64
	EventType   int
	EventSource string
}

func IsQuotaDataStreamEnabled() bool {
	if !common.QuotaDataStreamEnabled {
		return false
	}
	if !common.RedisEnabled || common.RDB == nil {
		return false
	}
	return true
}

func StartQuotaDataStreamWorkers() {
	if !IsQuotaDataStreamEnabled() {
		if common.QuotaDataStreamEnabled {
			common.SysLog("[QuotaStream] disabled at runtime because Redis prerequisites are not met")
		}
		return
	}

	quotaStreamStartOnce.Do(func() {
		if err := ensureQuotaDataStreamGroup(); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream] failed to ensure consumer group: %v", err))
			return
		}
		if err := ensureQuotaDataBackfillCursorInitialized(); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream] failed to initialize backfill cursor: %v", err))
			return
		}
		common.SysLog("[QuotaStream] stream workers enabled")
		for i := 0; i < maxInt(common.QuotaDataStreamConsumerCount, 1); i++ {
			consumerName := quotaStreamConsumerName(i)
			gopool.Go(func() {
				runQuotaDataStreamConsumer(consumerName)
			})
		}
		gopool.Go(runQuotaDataPendingReclaimer)
		if common.IsMasterNode {
			gopool.Go(runQuotaDataBackfillLoop)
		}
	})
}

func QueueConsumeLogToQuotaStream(log *Log, source string) {
	if !IsQuotaDataStreamEnabled() {
		return
	}
	if log == nil || log.Type != LogTypeConsume {
		return
	}
	event := buildQuotaStreamEvent(log)
	if event.EventID == "" {
		common.SysLog(fmt.Sprintf("[QuotaStream][Producer] skip source=%s because event id is not available requestId=%s model=%s", source, log.RequestId, log.ModelName))
		return
	}
	if !isQuotaDataStreamTargetLog(log) {
		return
	}
	if err := enqueueQuotaStreamEvent(context.Background(), event, source); err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Producer] enqueue failed source=%s eventId=%s logId=%d requestId=%s model=%s err=%v",
			source, event.EventID, log.Id, log.RequestId, log.ModelName, err))
		return
	}
	common.SysLog(fmt.Sprintf("[QuotaStream][Producer] enqueue ok source=%s eventId=%s logId=%d requestId=%s model=%s quota=%d bucket=%d",
		source, event.EventID, log.Id, log.RequestId, log.ModelName, log.Quota, hourBucket(log.CreatedAt)))
}

func buildQuotaStreamEvent(log *Log) quotaStreamEvent {
	return quotaStreamEvent{
		EventID:     getQuotaStreamEventIDFromLog(log),
		LogID:       log.Id,
		UserID:      log.UserId,
		Username:    log.Username,
		ModelName:   log.ModelName,
		Quota:       log.Quota,
		TokenUsed:   log.PromptTokens + log.CompletionTokens,
		BucketTS:    hourBucket(log.CreatedAt),
		RequestID:   log.RequestId,
		CreatedAt:   log.CreatedAt,
		EventType:   log.Type,
		EventSource: "log_insert",
	}
}

func enqueueQuotaStreamEvent(ctx context.Context, event quotaStreamEvent, source string) error {
	args := &redis.XAddArgs{
		Stream: quotaDataStreamName,
		Approx: true,
		MaxLen: int64(maxInt(common.QuotaDataStreamMaxLen, 1)),
		Values: map[string]interface{}{
			"event_id":     event.EventID,
			"log_id":       event.LogID,
			"user_id":      event.UserID,
			"username":     event.Username,
			"model_name":   event.ModelName,
			"quota":        event.Quota,
			"token_used":   event.TokenUsed,
			"bucket_ts":    event.BucketTS,
			"request_id":   event.RequestID,
			"created_at":   event.CreatedAt,
			"event_type":   event.EventType,
			"event_source": source,
		},
	}
	_, err := common.RDB.XAdd(ctx, args).Result()
	return err
}

func runQuotaDataStreamConsumer(consumerName string) {
	common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] start consumer=%s", consumerName))
	for {
		if !IsQuotaDataStreamEnabled() {
			time.Sleep(5 * time.Second)
			continue
		}
		ctx := context.Background()
		res, err := common.RDB.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    quotaDataStreamGroup,
			Consumer: consumerName,
			Streams:  []string{quotaDataStreamName, ">"},
			Count:    int64(maxInt(common.QuotaDataStreamBatchSize, 1)),
			Block:    time.Duration(maxInt(common.QuotaDataStreamBlockMs, 1)) * time.Millisecond,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			common.SysError(fmt.Sprintf("[QuotaStream][Consumer] read failed consumer=%s err=%v", consumerName, err))
			time.Sleep(2 * time.Second)
			continue
		}
		for _, stream := range res {
			for _, message := range stream.Messages {
				processQuotaStreamMessage(ctx, consumerName, message)
			}
		}
	}
}

func processQuotaStreamMessage(ctx context.Context, consumerName string, message redis.XMessage) {
	event, err := quotaStreamEventFromMessage(message)
	if err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Consumer] decode failed consumer=%s messageId=%s err=%v", consumerName, message.ID, err))
		moveQuotaStreamMessageToDLQ(ctx, consumerName, message, err, 0)
		ackQuotaStreamMessage(ctx, consumerName, message.ID)
		return
	}

	lockKey := quotaDataEventLockKey(event.EventID)
	doneKey := quotaDataEventDoneKey(event.EventID)

	done, err := common.RDB.Exists(ctx, doneKey).Result()
	if err == nil && done > 0 {
		common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] duplicate ack consumer=%s messageId=%s eventId=%s logId=%d", consumerName, message.ID, event.EventID, event.LogID))
		ackQuotaStreamMessage(ctx, consumerName, message.ID)
		return
	}

	locked, err := common.RDB.SetNX(ctx, lockKey, consumerName, time.Duration(maxInt(common.QuotaDataStreamLockTTLSeconds, 30))*time.Second).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Consumer] lock failed consumer=%s messageId=%s eventId=%s logId=%d err=%v", consumerName, message.ID, event.EventID, event.LogID, err))
		return
	}
	if !locked {
		common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] skip busy lock consumer=%s messageId=%s eventId=%s logId=%d", consumerName, message.ID, event.EventID, event.LogID))
		return
	}
	defer func() {
		if err := common.RDB.Del(ctx, lockKey).Err(); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream][Consumer] unlock failed consumer=%s messageId=%s eventId=%s logId=%d err=%v", consumerName, message.ID, event.EventID, event.LogID, err))
		}
	}()

	done, err = common.RDB.Exists(ctx, doneKey).Result()
	if err == nil && done > 0 {
		common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] duplicate ack after lock consumer=%s messageId=%s eventId=%s logId=%d", consumerName, message.ID, event.EventID, event.LogID))
		ackQuotaStreamMessage(ctx, consumerName, message.ID)
		return
	}

	if err := applyQuotaStreamEvent(event); err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Consumer] apply failed consumer=%s messageId=%s eventId=%s logId=%d err=%v", consumerName, message.ID, event.EventID, event.LogID, err))
		return
	}
	if err := common.RDB.Set(ctx, doneKey, "1", time.Duration(maxInt(common.QuotaDataStreamDoneTTLHours, 1))*time.Hour).Err(); err != nil {
		// 这里保留详细注释：不新增表时，done 标记与 quota_data 更新无法做单事务。
		// 如果此处写入失败，消息会在后续重试时被再次处理，因此只记录错误，不立即 ack。
		common.SysError(fmt.Sprintf("[QuotaStream][Consumer] done mark failed consumer=%s messageId=%s eventId=%s logId=%d err=%v", consumerName, message.ID, event.EventID, event.LogID, err))
		return
	}

	common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] apply ok consumer=%s messageId=%s eventId=%s logId=%d userId=%d model=%s quota=%d bucket=%d",
		consumerName, message.ID, event.EventID, event.LogID, event.UserID, event.ModelName, event.Quota, event.BucketTS))
	ackQuotaStreamMessage(ctx, consumerName, message.ID)
}

func applyQuotaStreamEvent(event quotaStreamEvent) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		quotaDataDB := &QuotaData{}
		err := tx.Table("quota_data").Where("user_id = ? and username = ? and model_name = ? and created_at = ?",
			event.UserID, event.Username, event.ModelName, event.BucketTS).First(quotaDataDB).Error
		if err == nil && quotaDataDB.Id > 0 {
			return tx.Table("quota_data").Where("id = ?", quotaDataDB.Id).Updates(map[string]interface{}{
				"count":      gorm.Expr("count + ?", 1),
				"quota":      gorm.Expr("quota + ?", event.Quota),
				"token_used": gorm.Expr("token_used + ?", event.TokenUsed),
			}).Error
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		quotaData := &QuotaData{
			UserID:    event.UserID,
			Username:  event.Username,
			ModelName: event.ModelName,
			CreatedAt: event.BucketTS,
			TokenUsed: event.TokenUsed,
			Count:     1,
			Quota:     event.Quota,
		}
		return tx.Table("quota_data").Create(quotaData).Error
	})
}

func runQuotaDataPendingReclaimer() {
	consumerName := quotaStreamConsumerName(-1)
	common.SysLog(fmt.Sprintf("[QuotaStream][Reclaimer] start consumer=%s", consumerName))
	for {
		if !IsQuotaDataStreamEnabled() {
			time.Sleep(10 * time.Second)
			continue
		}
		ctx := context.Background()
		messages, _, err := common.RDB.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   quotaDataStreamName,
			Group:    quotaDataStreamGroup,
			Consumer: consumerName,
			MinIdle:  time.Duration(maxInt(common.QuotaDataStreamPendingIdleMs, 1000)) * time.Millisecond,
			Start:    "0-0",
			Count:    int64(maxInt(common.QuotaDataStreamBatchSize, 1)),
		}).Result()
		if err != nil && err != redis.Nil {
			if isRedisUnknownCommandErr(err, "xautoclaim") {
				quotaStreamAutoClaimUnsupported.Do(func() {
					common.SysLog("[QuotaStream][Reclaimer] XAUTOCLAIM is not supported by current Redis, fallback to XPENDING + XCLAIM")
				})
				claimMessages, claimErr := reclaimPendingMessagesWithXClaim(ctx, consumerName)
				if claimErr != nil {
					common.SysError(fmt.Sprintf("[QuotaStream][Reclaimer] xclaim fallback failed err=%v", claimErr))
					time.Sleep(3 * time.Second)
					continue
				}
				messages = claimMessages
			} else {
				common.SysError(fmt.Sprintf("[QuotaStream][Reclaimer] autoclaim failed err=%v", err))
				time.Sleep(3 * time.Second)
				continue
			}
		}
		for _, message := range messages {
			retryCount := quotaStreamRetryCount(ctx, message.ID)
			if retryCount > int64(maxInt(common.QuotaDataStreamRetryLimit, 1)) {
				common.SysError(fmt.Sprintf("[QuotaStream][Reclaimer] move to dlq messageId=%s retry=%d", message.ID, retryCount))
				moveQuotaStreamMessageToDLQ(ctx, consumerName, message, fmt.Errorf("retry limit exceeded"), retryCount)
				ackQuotaStreamMessage(ctx, consumerName, message.ID)
				continue
			}
			processQuotaStreamMessage(ctx, consumerName, message)
		}
		time.Sleep(2 * time.Second)
	}
}

func reclaimPendingMessagesWithXClaim(ctx context.Context, consumerName string) ([]redis.XMessage, error) {
	minIdle := time.Duration(maxInt(common.QuotaDataStreamPendingIdleMs, 1000)) * time.Millisecond
	pendingItems, err := common.RDB.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: quotaDataStreamName,
		Group:  quotaDataStreamGroup,
		Start:  "-",
		End:    "+",
		Count:  int64(maxInt(common.QuotaDataStreamBatchSize, 1)),
	}).Result()
	if err != nil {
		return nil, err
	}

	messageIDs := make([]string, 0, len(pendingItems))
	for _, item := range pendingItems {
		if item.Idle >= minIdle {
			messageIDs = append(messageIDs, item.ID)
		}
	}
	if len(messageIDs) == 0 {
		return nil, nil
	}

	return common.RDB.XClaim(ctx, &redis.XClaimArgs{
		Stream:   quotaDataStreamName,
		Group:    quotaDataStreamGroup,
		Consumer: consumerName,
		MinIdle:  minIdle,
		Messages: messageIDs,
	}).Result()
}

func runQuotaDataBackfillLoop() {
	common.SysLog("[QuotaStream][Backfill] start leader loop")
	for {
		if !IsQuotaDataStreamEnabled() {
			time.Sleep(10 * time.Second)
			continue
		}
		runQuotaDataBackfillOnce()
		time.Sleep(time.Duration(maxInt(common.QuotaDataStreamBackfillIntervalSeconds, 5)) * time.Second)
	}
}

func runQuotaDataBackfillOnce() {
	ctx := context.Background()
	lockTTL := time.Duration(maxInt(common.QuotaDataStreamBackfillIntervalSeconds*2, 10)) * time.Second
	ok, err := common.RDB.SetNX(ctx, quotaDataBackfillLockKey, common.NodeName, lockTTL).Result()
	if err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Backfill] acquire lock failed err=%v", err))
		return
	}
	if !ok {
		return
	}

	lastID, err := getQuotaBackfillCursor(ctx)
	if err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Backfill] read cursor failed err=%v", err))
		return
	}
	if lastID == 0 {
		if err := ensureQuotaDataBackfillCursorInitialized(); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream][Backfill] init cursor failed err=%v", err))
		}
		return
	}

	var logs []*Log
	err = LOG_DB.Where("id > ? AND type = ?", lastID, LogTypeConsume).
		Order("id asc").
		Limit(maxInt(common.QuotaDataStreamBackfillBatch, 1)).
		Find(&logs).Error
	if err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Backfill] query logs failed cursor=%d err=%v", lastID, err))
		return
	}
	if len(logs) == 0 {
		return
	}

	nextCursor := lastID
	for _, logItem := range logs {
		if logItem == nil || !isQuotaDataStreamTargetLog(logItem) {
			nextCursor = logItem.Id
			continue
		}
		event := buildQuotaStreamEvent(logItem)
		if err := enqueueQuotaStreamEvent(ctx, event, "backfill"); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream][Backfill] enqueue failed eventId=%s logId=%d requestId=%s model=%s err=%v",
				event.EventID, logItem.Id, logItem.RequestId, logItem.ModelName, err))
			break
		}
		nextCursor = logItem.Id
		common.SysLog(fmt.Sprintf("[QuotaStream][Backfill] enqueue ok eventId=%s logId=%d requestId=%s model=%s",
			event.EventID, logItem.Id, logItem.RequestId, logItem.ModelName))
	}
	if nextCursor > lastID {
		if err := common.RDB.Set(ctx, quotaDataBackfillCursor, strconv.Itoa(nextCursor), 0).Err(); err != nil {
			common.SysError(fmt.Sprintf("[QuotaStream][Backfill] update cursor failed cursor=%d err=%v", nextCursor, err))
		}
	}
}

func ensureQuotaDataStreamGroup() error {
	ctx := context.Background()
	err := common.RDB.XGroupCreateMkStream(ctx, quotaDataStreamName, quotaDataStreamGroup, "0").Err()
	if err == nil {
		common.SysLog("[QuotaStream] created consumer group quota_data_workers")
		return nil
	}
	if strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func ensureQuotaDataBackfillCursorInitialized() error {
	ctx := context.Background()
	exists, err := common.RDB.Exists(ctx, quotaDataBackfillCursor).Result()
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	maxID, err := getCurrentMaxConsumeLogID()
	if err != nil {
		return err
	}
	if err := common.RDB.Set(ctx, quotaDataBackfillCursor, strconv.Itoa(maxID), 0).Err(); err != nil {
		return err
	}
	common.SysLog(fmt.Sprintf("[QuotaStream][Backfill] initialized cursor at current max log id=%d", maxID))
	return nil
}

func quotaStreamEventFromMessage(message redis.XMessage) (quotaStreamEvent, error) {
	event := quotaStreamEvent{}
	var err error
	event.EventID = streamValueToString(message.Values["event_id"])
	event.LogID, err = streamValueToInt(message.Values["log_id"])
	if err != nil {
		event.LogID = 0
	}
	if event.EventID == "" {
		if event.LogID > 0 {
			event.EventID = legacyQuotaStreamEventID(event.LogID)
		} else {
			return event, fmt.Errorf("parse event_id: empty")
		}
	}
	event.UserID, err = streamValueToInt(message.Values["user_id"])
	if err != nil {
		return event, fmt.Errorf("parse user_id: %w", err)
	}
	event.Username = streamValueToString(message.Values["username"])
	event.ModelName = streamValueToString(message.Values["model_name"])
	event.Quota, err = streamValueToInt(message.Values["quota"])
	if err != nil {
		return event, fmt.Errorf("parse quota: %w", err)
	}
	event.TokenUsed, err = streamValueToInt(message.Values["token_used"])
	if err != nil {
		return event, fmt.Errorf("parse token_used: %w", err)
	}
	event.BucketTS, err = streamValueToInt64(message.Values["bucket_ts"])
	if err != nil {
		return event, fmt.Errorf("parse bucket_ts: %w", err)
	}
	event.RequestID = streamValueToString(message.Values["request_id"])
	event.CreatedAt, err = streamValueToInt64(message.Values["created_at"])
	if err != nil {
		return event, fmt.Errorf("parse created_at: %w", err)
	}
	event.EventType, err = streamValueToInt(message.Values["event_type"])
	if err != nil {
		return event, fmt.Errorf("parse event_type: %w", err)
	}
	event.EventSource = streamValueToString(message.Values["event_source"])
	return event, nil
}

func moveQuotaStreamMessageToDLQ(ctx context.Context, consumerName string, message redis.XMessage, reason error, retryCount int64) {
	values := make(map[string]interface{}, len(message.Values)+4)
	for k, v := range message.Values {
		values[k] = v
	}
	values["failed_message_id"] = message.ID
	values["failed_consumer"] = consumerName
	values["failed_reason"] = reason.Error()
	values["retry_count"] = retryCount
	if _, err := common.RDB.XAdd(ctx, &redis.XAddArgs{Stream: quotaDataStreamDLQ, Values: values}).Result(); err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][DLQ] write failed messageId=%s err=%v", message.ID, err))
	}
}

func ackQuotaStreamMessage(ctx context.Context, consumerName string, messageID string) {
	if err := common.RDB.XAck(ctx, quotaDataStreamName, quotaDataStreamGroup, messageID).Err(); err != nil {
		common.SysError(fmt.Sprintf("[QuotaStream][Consumer] ack failed consumer=%s messageId=%s err=%v", consumerName, messageID, err))
		return
	}
	common.SysLog(fmt.Sprintf("[QuotaStream][Consumer] ack ok consumer=%s messageId=%s", consumerName, messageID))
}

func quotaStreamRetryCount(ctx context.Context, messageID string) int64 {
	result, err := common.RDB.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: quotaDataStreamName,
		Group:  quotaDataStreamGroup,
		Start:  messageID,
		End:    messageID,
		Count:  1,
	}).Result()
	if err != nil || len(result) == 0 {
		return 0
	}
	return result[0].RetryCount
}

func getQuotaBackfillCursor(ctx context.Context) (int, error) {
	value, err := common.RDB.Get(ctx, quotaDataBackfillCursor).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func getCurrentMaxConsumeLogID() (int, error) {
	var logItem Log
	err := LOG_DB.Where("type = ?", LogTypeConsume).Order("id desc").First(&logItem).Error
	if err == gorm.ErrRecordNotFound {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return logItem.Id, nil
}

func isQuotaDataStreamTargetLog(log *Log) bool {
	if log == nil {
		return false
	}
	return log.Type == LogTypeConsume && log.ModelName != ""
}

func streamValueToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func streamValueToInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		n, err := strconv.Atoi(v)
		return n, err
	case []byte:
		n, err := strconv.Atoi(string(v))
		return n, err
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func streamValueToInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", value)
	}
}

func quotaDataEventLockKey(eventID string) string {
	return fmt.Sprintf("quota_data:stream:lock:%s", eventID)
}

func quotaDataEventDoneKey(eventID string) string {
	return fmt.Sprintf("quota_data:stream:done:%s", eventID)
}

func quotaStreamConsumerName(index int) string {
	name := common.NodeName
	if name == "" {
		name = "node"
	}
	if index >= 0 {
		return fmt.Sprintf("%s-consumer-%d", name, index)
	}
	return fmt.Sprintf("%s-reclaimer", name)
}

func hourBucket(ts int64) int64 {
	return ts - (ts % 3600)
}

func getQuotaStreamEventIDFromLog(log *Log) string {
	if log == nil {
		return ""
	}
	otherMap, _ := common.StrToMap(log.Other)
	if otherMap != nil {
		if eventID, ok := otherMap["quota_stream_event_id"].(string); ok && eventID != "" {
			return eventID
		}
	}
	if log.Id > 0 {
		return legacyQuotaStreamEventID(log.Id)
	}
	return ""
}

func legacyQuotaStreamEventID(logID int) string {
	return fmt.Sprintf("legacy-log-%d", logID)
}

func isRedisUnknownCommandErr(err error, command string) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "unknown command") && strings.Contains(errMsg, strings.ToLower(command))
}

func maxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
