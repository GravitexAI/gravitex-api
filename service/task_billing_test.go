package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.Channel{},
		&model.Midjourney{},
		&model.TopUp{},
		&model.UserSubscription{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
		&model.TaskBillingSettlement{},
		&model.TaskBillingLogOutbox{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
		model.DB.Exec("DELETE FROM task_billing_settlements")
		model.DB.Exec("DELETE FROM task_billing_log_outbox")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func seedChargedAccounting(t *testing.T, userID, channelID, tokenID, quota, requestCount int) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"used_quota":    quota,
		"request_count": requestCount,
	}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).
		Update("used_quota", quota).Error)
	if tokenID > 0 {
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).
			Update("used_quota", quota).Error)
	}
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func TestPriceDataOtherRatiosFilterAndSnapshot(t *testing.T) {
	priceData := types.PriceData{}

	priceData.AddOtherRatio("zero", 0)
	priceData.AddOtherRatio("negative", -0.5)
	priceData.AddOtherRatio("nan", math.NaN())
	priceData.AddOtherRatio("inf", math.Inf(1))
	priceData.AddOtherRatio("one", 1)
	priceData.AddOtherRatio("positive", 2.5)

	ratios := priceData.OtherRatios()
	require.Len(t, ratios, 2)
	assert.Equal(t, 1.0, ratios["one"])
	assert.Equal(t, 2.5, ratios["positive"])
	assert.True(t, priceData.HasOtherRatio("one"))
	assert.False(t, priceData.HasOtherRatio("zero"))

	ratios["positive"] = 99
	ratios["new"] = 3
	nextSnapshot := priceData.OtherRatios()
	assert.Equal(t, 2.5, nextSnapshot["positive"])
	assert.NotContains(t, nextSnapshot, "new")
}

func TestPriceDataReplaceAndApplyOtherRatios(t *testing.T) {
	priceData := types.PriceData{}

	replaced := priceData.ReplaceOtherRatios(map[string]float64{
		"zero":     0,
		"negative": -3,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
		"one":      1,
		"duration": 2,
		"size":     1.5,
	})

	require.True(t, replaced)
	assert.Equal(t, 3.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, 30.0, priceData.ApplyOtherRatiosToFloat(10))
	assert.Equal(t, 10.0, priceData.RemoveOtherRatiosFromFloat(30))
	assert.True(t, decimal.NewFromInt(30).Equal(priceData.ApplyOtherRatiosToDecimal(decimal.NewFromInt(10))))

	replaced = priceData.ReplaceOtherRatios(map[string]float64{
		"zero": 0,
		"nan":  math.NaN(),
	})

	require.False(t, replaced)
	assert.Nil(t, priceData.OtherRatios())
	assert.Equal(t, 1.0, priceData.OtherRatioMultiplier())
}

func TestTaskBillingOtherFiltersHistoricalOtherRatios(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{
		"seconds":  2,
		"identity": 1,
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	}

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, 2.0, other["seconds"])
	assert.Equal(t, 1.0, other["identity"])
	assert.NotContains(t, other, "zero")
	assert.NotContains(t, other, "negative")
	assert.NotContains(t, other, "nan")
	assert.NotContains(t, other, "inf")
	assert.NotContains(t, other, "billing_mode")
	assert.NotContains(t, other, "expr_b64")
	assert.NotContains(t, other, "matched_tier")
	assert.NotContains(t, other, "usage_facts")
}

func TestTaskBillingOtherIncludesTieredSnapshotAndKeepsUsageFactsNested(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	expression := `tier("720P", u("seconds") * 5)`
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:    expression,
		EstimatedTier: "720P",
		UsageFacts: map[string]any{
			"resolution": "720P",
			"seconds":    5,
		},
	}

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(expression)), other["expr_b64"])
	assert.Equal(t, "720P", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{
		"resolution": "720P",
		"seconds":    5,
	}, facts)
	assert.NotContains(t, other, "resolution")
	assert.NotContains(t, other, "seconds")
}

func TestTaskBillingOtherOmitsEmptyUsageFacts(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	expression := `tier("base", 1)`
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:    expression,
		EstimatedTier: "base",
		UsageFacts:    map[string]any{},
	}

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(expression)), other["expr_b64"])
	assert.Equal(t, "base", other["matched_tier"])
	assert.NotContains(t, other, "usage_facts")
}

func TestTaskBillingOtherPreservesNativeInteractionsRequestPath(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatformVertexInteractions
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{RequestPath: "/v1beta/interactions"}

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, "/v1beta/interactions", other["request_path"])
}

func TestTaskExecutionSnapshotUsesOriginalInteractionsPath(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	ctx.Set("native_interactions_original_path", "/v1beta/interactions")

	snapshot := TaskExecutionSnapshotFromContext(ctx)

	require.NotNil(t, snapshot)
	assert.Equal(t, "/v1beta/interactions", snapshot.RequestPath)
}

func callLogTaskConsumption(t *testing.T, info *relaycommon.RelayInfo, task *model.Task) *model.Log {
	return callLogTaskConsumptionWithPath(t, info, task, "/v1/videos")
}

func callLogTaskConsumptionWithPath(t *testing.T, info *relaycommon.RelayInfo, task *model.Task, path string) *model.Log {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumption(ctx, info)
	log := getLastLog(t)
	require.NotNil(t, log)
	return log
}

func TestLogTaskConsumptionPreservesNativeInteractionRequestPath(t *testing.T) {
	truncate(t)
	const userID, channelID = 42, 42
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:             userID,
		OriginModelName:    "gemini-nano-banana-2.1",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{Action: "generate_content"},
		UsingGroup:         "default",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelPrice:     0.75,
			Quota:          100,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	ctx.Set("token_name", "test_token")
	ctx.Set("native_interactions_original_path", "/v1beta/interactions")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		UsageAvailable:   true,
		InputTokens:      22,
		CompletionTokens: 1_680,
		TotalTokens:      1_702,
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "/v1beta/interactions", other["request_path"])
	assert.Equal(t, "upstream", other["billing_process"].(map[string]any)["source"])
	assert.Equal(t, "upstream", other["admin_info"].(map[string]any)["usage_billing_path"])
	assert.Equal(t, 22, log.PromptTokens)
	assert.Equal(t, 0, log.CompletionTokens)
	assert.Equal(t, float64(1_680), other["image_output_tokens"])
	assert.Equal(t, float64(0), other["text_output_tokens"])
}

func TestLogTaskConsumptionMarksMissingInteractionUsageAsConfiguredBilling(t *testing.T) {
	truncate(t)
	const userID, channelID = 44, 44
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:             userID,
		OriginModelName:    "gemini-2.5-flash",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{Action: "generate_content"},
		UsingGroup:         "default",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData:          types.PriceData{ModelPrice: 0.75, Quota: 100, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "configured", other["billing_process"].(map[string]any)["source"])
	assert.Equal(t, "configured", other["admin_info"].(map[string]any)["usage_billing_path"])
	assert.Equal(t, true, other["admin_info"].(map[string]any)["usage_missing"])
	assert.NotContains(t, other, "usage_facts")
}

