package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoFetchByIDRespBodyBuilder_RawMirror_CachedTerminalTask_ReturnsRawData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTaskTestDB(t)

	rawUpstreamJSON := `{"id":"cgt-xyz","model":"seedance-2-0","status":"succeeded","content":{"video_url":"https://example.com/v.mp4"},"billing_processed":true}`
	task := &model.Task{
		TaskID:    "cgt-xyz",
		Platform:  "54", // channel type 54 (ChannelTypeDoubaoVideo) resolves via GetTaskAdaptor to taskdoubao.TaskAdaptor, which implements OpenAIVideoConverter
		UserId:    77,
		ChannelId: 1,
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		Quota:     100,
		// Data must contain billing_processed:true — isVideoTaskBillingProcessed reads this flag
		// (not Quota) to decide that tryRealtimeFetch should skip the live upstream refetch.
		Data: []byte(rawUpstreamJSON),
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Type: 54}).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(common.KeySeedanceRawMirror, true)
	c.Set("id", 77)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/video/generations/cgt-xyz", nil)
	c.Params = gin.Params{{Key: "task_id", Value: "cgt-xyz"}}

	respBody, taskErr := videoFetchByIDRespBodyBuilder(c)
	require.Nil(t, taskErr)
	assert.JSONEq(t, rawUpstreamJSON, string(respBody))
}

func TestNativeInteractionsTieredPriceUsesOriginalInputAndOutputLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() { require.NoError(t, config.GlobalConfig.LoadFromDB(saved)) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"interactions-tiered":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"interactions-tiered":"param(\"service_tier\") == \"fast\" ? tier(\"fast\", p * 10000 + c * 20000) : tier(\"base\", p * 1000 + c * 2000)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/interactions", nil)
	c.Set("group", "default")
	raw := []byte(`{"model":"interactions-tiered","input":"hello","service_tier":"fast","max_output_tokens":10}`)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "interactions-tiered",
		UserGroup:          "default",
		UsingGroup:         "default",
		RequestHeaders:     map[string]string{"Content-Type": "application/json"},
	}

	price, handled, err := nativeInteractionsTieredPrice(c, info, raw)
	require.NoError(t, err)
	require.True(t, handled)
	require.NotNil(t, info.TieredBillingSnapshot)
	assert.Equal(t, "fast", info.TieredBillingSnapshot.EstimatedTier)
	assert.Equal(t, 10, info.TieredBillingSnapshot.EstimatedCompletionTokens)
	assert.Greater(t, price.Quota, 0)
	assert.Equal(t, price.Quota, info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)

	var input map[string]any
	require.NoError(t, common.Unmarshal(info.BillingRequestInput.Body, &input))
	assert.Equal(t, "hello", input["input"])
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		channelInfo := &relaycommon.RelayInfo{
			NativeInteractions: true, OriginModelName: "interactions-tiered", UserGroup: "default", UsingGroup: "default",
			ChannelMeta:    &relaycommon.ChannelMeta{ChannelType: channelType},
			RequestHeaders: map[string]string{"Content-Type": "application/json"},
		}
		channelPrice, channelHandled, channelErr := nativeInteractionsTieredPrice(c, channelInfo, raw)
		require.NoError(t, channelErr)
		require.True(t, channelHandled)
		assert.Equal(t, price.Quota, channelPrice.Quota)
		assert.Equal(t, "fast", channelInfo.TieredBillingSnapshot.EstimatedTier)
	}

	for _, testCase := range []struct {
		name               string
		body               string
		expectedCompletion int
	}{
		{name: "omitted output limit uses bounded estimate", body: `{"model":"interactions-tiered","input":"hello","service_tier":"fast"}`, expectedCompletion: 8192},
		{name: "explicit zero output limit is preserved", body: `{"model":"interactions-tiered","input":"hello","service_tier":"fast","max_output_tokens":0}`, expectedCompletion: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requestInfo := &relaycommon.RelayInfo{
				NativeInteractions: true, OriginModelName: "interactions-tiered", UserGroup: "default", UsingGroup: "default",
				RequestHeaders: map[string]string{"Content-Type": "application/json"},
			}
			price, handled, err := nativeInteractionsTieredPrice(c, requestInfo, []byte(testCase.body))
			require.NoError(t, err)
			require.True(t, handled)
			assert.Equal(t, testCase.expectedCompletion, requestInfo.TieredBillingSnapshot.EstimatedCompletionTokens)
			if testCase.expectedCompletion == 0 {
				assert.Less(t, price.Quota, info.PriceData.Quota)
			}
		})
	}
}

func TestNativeInteractionsTieredPriceDoesNotHandleOtherEndpoints(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "interactions-tiered"}
	_, handled, err := nativeInteractionsTieredPrice(nil, info, []byte(`{"input":"hello"}`))
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestShouldSkipRealtimeFetchForLyriaUsesPersistedCreateResponse(t *testing.T) {
	task := &model.Task{
		Platform: constant.TaskPlatformLyria,
		Status:   model.TaskStatusSuccess,
		Data:     []byte(`{"id":"interaction-1","status":"completed","outputs":[]}`),
	}

	require.True(t, shouldSkipRealtimeFetch(task, false))
}

func setupRelayTaskTestDB(t *testing.T) {
	t.Helper()
	// See existing setupModelListControllerTestDB in controller/model_list_test.go
	// for the established in-memory-sqlite fixture pattern this mirrors.
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db := mustOpenTestSqlite(t, dsn)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.User{}))
}

func mustOpenTestSqlite(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
