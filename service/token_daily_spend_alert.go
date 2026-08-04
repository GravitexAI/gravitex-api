package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// dailySpendAlertRedisKeyPrefix 是 token 日消费告警去重锁的 key 前缀，
// 每个 token 每天最多触发一次告警，key 中带日期分量。
const dailySpendAlertRedisKeyPrefix = "token:daily_spend_alert:"

// getDailySpendAlertRedisKey 构建"token 每日消费告警"去重锁的 Redis key，
// 形如 token:daily_spend_alert:{tokenId}:{yyyymmdd}。
func getDailySpendAlertRedisKey(tokenId int) string {
	return fmt.Sprintf("%s%d:%s", dailySpendAlertRedisKeyPrefix, tokenId, time.Now().Format("20060102"))
}

// acquireDailySpendAlertLock 尝试获取"该 token 今天"的告警去重锁，24 小时后自动过期。
// Redis 未启用时直接放行（不做去重），与 acquireQuotaWarningNotifyLock 的降级策略一致。
func acquireDailySpendAlertLock(redisKey string) (bool, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return true, nil
	}
	ok, err := common.RDB.SetNX(context.Background(), redisKey, "1", 24*time.Hour).Result()
	if err != nil {
		return false, fmt.Errorf("set daily spend alert redis key: %w", err)
	}
	return ok, nil
}

// CheckAndAlertDailySpend 检查指定 token 今日消费是否超过其日消费告警阈值，超过则
// best-effort 通知 Java 后端向该企业的告警收件人发送邮件（复用敏感操作告警回调）。
//
// threshold<=0 表示未开启该 token 的日消费告警，直接跳过。
// 每个 token 每天最多触发一次（Redis 锁去重）；Redis 未启用时不做去重，可能重复告警。
// 本函数只做检测与告警，不影响计费主流程；所有失败路径均只记录日志。
func CheckAndAlertDailySpend(tokenId int, userId int, tokenName string, threshold int) {
	if threshold <= 0 {
		return
	}

	spend, err := model.GetTokenDailySpend(tokenId)
	if err != nil {
		common.SysError(fmt.Sprintf("check token daily spend failed, tokenId=%d: %s", tokenId, err.Error()))
		return
	}
	if spend < int64(threshold) {
		return
	}

	redisKey := getDailySpendAlertRedisKey(tokenId)
	locked, err := acquireDailySpendAlertLock(redisKey)
	if err != nil {
		common.SysError(fmt.Sprintf("acquire daily spend alert lock failed, tokenId=%d: %s", tokenId, err.Error()))
		return
	}
	if !locked {
		return
	}

	htmlContent := fmt.Sprintf(
		"<p>API 密钥「%s」今日消费已超过设定的日消费告警阈值。</p><p>阈值：%s</p><p>今日消费：%s</p>",
		tokenName, logger.FormatQuota(threshold), logger.FormatQuota(int(spend)),
	)
	NotifyDailySpendAlert(userId, tokenId, "API密钥日消费超额提醒", htmlContent)
}