func TestLogTaskConsumptionWritesFlatModalityKeysForJavaLayer(t *testing.T) {
	truncate(t)
	const userID, channelID = 43, 43
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:             userID,
		OriginModelName:    "gemini-nano-banana-2.1",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{Action: "generate_content"},
		UsingGroup:         "default",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelRatio:           0.75,
			CompletionRatio:      5,
			ImageCompletionRatio: 20,
			CacheRatio:           0.1,
			AudioRatio:           1.5,
			AudioCompletionRatio: 2,
			Quota:                100,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	ctx.Set("token_name", "test_token")
	ctx.Set("native_interactions_original_path", "/v1beta/interactions")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		UsageAvailable:    true,
		InputTokens:       1231,
		ImageInputTokens:  1120,
		CachedTokens:      100,
		CompletionTokens:  1120,
		TotalTokens:       2351,
		TextInputTokens:   11,
		TextOutputTokens:  7,
		AudioOutputTokens: 13,
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	// Flat keys consumed by the Java log layer (api/logs/list); without them
	// imageInputTokens stays null and the billing process hides image input.
	assert.Equal(t, float64(1120), other["input_image_tokens"])
	assert.Equal(t, 1.5, other["input_image_price"])
	assert.Equal(t, float64(100), other["cache_tokens"])
	assert.Equal(t, 0.1, other["cache_ratio"])
	assert.Equal(t, 5.0, other["completion_ratio"])
	assert.Equal(t, 1.5, other["input_text_price"])
	assert.Equal(t, true, other["audio"])
	assert.Equal(t, 1.5, other["audio_ratio"])
	assert.Equal(t, 2.0, other["audio_completion_ratio"])
	assert.Equal(t, float64(11), other["text_input"])
	assert.Equal(t, float64(7), other["text_output"])
	assert.Equal(t, float64(13), other["audio_output"])
	billingProcess := other["billing_process"].(map[string]any)
	assert.Equal(t, 1.5, billingProcess["audio_ratio"])
	assert.Equal(t, 2.0, billingProcess["audio_completion_ratio"])
}

func TestTaskBillingProcessReportsModalityRatiosSeparately(t *testing.T) {
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "gemini-nano-banana-2.1",
		PriceData: types.PriceData{
			ModelRatio:           0.75,
			CompletionRatio:      1,
			ImageCompletionRatio: 20,
			CacheRatio:           0.2,
			AudioRatio:           1.5,
			AudioCompletionRatio: 2,
		},
	}

	billing := taskBillingProcess(info, "upstream")
	// Mixed output is billed per modality, so the text completion ratio must
	// not be overridden with the image ratio anymore.
	require.Equal(t, 1.0, billing["completion_ratio"])
	require.Equal(t, 20.0, billing["image_completion_ratio"])
	require.Equal(t, 0.2, billing["cache_ratio"])
	require.Equal(t, 1.5, billing["audio_ratio"])
	require.Equal(t, 2.0, billing["audio_completion_ratio"])
}

func TestLogTaskConsumptionIncludesTieredSnapshotUsageFacts(t *testing.T) {
	truncate(t)
	const userID, channelID = 40, 40
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	expression := `tier("720P", u("seconds") * 5)`
	task := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         0,
		OriginModelName: "wan2.5-i2v-preview",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData: types.PriceData{
			ModelPrice:     0.02,
			Quota:          100,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			ExprString:    expression,
			EstimatedTier: "720P",
			UsageFacts: map[string]any{
				"resolution": "720P",
				"seconds":    5,
			},
		},
	}

	log := callLogTaskConsumption(t, info, task)

	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(expression)), other["expr_b64"])
	assert.Equal(t, "720P", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "720P", facts["resolution"])
	assert.Equal(t, float64(5), facts["seconds"])
	assert.NotContains(t, other, "resolution")
	assert.NotContains(t, other, "seconds")
	assert.Contains(t, log.Content, "计算参数：")
	assert.Contains(t, log.Content, "resolution: 720P")
	assert.Contains(t, log.Content, "seconds: 5")
}

func TestLogTaskConsumptionIncludesNativeInteractionsTieredBillingDetail(t *testing.T) {
	truncate(t)
	const userID, channelID = 46, 46
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		UserId:             userID, OriginModelName: "gemini-3-pro-image", UsingGroup: "default",
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:     types.PriceData{Quota: 100, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr", ExprString: `tier("base", p * 2 + c * 8)`, ExprHash: "expr-hash", EstimatedTier: "base",
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	logTaskConsumption(ctx, info, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess})
	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	adminInfo, ok := other["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, adminInfo["tiered_usage_unavailable"])
	assert.Equal(t, "expr-hash", adminInfo["expression_hash"])
	assert.Equal(t, "configured", adminInfo["usage_billing_path"])
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(`tier("base", p * 2 + c * 8)`)), other["expr_b64"])
	assert.Equal(t, "base", other["matched_tier"])
}

func TestLogNativeInteractionsTieredImageKeepsAggregateOutputForBillingDetail(t *testing.T) {
	truncate(t)
	const userID, channelID = 48, 48
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	expression := `tier("base", p * 2 + c * 8 + img_o * 20)`
	info := &relaycommon.RelayInfo{
		NativeInteractions: true, UserId: userID, OriginModelName: "gemini-3.5-flash",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:     types.PriceData{Quota: 100, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
			EstimatedTier: "base",
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 13,
		CompletionTokens: 1120, ImageOutputTokens: 1120,
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 13, log.PromptTokens)
	assert.Equal(t, 1120, log.CompletionTokens)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(expression)), other["expr_b64"])
	assert.Equal(t, float64(1120), other["image_output_tokens"])
}

func TestLogNativeInteractionsAggregateTextOutputForBillingDetail(t *testing.T) {
	truncate(t)
	const userID, channelID = 49, 49
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	expression := `tier("short context", p * 1.5 + c * 1.5)`
	info := &relaycommon.RelayInfo{
		NativeInteractions: true, UserId: userID, OriginModelName: "gemini-3.5-flash",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData: types.PriceData{
			ModelRatio: 0.75, CompletionRatio: 1,
			Quota: 407, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
			EstimatedTier: "short context",
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 7, CompletionTokens: 264,
		TotalTokens: 271, TextInputTokens: 7, TextOutputTokens: 264,
		UsageFacts: map[string]any{"input_tokens": 7, "output_tokens": 264, "total_tokens": 271, "text_output_tokens": 264},
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, float64(264), other["text_output_tokens"])
	assert.Equal(t, float64(264), other["usage_facts"].(map[string]any)["text_output_tokens"])
}

func TestLogNativeInteractionsIncludesThoughtTokensInCompletionColumn(t *testing.T) {
	truncate(t)
	const userID, channelID = 46, 46
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:             userID,
		OriginModelName:    "gemini-3.6-flash",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{Action: "generate_content"},
		UsingGroup:         "default",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelRatio:      0.375,
			CompletionRatio: 5,
			Quota:           85,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		UsageAvailable:   true,
		InputTokens:      7,
		ThoughtTokens:    44,
		CompletionTokens: 0,
		TotalTokens:      51,
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, 7, log.PromptTokens)
	assert.Equal(t, 44, log.CompletionTokens)
	assert.Equal(t, float64(44), other["reasoning_tokens"])
	assert.Equal(t, 0.75, other["input_text_price"])
	assert.Equal(t, 3.75, other["output_text_price"])
	billingProcess, ok := other["billing_process"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "upstream", billingProcess["source"])
}

