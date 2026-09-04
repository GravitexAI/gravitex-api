package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordConsumeLogAddsContextCostDiscountAsFallback(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.75)

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ChannelId: 81,
		ModelName: "seedance-2-0-fast",
		Quota:     100,
		Other:     NewLogOtherFromMap(map[string]interface{}{"group_ratio": 1.0}),
	})

	var log Log
	require.NoError(t, LOG_DB.Order("id desc").First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.75, adminInfo["cost_discount"])
}

func TestRecordConsumeLogPreservesExistingCostDiscount(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.75)

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ChannelId: 81,
		ModelName: "seedance-2-0-fast",
		Other: NewLogOtherFromMap(map[string]interface{}{
			"admin_info": map[string]interface{}{"cost_discount": 0.8},
		}),
	})

	var log Log
	require.NoError(t, LOG_DB.Order("id desc").First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo := other["admin_info"].(map[string]interface{})
	assert.Equal(t, 0.8, adminInfo["cost_discount"])
}

func TestRecordConsumeLogTreatsNilAdminInfoAsAbsent(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.75)

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ChannelId: 81,
		ModelName: "seedance-2-0-fast",
		Other:     NewLogOtherFromMap(map[string]interface{}{"admin_info": nil}),
	})

	var log Log
	require.NoError(t, LOG_DB.Order("id desc").First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.75, adminInfo["cost_discount"])
}

func TestRecordConsumeLogWithoutCostDiscountLeavesOtherUnchanged(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ChannelId: 81,
		ModelName: "seedance-2-0-fast",
	})

	var log Log
	require.NoError(t, LOG_DB.Order("id desc").First(&log).Error)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "admin_info")
}
