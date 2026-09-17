package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useTpmSupplementTestDB(t *testing.T) {
	t.Helper()
	previousLogDB := LOG_DB
	previousLogType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogType)
	})
}

func TestSumUsedQuotaAddsClaudeCacheTokensWithoutDoubleCountingDerivedFields(t *testing.T) {
	useTpmSupplementTestDB(t)

	now := time.Now().Unix()
	logs := []Log{
		{
			UserId: 1, Type: LogTypeConsume, CreatedAt: now - 10,
			PromptTokens: 10, CompletionTokens: 5,
			Other: `{"claude":true,"cache_tokens":7,"cache_creation_tokens":11,"cache_creation_tokens_5m":11,"cache_write_tokens":11}`,
		},
		{
			UserId: 1, Type: LogTypeConsume, CreatedAt: now - 8,
			PromptTokens: 12, CompletionTokens: 3,
			Other: `{"usage_semantic":"anthropic","cache_tokens":2,"cache_creation_tokens":4}`,
		},
		{
			UserId: 1, Type: LogTypeConsume, CreatedAt: now - 6,
			Other: `{"claude":true,"cache_creation_tokens_5m":3,"cache_creation_tokens_1h":4}`,
		},
		{
			UserId: 1, Type: LogTypeConsume, CreatedAt: now - 5,
			PromptTokens: 1, CompletionTokens: 1,
			Other: `{"cache_tokens":100,"cache_creation_tokens":100}`,
		},
	}
	for i := range logs {
		require.NoError(t, LOG_DB.Create(&logs[i]).Error)
	}

	stat, err := SumUsedQuota(LogTypeConsume, 0, 0, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 4, stat.Rpm)
	// 基础 token 为 32；Claude 补充缓存读/创建 7+11+2+4+3+4。
	// 创建量的总量、规范化字段与 5m/1h 拆分是互斥回退关系，不能重复累计。
	assert.Equal(t, 63, stat.Tpm)
}