func TestLogNativeInteractionsDerivesUnreportedThoughtTokensForBillingDetail(t *testing.T) {
	truncate(t)
	const userID, channelID = 47, 47
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	info := &relaycommon.RelayInfo{
		UserId:             userID,
		OriginModelName:    "gemini-3.6-flash",
		NativeInteractions: true,
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{Action: "generate_content"},
		UsingGroup:         "default",
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: channelID},
		PriceData: types.PriceData{
			ModelRatio:      0.375,
			CompletionRatio: 5,
			Quota:           85,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		UsageAvailable:   true,
		InputTokens:      7,
		CompletionTokens: 0,
		TotalTokens:      51,
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, 7, log.PromptTokens)
	assert.Equal(t, 44, log.CompletionTokens)
	assert.Equal(t, float64(44), other["reasoning_tokens"])
	assert.Equal(t, 0.75, other["input_text_price"])
	assert.Equal(t, 3.75, other["output_text_price"])
	billingProcess, ok := other["billing_process"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "upstream", billingProcess["source"])
}

func TestLogNativeInteractionsAudioVideoUsageAsFlatBillingDetails(t *testing.T) {
	truncate(t)
	const userID, channelID = 50, 50
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true, UserId: userID, OriginModelName: "gemini-omni-flash-preview",
		UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData: types.PriceData{
			ModelRatio: 0.75, CompletionRatio: 1,
			Quota: 407, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr", ExprString: `tier("base", p + c + ao)`,
			ExprHash: billingexpr.ExprHashString(`tier("base", p + c + ao)`), EstimatedTier: "base",
		},
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	ctx.Set("token_name", "test_token")
	LogTaskConsumptionWithTaskInfo(ctx, info, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 7, CompletionTokens: 264,
		TotalTokens: 271, TextInputTokens: 7, TextOutputTokens: 120,
		VideoOutputTokens: 100, AudioInputTokens: 3, AudioOutputTokens: 44,
		UsageFacts: map[string]any{
			"input_tokens": 7, "output_tokens": 264, "total_tokens": 271,
			"text_output_tokens": 120, "video_output_tokens": 100,
			"audio_input_tokens": 3, "audio_output_tokens": 44,
		},
	})

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, float64(120), other["text_output_tokens"])
	assert.Equal(t, float64(100), other["video_output_tokens"])
	assert.Equal(t, float64(3), other["audio_input"])
	assert.Equal(t, float64(44), other["audio_output"])
	assert.Equal(t, float64(44), other["audio_output_tokens"])
}

func TestLogTaskConsumptionWithoutSnapshotKeepsRatioMode(t *testing.T) {
	truncate(t)
	const userID, channelID = 41, 41
	seedUser(t, userID, 10_000)
	seedChannel(t, channelID)

	priceData := types.PriceData{
		ModelPrice:     0.02,
		Quota:          100,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	priceData.AddOtherRatio("size", 2)
	task := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         0,
		OriginModelName: "test-model",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:       priceData,
	}

	log := callLogTaskConsumption(t, info, task)

	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, true, other["is_task"])
	assert.Equal(t, "/v1/videos", other["request_path"])
	assert.NotContains(t, other, "billing_mode")
	assert.NotContains(t, other, "expr_b64")
	assert.NotContains(t, other, "matched_tier")
	assert.NotContains(t, other, "usage_facts")
	assert.Contains(t, log.Content, "计算参数：")
	assert.Contains(t, log.Content, "size: 2.00")
}

func TestTaskBillingOtherSeparatesPluginAndRootDiagnostics(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	task.TaskID = "task_public"
	task.PrivateData.UpstreamTaskID = "upstream-private"
	task.PrivateData.NodeName = "node-a"
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{
		TaskPlugin: &model.TaskPluginSnapshot{
			Key:     "document-parser",
			Name:    "Document Parser",
			Version: "1.2.3",
			Author: &model.TaskPluginAuthorSnapshot{
				Name: "Community Author",
				URL:  "https://plugins.example/author",
			},
			APIVersion: 1,
			Generation: 42,
		},
	}

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, "task_public", other["task_id"])
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	pluginInfo, ok := adminInfo["task_plugin"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "document-parser", pluginInfo["key"])
	assert.Equal(t, "1.2.3", pluginInfo["version"])
	assert.Equal(t, map[string]interface{}{
		"name": "Community Author",
		"url":  "https://plugins.example/author",
	}, pluginInfo["author"])

	rootInfo, ok := other["root_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "upstream-private", rootInfo["upstream_task_id"])
	assert.Equal(t, "node-a", rootInfo["node_name"])
	runtimeInfo, ok := rootInfo["task_plugin"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, uint64(42), runtimeInfo["generation"])
	assert.NotContains(t, runtimeInfo, "author")
}

