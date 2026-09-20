package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordErrorLogTruncatesContentToFourThousandRunes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := DB, LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, database.AutoMigrate(&User{}, &Log{}))
	DB, LOG_DB = database, database
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		require.NoError(t, sqlDB.Close())
	})

	require.NoError(t, database.Create(&User{Id: 7, Username: "log-owner"}).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("username", "log-owner")

	content := strings.Repeat("错", 4001)
	RecordErrorLog(ctx, 7, 101, "test-model", "test-token", content, 11, 0, false, "default", nil)

	var stored Log
	require.NoError(t, database.First(&stored).Error)
	assert.Equal(t, LogTypeError, stored.Type)
	assert.True(t, utf8.ValidString(stored.Content))
	assert.LessOrEqual(t, utf8.RuneCountInString(stored.Content), 4000)
	assert.NotEqual(t, content, stored.Content)
}
