package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureAsyncTaskCostDiscountSnapshotAddsExistingField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.75)
	billingContext := &model.TaskBillingContext{}

	data := ensureAsyncTaskCostDiscountSnapshot(c, billingContext, []byte(`{"status":"submitted"}`))
	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, "submitted", got["status"])
	assert.Equal(t, 0.75, got["billing_cost_discount"])
	require.NotNil(t, billingContext.CostDiscount)
	assert.Equal(t, 0.75, *billingContext.CostDiscount)
}

func TestEnsureAsyncTaskCostDiscountSnapshotDoesNotOverwriteExistingValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.75)
	billingContext := &model.TaskBillingContext{}

	data := ensureAsyncTaskCostDiscountSnapshot(c, billingContext, []byte(`{"billing_cost_discount":0.952}`))
	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, 0.952, got["billing_cost_discount"])
	require.NotNil(t, billingContext.CostDiscount)
	assert.Equal(t, 0.75, *billingContext.CostDiscount)
}

func TestEnsureAsyncTaskCostDiscountSnapshotLeavesDataWithoutDiscount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	billingContext := &model.TaskBillingContext{}

	original := []byte(`{"status":"submitted"}`)
	data := ensureAsyncTaskCostDiscountSnapshot(c, billingContext, original)
	assert.Equal(t, string(original), string(data))
	assert.Nil(t, billingContext.CostDiscount)
}

func TestEnsureAsyncTaskCostDiscountSnapshotPreservesConfiguredZeroCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, 0.0)
	common.SetContextKey(c, constant.ContextKeyChannelCostDiscountConfigured, true)
	billingContext := &model.TaskBillingContext{}

	data := ensureAsyncTaskCostDiscountSnapshot(c, billingContext, []byte(`{"status":"submitted"}`))
	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &got))
	assert.Equal(t, 0.0, got["billing_cost_discount"])
	require.NotNil(t, billingContext.CostDiscount)
	assert.Zero(t, *billingContext.CostDiscount)
}

func TestMergeVideoTaskDataPreservesCostDiscountSnapshot(t *testing.T) {
	task := &model.Task{Data: []byte(`{"billing_cost_discount":0.75,"billing_processed":false}`)}
	MergeVideoTaskDataWithUpstreamResponse(task, []byte(`{"id":"task-1","status":"running"}`))

	var got map[string]interface{}
	require.NoError(t, common.Unmarshal(task.Data, &got))
	assert.Equal(t, 0.75, got["billing_cost_discount"])
	assert.Equal(t, false, got["billing_processed"])
	assert.Equal(t, "running", got["status"])
}

func TestTaskCostDiscountSnapshotPrefersTaskData(t *testing.T) {
	task := &model.Task{Data: []byte(`{"billing_cost_discount":0.75}`)}
	require.NoError(t, common.UnmarshalJsonStr(`{"billing_context":{"cost_discount":0.8}}`, &task.PrivateData))

	discount, configured := taskCostDiscountSnapshot(task, map[string]interface{}{"billing_cost_discount": 0.75})
	assert.True(t, configured)
	assert.Equal(t, 0.75, discount)
}

func TestTaskCostDiscountSnapshotFallsBackToBillingContext(t *testing.T) {
	task := &model.Task{Data: []byte(`{"status":"succeeded"}`)}
	require.NoError(t, common.UnmarshalJsonStr(`{"billing_context":{"cost_discount":0.75}}`, &task.PrivateData))

	discount, configured := taskCostDiscountSnapshot(task, map[string]interface{}{"status": "succeeded"})
	assert.True(t, configured)
	assert.Equal(t, 0.75, discount)
}

func TestTaskCostDiscountSnapshotWithoutConfigurationIsZero(t *testing.T) {
	task := &model.Task{Data: []byte(`{"status":"succeeded"}`)}

	discount, configured := taskCostDiscountSnapshot(task, map[string]interface{}{"status": "succeeded"})
	assert.False(t, configured)
	assert.Zero(t, discount)
}

func TestTaskCostDiscountSnapshotTreatsZeroAsConfigured(t *testing.T) {
	task := &model.Task{Data: []byte(`{"billing_cost_discount":0}`)}

	discount, configured := taskCostDiscountSnapshot(task, map[string]interface{}{"billing_cost_discount": 0.0})

	assert.True(t, configured)
	assert.Zero(t, discount)
}