func TestTaskBillingContextPriceDataFiltersMultiplier(t *testing.T) {
	priceData := taskBillingContextPriceData(&model.TaskBillingContext{
		OtherRatios: map[string]float64{
			"seconds":  2,
			"size":     3,
			"identity": 1,
			"zero":     0,
			"negative": -1,
			"nan":      math.NaN(),
			"inf":      math.Inf(1),
		},
	})

	require.NotNil(t, priceData)
	assert.Equal(t, 6.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, map[string]float64{
		"seconds":  2,
		"size":     3,
		"identity": 1,
	}, priceData.OtherRatios())
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getUserUsageAccounting(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return user.UsedQuota, user.RequestCount
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return channel.UsedQuota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return task.Quota
}

func getMidjourneyTask(t *testing.T, id int) model.Midjourney {
	t.Helper()
	var task model.Midjourney
	require.NoError(t, model.DB.First(&task, id).Error)
	return task
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

// ===========================================================================
// Legacy Midjourney billing tests
// ===========================================================================

func TestPrepareMidjourneyTaskBillingKeepsUnbilledMarkerClear(t *testing.T) {
	task := &model.Midjourney{Quota: 900, TokenId: 7, BillingChannelId: 8}

	prepared, err := PrepareMidjourneyTaskBilling(&relaycommon.RelayInfo{}, task, 900, false)

	require.NoError(t, err)
	assert.False(t, prepared)
	assert.Zero(t, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Zero(t, task.BillingChannelId)
}

func TestSettleMidjourneyTaskBillingRequiresPersistedTask(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 49, 49, 49
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-midjourney-unpersisted", initialTokenQuota)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-unpersisted",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	assert.False(t, billed)
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
}

func TestMidjourneyRefundRestoresEveryAccountingElementOnBillingChannel(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, billingChannelID, executionChannelID = 50, 50, 50, 51
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-midjourney", initialTokenQuota)
	seedChannel(t, billingChannelID)
	seedChannel(t, executionChannelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:     userID,
		TokenId:    tokenID,
		TokenKey:   "sk-midjourney",
		UserQuota:  initialUserQuota,
		UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: billingChannelID,
		},
	}
	task := &model.Midjourney{
		UserId:    userID,
		Action:    "IMAGINE",
		MjId:      "mj-accounting-refund",
		ChannelId: executionChannelID,
		Progress:  "0%",
	}

	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	assert.Equal(t, chargedQuota, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Equal(t, billingChannelID, task.BillingChannelId)
	require.NoError(t, task.Insert())

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)
	require.NoError(t, err)
	require.True(t, billed)
	assert.Equal(t, initialUserQuota-chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota-chargedQuota, getTokenRemainQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Equal(t, chargedQuota, persisted.Quota)
	assert.Equal(t, tokenID, persisted.TokenId)
	assert.Equal(t, billingChannelID, persisted.BillingChannelId)

	seedChargedAccounting(t, userID, billingChannelID, tokenID, chargedQuota, 1)

	assert.True(t, RefundMidjourneyQuota(ctx, task, "构图失败"))
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, billingChannelID))
	assert.Zero(t, getChannelUsedQuota(t, executionChannelID))

	persisted = getMidjourneyTask(t, task.Id)
	assert.Zero(t, persisted.Quota)
	assert.Equal(t, tokenID, persisted.TokenId)
	assert.Equal(t, billingChannelID, persisted.BillingChannelId)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, chargedQuota, log.Quota)
	assert.Equal(t, tokenID, log.TokenId)
	assert.Equal(t, billingChannelID, log.ChannelId)

	assert.True(t, RefundMidjourneyQuota(ctx, task, "duplicate poll"))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestSettleMidjourneyTaskBillingFundingFailureClearsMarkers(t *testing.T) {
	truncate(t)

	const userID, tokenID, channelID = 52, 52, 52
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-midjourney-funding-failure", initialTokenQuota)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-funding-failure",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, MjId: "mj-funding-failure", ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.NoError(t, task.Insert())

	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_midjourney_user_update
		BEFORE UPDATE ON users
		WHEN OLD.id = 52
		BEGIN
			SELECT RAISE(ABORT, 'forced user quota failure');
		END;
	`).Error)
	t.Cleanup(func() {
		model.DB.Exec("DROP TRIGGER IF EXISTS fail_midjourney_user_update")
	})

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	assert.False(t, billed)
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Zero(t, persisted.Quota)
	assert.Zero(t, persisted.TokenId)
	assert.Zero(t, persisted.BillingChannelId)
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Zero(t, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	assert.Zero(t, countLogs(t))
}

func TestSettleMidjourneyTaskBillingTokenFailureKeepsFundingRefundable(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 53, 53, 53
	const initialUserQuota, initialTokenQuota, chargedQuota = 10000, 5000, 3000
	seedUser(t, userID, initialUserQuota)
	seedToken(t, tokenID, userID, "sk-midjourney-token-failure", initialTokenQuota)
	seedChannel(t, channelID)

	relayInfo := &relaycommon.RelayInfo{
		UserId:    userID,
		TokenId:   tokenID,
		TokenKey:  "sk-midjourney-token-failure",
		UserQuota: initialUserQuota,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: channelID,
		},
	}
	task := &model.Midjourney{UserId: userID, MjId: "mj-token-failure", ChannelId: channelID}
	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, chargedQuota, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.NoError(t, task.Insert())

	require.NoError(t, model.DB.Exec(`
		CREATE TRIGGER fail_midjourney_token_update
		BEFORE UPDATE ON tokens
		WHEN OLD.id = 53
		BEGIN
			SELECT RAISE(ABORT, 'forced token quota failure');
		END;
	`).Error)
	t.Cleanup(func() {
		model.DB.Exec("DROP TRIGGER IF EXISTS fail_midjourney_token_update")
	})

	billed, err := SettleMidjourneyTaskBilling(relayInfo, task, prepared)

	require.Error(t, err)
	require.True(t, billed)
	assert.Equal(t, initialUserQuota-chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	persisted := getMidjourneyTask(t, task.Id)
	assert.Equal(t, chargedQuota, persisted.Quota)
	assert.Zero(t, persisted.TokenId)
	assert.Equal(t, channelID, persisted.BillingChannelId)

	seedChargedAccounting(t, userID, channelID, 0, chargedQuota, 1)
	assert.True(t, RefundMidjourneyQuota(ctx, task, "token settlement failed"))
	assert.Equal(t, initialUserQuota, getUserQuota(t, userID))
	assert.Equal(t, initialTokenQuota, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Zero(t, log.TokenId)
}

func TestPrepareMidjourneyTaskBillingRejectsSubscriptionBeforeCharge(t *testing.T) {
	task := &model.Midjourney{Quota: 900, TokenId: 7, BillingChannelId: 8}
	relayInfo := &relaycommon.RelayInfo{BillingSource: BillingSourceSubscription, SubscriptionId: 1}

	prepared, err := PrepareMidjourneyTaskBilling(relayInfo, task, 900, true)

	require.Error(t, err)
	assert.False(t, prepared)
	assert.Zero(t, task.Quota)
	assert.Zero(t, task.TokenId)
	assert.Zero(t, task.BillingChannelId)
}

func TestRefundMidjourneyQuotaUsesLegacyChannelFallbackWithoutTokenAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 54, 54, 54
	const walletAfterCharge, tokenQuota, chargedQuota = 7000, 5000, 3000
	seedUser(t, userID, walletAfterCharge)
	seedToken(t, tokenID, userID, "sk-midjourney-legacy", tokenQuota)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, chargedQuota, 1)
	task := &model.Midjourney{
		UserId:    userID,
		MjId:      "mj-legacy-fallback",
		Action:    "IMAGINE",
		ChannelId: channelID,
		Quota:     chargedQuota,
		TokenId:   0,
		Progress:  "0%",
	}
	require.NoError(t, task.Insert())

	assert.True(t, RefundMidjourneyQuota(ctx, task, "legacy failure"))

	assert.Equal(t, walletAfterCharge+chargedQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenQuota, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, channelID, log.ChannelId)
	assert.Zero(t, log.TokenId)
}

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	assert.Zero(t, task.Quota)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestTaskBillingOtherIncludesVideoBillingFields(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	data, err := common.Marshal(map[string]interface{}{
		"requested_seconds":               8,
		"generate_audio":                  true,
		"video_resolution":                "1080p",
		"official_video_price_per_second": 0.12,
	})
	require.NoError(t, err)
	task.Data = data

	other := taskBillingOther(task).Snapshot()

	assert.Equal(t, "per_second", other["billing_type"])
	assert.Equal(t, 8, other["requested_seconds"])
	assert.Equal(t, true, other["generate_audio"])
	assert.Equal(t, "1080p", other["video_resolution"])
	assert.Equal(t, 0.12, other["official_video_price_per_second"])
	assert.Equal(t, 0.12, other["video_price_per_second"])
}

func TestTaskBillingOtherInjectsCostDiscountSnapshot(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	require.NoError(t, common.UnmarshalJsonStr(`{"billing_context":{"cost_discount":0.8}}`, &task.PrivateData))
	task.Data = json.RawMessage(`{"billing_cost_discount":0.75,"admin_info":{"cost_discount":0.952,"use_channel":["81"]}}`)

	other := taskBillingOther(task).Snapshot()

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.75, adminInfo["cost_discount"])
	assert.Equal(t, []interface{}{"81"}, adminInfo["use_channel"])
}

func TestTaskBillingOtherFallsBackToBillingContextCostDiscount(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	require.NoError(t, common.UnmarshalJsonStr(`{"billing_context":{"cost_discount":0.75}}`, &task.PrivateData))
	task.Data = json.RawMessage(`{"status":"succeeded"}`)

	other := taskBillingOther(task).Snapshot()

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.75, adminInfo["cost_discount"])
}

func TestTaskBillingOtherFallsBackToChannelCostDiscount(t *testing.T) {
	truncate(t)
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = originalMemoryCacheEnabled })
	discount := 0.952
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:           81,
		Name:         "cost-channel",
		Key:          "sk-test",
		Status:       common.ChannelStatusEnabled,
		CostDiscount: &discount,
	}).Error)
	task := makeTask(1, 81, 3000, 1, BillingSourceWallet, 0)
	task.Data = json.RawMessage(`{"status":"succeeded"}`)

	other := taskBillingOther(task).Snapshot()

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.952, adminInfo["cost_discount"])
}

func TestTaskBillingOtherInjectsConfiguredZeroCostDiscount(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	task.Data = json.RawMessage(`{"billing_cost_discount":0}`)

	other := taskBillingOther(task).Snapshot()

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, 0.0, adminInfo["cost_discount"])
}

func TestTaskBillingOtherWithoutCostDiscountDoesNotCreateAdminInfo(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	task.Data = json.RawMessage(`{"status":"submitted"}`)

	other := taskBillingOther(task).Snapshot()

	assert.NotContains(t, other, "admin_info")
}

func TestTaskBillingOtherIgnoresInvalidCostDiscount(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	task.Data = json.RawMessage(`{"billing_cost_discount":"invalid"}`)

	other := taskBillingOther(task).Snapshot()

	assert.NotContains(t, other, "admin_info")
}

func TestTaskBillingOtherIgnoresMalformedTaskData(t *testing.T) {
	task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
	task.Data = json.RawMessage(`{"billing_cost_discount":`)

	other := taskBillingOther(task).Snapshot()

	assert.NotContains(t, other, "admin_info")
}

func TestEnrichTaskBillingDataForTokenSettlementAddsVideoBillingDetails(t *testing.T) {
	for _, modelName := range []string{"gemini-omni-flash-preview", "gemini-omni-1.1-flash-preview"} {
		t.Run(modelName, func(t *testing.T) {
			task := makeTask(1, 1, 3000, 1, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.OriginModelName = modelName
			task.Properties.OriginModelName = modelName
			task.Data = json.RawMessage(`{"requested_seconds":4}`)

			result := &relaycommon.TaskInfo{TotalTokens: 17813, InputTokens: 1000, TextOutputTokens: 100, VideoOutputTokens: 500}
			enrichTaskBillingDataForTokenSettlement(task, result)

			var data map[string]interface{}
			require.NoError(t, common.Unmarshal(task.Data, &data))
			assert.Equal(t, "video_token_ratio", data["billing_type"])
			assert.Equal(t, float64(17813), data["tokens"])
			assert.Equal(t, float64(1000), data["input_tokens"])
			assert.Equal(t, float64(1000), data["input_text_tokens"])
			assert.Equal(t, float64(100), data["text_output_tokens"])
			assert.Equal(t, float64(500), data["video_output_tokens"])
			assert.Equal(t, 1.5, data["input_price_per_million_tokens"])

			actualQuota, ok, _ := calculateGeminiOmniQuota(task, result)
			require.True(t, ok)
			assert.Equal(t, 5575, actualQuota)
		})
	}
}

func TestRecalculateGeminiOmniWritesUsageAndDurationColumns(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	seedUser(t, 21, 10000)
	seedToken(t, 21, 21, "sk-omni", 10000)
	seedChannel(t, 21)

	task := makeTask(21, 21, 1000, 21, BillingSourceWallet, 0)
	task.Properties.OriginModelName = "gemini-omni-flash-preview"
	task.PrivateData.BillingContext.OriginModelName = "gemini-omni-flash-preview"
	task.StartTime = 100
	task.FinishTime = 116
	task.Data = json.RawMessage(`{"billing_type":"video_token_ratio","input_tokens":21,"input_text_tokens":21,"text_output_tokens":199,"video_output_tokens":23168}`)
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuota(ctx, task, 2000, "Gemini Omni token重算")

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, 21, log.PromptTokens)
	require.Equal(t, 23367, log.CompletionTokens)
	require.Equal(t, 16, log.UseTime)
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "subscription task failed"))

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	assert.True(t, RefundTaskQuota(ctx, task, "zero quota task"))

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	require.NoError(t, model.DB.Create(task).Error)

	assert.True(t, RefundTaskQuota(ctx, task, "no token task failed"))

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_FundingFailureKeepsAccountingAndPendingMarker(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID, preConsumed = 5, 5, 1200
	seedUser(t, userID, 5000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceSubscription, 9999)
	task.Status = model.TaskStatusFailure
	require.NoError(t, model.DB.Create(task).Error)

	assert.False(t, RefundTaskQuota(ctx, task, "subscription missing"))
	assert.Equal(t, 5000, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, actualQuota, "adaptor adjustment")

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, preConsumed, "exact match")

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 13, 5000
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	RecalculateTaskQuota(ctx, task, 0, "zero actual")

	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Zero(t, task.Quota)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
}

func TestRecalculate_RejectsNegativeActualQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 34, 5000
	const initQuota = 10000
	seedUser(t, userID, initQuota)
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	RecalculateTaskQuota(ctx, task, -1, "invalid negative actual")

	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	RecalculateTaskQuota(ctx, task, actualQuota, "subscription over-charge")

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Simulates the flow in updateVideoSingleTask (service/task_polling.go)
// ===========================================================================

// simulatePollBilling reproduces the CAS + billing logic from updateVideoSingleTask.
// It takes a persisted task (already in DB), applies the new status, and performs
// the conditional update + billing exactly as the polling loop does.
func simulatePollBilling(ctx context.Context, task *model.Task, newStatus model.TaskStatus, actualQuota int) {
	snap := task.Snapshot()

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota

	task.Status = newStatus
	switch string(newStatus) {
	case model.TaskStatusSuccess:
		task.Progress = "100%"
		task.FinishTime = 9999
		shouldSettle = true
	case model.TaskStatusFailure:
		task.Progress = "100%"
		task.FinishTime = 9999
		task.FailReason = "upstream error"
		if quota != 0 {
			shouldRefund = true
		}
	default:
		task.Progress = "50%"
	}

	isDone := task.Status == model.TaskStatus(model.TaskStatusSuccess) || task.Status == model.TaskStatus(model.TaskStatusFailure)
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	if shouldSettle && actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "test settle")
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusFailure), 0)

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusSuccess), actualQuota)

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

func TestNonTerminalUpdate_NoBilling(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 23, 23
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	task.Progress = "20%"
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate a non-terminal poll update (still IN_PROGRESS, progress changed)
	simulatePollBilling(ctx, task, model.TaskStatus(model.TaskStatusInProgress), 0)

	// User quota should NOT change
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No billing log
	assert.Equal(t, int64(0), countLogs(t))

	// Task progress should be updated in DB
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.Equal(t, "50%", reloaded.Progress)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

type recoveryTaskAdaptor struct {
	result *relaycommon.TaskInfo
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

func (a *recoveryTaskAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (a *recoveryTaskAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (a *recoveryTaskAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return a.result, nil
}
func (a *recoveryTaskAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settled := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no adjustment despite adaptor returning 2000
	assert.False(t, settled)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	settled := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Per-call: no recalculation by tokens
	assert.False(t, settled)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	settled := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.True(t, settled)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestSettle_TieredEvaluationFailureKeepsPreConsumedCharge(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, preConsumed = 33, 5_000
	const initialQuota = 10_000
	seedUser(t, userID, initialQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       `tier("broken",`,
		ExprHash:         billingexpr.ExprHashString(`tier("broken",`),
		GroupRatio:       1,
		QuotaPerUnit:     1_000,
		ExprVersion:      1,
		TaskUsageBilling: true,
	}

	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{Status: model.TaskStatusFailure})

	assert.True(t, settled)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_TieredFailureReturnsFalseForCallerRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 37
	const initialQuota, preConsumed = 10_000, 25
	seedUser(t, userID, initialQuota)

	expression := `tier("base", u("seconds") + u("clips") * 10)`
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusFailure
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"seconds": float64(5), "clips": float64(2)},
		EstimatedTier:    "base",
	}

	settled := settleTaskBillingOnComplete(
		ctx,
		&mockAdaptor{adjustReturn: 1},
		task,
		&relaycommon.TaskInfo{Status: model.TaskStatusFailure, UsageFacts: map[string]any{"seconds": float64(8)}},
	)

	assert.False(t, settled)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, map[string]any{"seconds": float64(5), "clips": float64(2)}, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
	assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_TieredSuccessStillRecomputes(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 38
	const initialQuota, preConsumed = 10_000, 50
	seedUser(t, userID, initialQuota)

	expression := `tier("base", u("seconds") + u("clips") * 10)`
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"seconds": float64(5), "clips": float64(2)},
		EstimatedTier:    "base",
	}

	settled := settleTaskBillingOnComplete(
		ctx,
		&mockAdaptor{adjustReturn: 1},
		task,
		&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"seconds": float64(8)}},
	)

	assert.True(t, settled)
	assert.Equal(t, 28, task.Quota)
	assert.Equal(t, map[string]any{"seconds": float64(8), "clips": float64(2)}, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
	assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)
	assert.Equal(t, initialQuota+(preConsumed-28), getUserQuota(t, userID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, "base", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"seconds": float64(8), "clips": float64(2)}, facts)
}

func TestSettle_TieredTokenSnapshotUsesTaskUsageAndFrozenRequest(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, preConsumed = 39, 500
	const initialQuota = 10_000
	seedUser(t, userID, initialQuota)

	expression := `param("tier") == "fast" ? tier("fast", p * 10 + c * 20) : tier("base", p + c)`
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
		GroupRatio: 1, QuotaPerUnit: 500_000, ExprVersion: 1, EstimatedTier: "fast",
	}
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredRequestInput = &billingexpr.RequestInput{Body: []byte(`{"tier":"fast"}`)}

	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 100, CompletionTokens: 50, TotalTokens: 150,
	})

	assert.True(t, settled)
	assert.Equal(t, 1_000, task.Quota)
	assert.Equal(t, initialQuota+(preConsumed-1_000), getUserQuota(t, userID))
	assert.Equal(t, "fast", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)
}

func TestSettle_TieredTokenSnapshotKeepsPreconsumeWhenUsageMissing(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, preConsumed = 45, 500
	seedUser(t, userID, 10_000)
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: `tier("base", p * 10)`,
		ExprHash:   billingexpr.ExprHashString(`tier("base", p * 10)`),
		GroupRatio: 1, QuotaPerUnit: 500_000, ExprVersion: 1,
	}
	task.PrivateData.BillingContext.NativeInteractions = true

	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{adjustReturn: 1}, task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess})

	assert.False(t, settled)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, 10_000, getUserQuota(t, userID))
}

func TestBuildTieredTokenParamsForImageInteractionTaskNormalizesModalities(t *testing.T) {
	taskInfo := &relaycommon.TaskInfo{
		InputTokens: 100, CompletionTokens: 40, TextOutputTokens: 10, CachedTokens: 10, ImageInputTokens: 20,
		AudioInputTokens: 5, AudioOutputTokens: 8,
	}
	params := BuildTieredTokenParamsForTask(taskInfo, "gemini-3-pro-image", `p + c + cr + img + img_o + ai + ao`)
	assert.Equal(t, float64(65), params.P)
	assert.Equal(t, float64(2), params.C)
	assert.Equal(t, float64(10), params.CR)
	assert.Equal(t, float64(20), params.Img)
	assert.Equal(t, float64(30), params.ImgO)
	assert.Equal(t, float64(5), params.AI)
	assert.Equal(t, float64(8), params.AO)
}

func TestBuildTieredTokenParamsForObservedImageOutputDoesNotDependOnModelName(t *testing.T) {
	taskInfo := &relaycommon.TaskInfo{
		InputTokens: 7, CompletionTokens: 1120, TotalTokens: 1127,
		ImageOutputTokens: 1120,
	}
	params := BuildTieredTokenParamsForTask(taskInfo, "gemini-3.5-flash", `p + c + img_o`)
	require.Equal(t, float64(7), params.P)
	require.Zero(t, params.C)
	require.Equal(t, float64(1120), params.ImgO)
}

func TestBuildTieredTokenParamsForTaskUsesAggregateOutputIncludingThoughts(t *testing.T) {
	params := BuildTieredTokenParamsForTask(&relaycommon.TaskInfo{
		InputTokens: 12, CompletionTokens: 90, TotalTokens: 112, ThoughtTokens: 10,
	}, "gemini-omni-flash-preview", `p + c`)
	assert.Equal(t, float64(100), params.C)
}

func TestSettle_NativeInteractionsUsageExpressionRequiresAllValidatedFacts(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, preConsumed = 47, 500
	seedUser(t, userID, 10_000)
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr",
		ExprString:  `u("mode") == "fast" ? tier("fast", u("seconds") * 10) : tier("base", 1)`,
		ExprHash:    billingexpr.ExprHashString(`u("mode") == "fast" ? tier("fast", u("seconds") * 10) : tier("base", 1)`),
		GroupRatio:  1, QuotaPerUnit: 1_000, ExprVersion: 1, TaskUsageBilling: true,
	}

	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{adjustReturn: 1}, task, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true,
	})

	assert.False(t, settled)
	assert.True(t, task.PrivateData.BillingContext.TieredUsageMissing)
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, 10_000, getUserQuota(t, userID))
}

func TestSettle_NativeInteractionsTieredUsesAtomicJournalForQuotaDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID, preConsumed = 647, 647, 647, 500
	seedUser(t, userID, 9_500)
	seedToken(t, tokenID, userID, "sk-interactions-atomic", 9_500)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, tokenID, "wallet", 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: `tier("base", p * 1)`,
		ExprHash:   billingexpr.ExprHashString(`tier("base", p * 1)`),
		GroupRatio: 1, QuotaPerUnit: 1_000_000, ExprVersion: 1,
	}
	require.NoError(t, task.Insert())
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Update("status", model.TaskStatusSuccess).Error)

	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 100, TotalTokens: 100,
	})
	require.True(t, settled)
	require.Equal(t, 100, task.Quota)
	assert.Equal(t, 9_900, getUserQuota(t, userID))
	var savedTask model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&savedTask).Error)
	assert.Equal(t, 100, savedTask.Quota)
	var settlement model.TaskBillingSettlement
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&settlement).Error)
	assert.Equal(t, model.TaskBillingSettlementSettled, settlement.Status)
	var outboxCount int64
	require.NoError(t, model.DB.Model(&model.TaskBillingLogOutbox{}).Where("task_id = ?", task.TaskID).Count(&outboxCount).Error)
	assert.EqualValues(t, 1, outboxCount)
	var outbox model.TaskBillingLogOutbox
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&outbox).Error)
	var payload map[string]any
	require.NoError(t, common.Unmarshal([]byte(outbox.Payload), &payload))
	other, ok := payload["other"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(`tier("base", p * 1)`)), other["expr_b64"])
	assert.Equal(t, "base", other["matched_tier"])
	assert.Equal(t, float64(100), other["input_tokens"])
	assert.Equal(t, float64(100), payload["prompt_tokens"])
}

func TestRecoverPendingInteractionsTieredSettlementFromStoredTaskResult(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, preConsumed = 650, 650, 500
	seedUser(t, userID, 9_500)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, 0, "wallet", 0)
	task.Platform = constant.TaskPlatformVertexInteractions
	task.Data = json.RawMessage(`{"stored":"terminal-response"}`)
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: `tier("base", p * 1)`,
		ExprHash:   billingexpr.ExprHashString(`tier("base", p * 1)`),
		GroupRatio: 1, QuotaPerUnit: 1_000_000, ExprVersion: 1,
	}
	require.NoError(t, task.Insert())
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Update("status", model.TaskStatusSuccess).Error)
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &recoveryTaskAdaptor{result: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 100, TotalTokens: 100,
		}}
	}
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	recoverPendingInteractionsTaskSettlements(ctx, 100)
	var settlement model.TaskBillingSettlement
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&settlement).Error)
	assert.Equal(t, model.TaskBillingSettlementSettled, settlement.Status)
	var saved model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&saved).Error)
	assert.Equal(t, 100, saved.Quota)
}

func TestUsageUnavailableSettlementCanRecoverWithLaterUsageAndDistinctLogEvent(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, preConsumed = 651, 651, 500
	seedUser(t, userID, 9_500)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	expression := `tier("base", u("seconds") * 10)`
	task := makeTask(userID, channelID, preConsumed, 0, "wallet", 0)
	task.Platform = constant.TaskPlatformVertexInteractions
	task.Data = json.RawMessage(`{"stored":"terminal-response"}`)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
		GroupRatio: 1, QuotaPerUnit: 100, ExprVersion: 1, TaskUsageBilling: true,
	}
	require.NoError(t, task.Insert())
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Update("status", model.TaskStatusSuccess).Error)
	assert.True(t, settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess,
	}))
	var settlement model.TaskBillingSettlement
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&settlement).Error)
	assert.Equal(t, model.TaskBillingSettlementUsageUnavailable, settlement.Status)
	deliverPendingTaskBillingLogs(ctx, 100) // acknowledges the zero-delta audit without a customer log

	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor {
		return &recoveryTaskAdaptor{result: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess, UsageAvailable: true, UsageFacts: map[string]any{"seconds": float64(0.2)},
		}}
	}
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })
	require.NoError(t, model.DB.Model(&model.TaskBillingSettlement{}).Where("id = ?", settlement.ID).Update("updated_at", 1).Error)
	recoverPendingInteractionsTaskSettlements(ctx, 100)
	deliverPendingTaskBillingLogs(ctx, 100)
	require.NoError(t, model.DB.Where("id = ?", settlement.ID).First(&settlement).Error)
	assert.Equal(t, model.TaskBillingSettlementSettled, settlement.Status)
	assert.Equal(t, 200, settlement.ActualQuota)
	assert.Equal(t, 9_800, getUserQuota(t, userID))
	var correctionLogCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", "interactions-settlement:"+task.TaskID+":settled").Count(&correctionLogCount).Error)
	assert.EqualValues(t, 1, correctionLogCount)
}

func TestTaskBillingOutboxDeliveryUsesStableRequestIDForRetry(t *testing.T) {
	truncate(t)
	seedUser(t, 648, 10_000)
	payload, err := model.EncodeTaskBillingPayload(map[string]any{
		"user_id": 648, "log_type": model.LogTypeRefund, "content": "settlement",
		"channel_id": 0, "model_name": "gemini-3-pro-image", "quota": 25,
		"token_id": 0, "group": "default", "other": map[string]any{"task_id": "outbox-retry"},
	})
	require.NoError(t, err)
	_, err = model.CreateTaskBillingLogOutbox("interactions-settlement:outbox-retry", "outbox-retry", payload)
	require.NoError(t, err)
	deliverPendingTaskBillingLogs(context.Background(), 100)
	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", "interactions-settlement:outbox-retry").Count(&logs).Error)
	assert.EqualValues(t, 1, logs)

	// Simulate the outbox acknowledgement being lost after the log write.
	require.NoError(t, model.DB.Model(&model.TaskBillingLogOutbox{}).
		Where("operation_id = ?", "interactions-settlement:outbox-retry").
		Updates(map[string]any{"status": model.TaskBillingLogOutboxPending, "updated_at": time.Now().Unix()}).Error)
	deliverPendingTaskBillingLogs(context.Background(), 100)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", "interactions-settlement:outbox-retry").Count(&logs).Error)
	assert.EqualValues(t, 1, logs)
}

func TestSettle_NativeInteractionsMixedUsageAndTokenExpression(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, channelID, preConsumed = 649, 649, 2_000
	seedUser(t, userID, 8_000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, 0, "wallet", 0)
	task.Status = model.TaskStatusSuccess
	task.PrivateData.BillingContext.NativeInteractions = true
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		BillingMode: "tiered_expr", ExprString: `tier("base", u("seconds") * 0.4 + p * 9.8 / 1000000)`,
		ExprHash:   billingexpr.ExprHashString(`tier("base", u("seconds") * 0.4 + p * 9.8 / 1000000)`),
		GroupRatio: 1, QuotaPerUnit: 1_000, ExprVersion: 1, TaskUsageBilling: true,
	}
	require.NoError(t, task.Insert())
	require.NoError(t, model.DB.Model(&model.Task{}).Where("task_id = ?", task.TaskID).Update("status", model.TaskStatusSuccess).Error)
	settled := settleTaskBillingOnComplete(ctx, &mockAdaptor{}, task, &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, UsageAvailable: true, InputTokens: 100_000, TotalTokens: 100_000,
		UsageFacts: map[string]any{"seconds": float64(2)},
	})
	require.True(t, settled)
	assert.Equal(t, 1_780, task.Quota)
	assert.Equal(t, 8_220, getUserQuota(t, userID))
}

func TestSettle_TieredUsageFactsMergeCompletionOverSubmission(t *testing.T) {
	tests := []struct {
		name            string
		completionFacts map[string]any
		expectedQuota   int
		expectedFacts   map[string]any
	}{
		{
			name:          "submission facts survive missing completion facts",
			expectedQuota: 25,
			expectedFacts: map[string]any{"seconds": float64(5), "clips": float64(2)},
		},
		{
			name:            "completion facts partially override submission facts",
			completionFacts: map[string]any{"seconds": float64(8)},
			expectedQuota:   28,
			expectedFacts:   map[string]any{"seconds": float64(8), "clips": float64(2)},
		},
		{
			name:            "completion facts fully override submission facts",
			completionFacts: map[string]any{"seconds": float64(8), "clips": float64(3)},
			expectedQuota:   38,
			expectedFacts:   map[string]any{"seconds": float64(8), "clips": float64(3)},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			truncate(t)
			const userID = 34
			const initialQuota = 10_000
			const preConsumed = 50
			seedUser(t, userID, initialQuota)

			expression := `tier("base", u("seconds") + u("clips") * 10)`
			submissionFacts := map[string]any{"seconds": float64(5), "clips": float64(2)}
			task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
			task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
				ExprString:       expression,
				ExprHash:         billingexpr.ExprHashString(expression),
				GroupRatio:       1,
				QuotaPerUnit:     1,
				ExprVersion:      1,
				TaskUsageBilling: true,
				UsageFacts:       submissionFacts,
				EstimatedTier:    "base",
			}

			settled := settleTaskBillingOnComplete(
				context.Background(),
				&mockAdaptor{},
				task,
				&relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: testCase.completionFacts},
			)

			assert.True(t, settled)
			assert.Equal(t, testCase.expectedQuota, task.Quota)
			assert.Equal(t, map[string]any{"seconds": float64(5), "clips": float64(2)}, submissionFacts)
			require.NotNil(t, task.PrivateData.BillingContext.TieredSnapshot)
			assert.Equal(t, testCase.expectedFacts, task.PrivateData.BillingContext.TieredSnapshot.UsageFacts)
			assert.Equal(t, "base", task.PrivateData.BillingContext.TieredSnapshot.EstimatedTier)

			log := getLastLog(t)
			require.NotNil(t, log)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, "tiered_expr", other["billing_mode"])
			assert.Equal(t, "base", other["matched_tier"])
			facts, ok := other["usage_facts"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, testCase.expectedFacts, facts)
			assert.NotContains(t, other, "seconds")
			assert.NotContains(t, other, "clips")
		})
	}
}

func TestSettle_TieredSnapshotWriteBackUsesSettledFactsAndMatchedTier(t *testing.T) {
	truncate(t)
	const userID = 36
	const initialQuota = 10_000
	const preConsumed = 25
	seedUser(t, userID, initialQuota)

	expression := `u("resolution") == "1080P" ? tier("1080P", u("seconds") * 10) : tier("720P", u("seconds") * 5)`
	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.TieredSnapshot = &billingexpr.BillingSnapshot{
		ExprString:       expression,
		ExprHash:         billingexpr.ExprHashString(expression),
		GroupRatio:       1,
		QuotaPerUnit:     1,
		ExprVersion:      1,
		TaskUsageBilling: true,
		UsageFacts:       map[string]any{"resolution": "720P", "seconds": float64(5)},
		EstimatedTier:    "720P",
	}

	settled := settleTaskBillingOnComplete(
		context.Background(),
		&mockAdaptor{},
		task,
		&relaycommon.TaskInfo{
			Status:     model.TaskStatusSuccess,
			UsageFacts: map[string]any{"resolution": "1080P"},
		},
	)

	require.True(t, settled)
	snap := task.PrivateData.BillingContext.TieredSnapshot
	require.NotNil(t, snap)
	assert.Equal(t, map[string]any{"resolution": "1080P", "seconds": float64(5)}, snap.UsageFacts)
	assert.Equal(t, "1080P", snap.EstimatedTier)
	assert.Equal(t, 50, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	assert.Equal(t, "tiered_expr", other["billing_mode"])
	assert.Equal(t, "1080P", other["matched_tier"])
	facts, ok := other["usage_facts"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1080P", facts["resolution"])
	assert.Equal(t, float64(5), facts["seconds"])
	assert.NotContains(t, other, "resolution")
	assert.NotContains(t, other, "seconds")
}

func TestSettle_TokenRecalcFallsBackToCompletionTokens(t *testing.T) {
	previousRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"test-model":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
	})

	tests := []struct {
		name             string
		totalTokens      int
		completionTokens int
		wantSettled      bool
		wantQuota        int
	}{
		{
			name:             "total tokens still win when both are present",
			totalTokens:      80,
			completionTokens: 20,
			wantSettled:      true,
			wantQuota:        80,
		},
		{
			name:             "completion tokens trigger recalc when total is zero",
			totalTokens:      0,
			completionTokens: 80,
			wantSettled:      true,
			wantQuota:        80,
		},
		{
			name:        "neither token count skips recalc",
			wantSettled: false,
			wantQuota:   50,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			truncate(t)
			const userID, tokenID, channelID = 35, 35, 35
			const initialQuota, preConsumed, tokenRemain = 10_000, 50, 8_000
			seedUser(t, userID, initialQuota)
			seedToken(t, tokenID, userID, "sk-completion-fallback", tokenRemain)
			seedChannel(t, channelID)

			task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
			settled := settleTaskBillingOnComplete(
				context.Background(),
				&mockAdaptor{},
				task,
				&relaycommon.TaskInfo{
					Status:           model.TaskStatusSuccess,
					TotalTokens:      testCase.totalTokens,
					CompletionTokens: testCase.completionTokens,
				},
			)

			assert.Equal(t, testCase.wantSettled, settled)
			assert.Equal(t, testCase.wantQuota, task.Quota)
		})
	}
}
