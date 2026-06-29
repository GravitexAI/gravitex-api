package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSumUsedQuotaDoesNotResetQuotaByRpmTpmScan 回归保护：
// 之前 bug：SumUsedQuota 内部两次 Scan(&stat)，第二次（rpm/tpm 查询）
// 把 stat.Quota 覆盖为 0。
func TestSumUsedQuotaDoesNotResetQuotaByRpmTpmScan(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()
	logs := []Log{
		{UserId: 1, Username: "chz", Type: LogTypeConsume, CreatedAt: now - 10, Quota: 100, PromptTokens: 10, CompletionTokens: 5},
		{UserId: 1, Username: "chz", Type: LogTypeConsume, CreatedAt: now - 5, Quota: 200, PromptTokens: 20, CompletionTokens: 10},
		{UserId: 2, Username: "alice", Type: LogTypeConsume, CreatedAt: now - 3, Quota: 50, PromptTokens: 5, CompletionTokens: 2},
		// 一个非 consume 的日志，验证 type 过滤
		{UserId: 1, Username: "chz", Type: LogTypeTopup, CreatedAt: now - 2, Quota: 999},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}

	t.Run("no username filter sums all consume logs", func(t *testing.T) {
		stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
		require.NoError(t, err)
		assert.Equalf(t, 350, stat.Quota, "100 + 200 + 50 = 350，topup 类型应被过滤掉")
	})

	t.Run("username filter limits to that user", func(t *testing.T) {
		stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "chz", "", 0, "")
		require.NoError(t, err)
		assert.Equalf(t, 300, stat.Quota, "chz 的 consume = 100 + 200 = 300")
	})

	t.Run("rpm/tpm only counts last 60s and does not reset quota", func(t *testing.T) {
		stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
		require.NoError(t, err)
		// 关键回归：stat.Quota 不能因为 rpm/tpm 那次 Scan 被重置为 0
		assert.Equalf(t, 350, stat.Quota, "Quota 不能被 rpm/tpm 的 Scan 覆盖为 0")
		// rpm（最近 60s 的 consume 数）也应该有值
		assert.GreaterOrEqualf(t, stat.Rpm, 3, "最近 60s 有 3 条 consume 日志")
	})
}
