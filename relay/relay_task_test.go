package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
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

func TestSharedTaskBillingExpressionSelectionAndFrozenSettlement(t *testing.T) {
	const baseExpr = `tier("base", u("seconds") * 2)`
	const alphaExpr = `tier("alpha", u("seconds") * 3)`
	const betaExpr = `tier("beta", u("credits") * 5)`
	const aliasExpr = `tier("alias", u("credits") * 7)`
	for _, tc := range []struct {
		name, plugin, model, mapping, modelExpr, mode, wantExpr string
		variants                                                map[string]string
		wantPriceError, profiled                                bool
	}{
		{name: "executing plugin override", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-alpha::declared-model": alphaExpr, "billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "override ignores model mode", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "ratio", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "model expression fallback", plugin: "billing-alpha", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", wantExpr: baseExpr},
		{name: "alias override precedes mapped override", plugin: "billing-beta", model: "alias-model", mapping: `{"alias-model":"declared-model"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr, "billing-beta::alias-model": aliasExpr}, wantExpr: aliasExpr},
		{name: "mapped override precedes model fallback", plugin: "billing-beta", model: "alias-model", mapping: `{"alias-model":"declared-model"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr},
		{name: "unconfigured plugin cannot use another schema", plugin: "billing-beta", model: "declared-model", modelExpr: baseExpr, mode: "tiered_expr", wantPriceError: true},
		{name: "missing usage in skipped branch remains incompatible", plugin: "billing-beta", model: "declared-model", modelExpr: `true ? tier("free", 0) : tier("missing", u("seconds"))`, mode: "tiered_expr", wantPriceError: true},
		{name: "fixed pricing is still rejected", plugin: "billing-beta", model: "declared-model", variants: map[string]string{"billing-beta::declared-model": `tier("fixed", fixed(1))`}, wantPriceError: true},
		{name: "endpoint mapping keeps the declared profile", plugin: "billing-beta", model: "declared-model", mapping: `{"declared-model":"ep-endpoint"}`, modelExpr: baseExpr, mode: "tiered_expr", variants: map[string]string{"billing-beta::declared-model": betaExpr}, wantExpr: betaExpr, profiled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			registry := pluginruntime.NewRegistry()
			for _, spec := range []struct{ key, field, unit string }{{"billing-alpha", "seconds", "second"}, {"billing-beta", "credits", "credit"}} {
				source := strings.ReplaceAll(billingFallbackPlugin, "bill-fallback", spec.key)
				schema := `usageSchema:{` + spec.field + `:{type:"number",unit:"` + spec.unit + `"}}`
				if tc.profiled && spec.key == "billing-beta" {
					// The endpoint ID is undeclared, so only the declared model's profile carries this schema.
					schema = `usageSchema:{seconds:{type:"number",unit:"second"}},usageProfiles:[{models:["declared-model"],schema:{` + spec.field + `:{type:"number",unit:"` + spec.unit + `"}}}]`
				}
				source = strings.Replace(source, `fetchMode:"per_task"`, `fetchMode:"per_task",`+schema, 1)
				source += `export function extractUsage(){return {` + spec.field + `:2};}`
				_, err := registry.Register(source, pluginruntime.Options{})
				require.NoError(t, err)
			}
			variants := tc.variants
			if variants == nil {
				variants = map[string]string{}
			}
			rawVariants, err := common.Marshal(variants)
			require.NoError(t, err)
			modes, err := common.Marshal(map[string]string{"declared-model": tc.mode})
			require.NoError(t, err)
			expressions, err := common.Marshal(map[string]string{"declared-model": tc.modelExpr})
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: string(rawVariants), "billing_setting.billing_mode": string(modes), "billing_setting.billing_expr": string(expressions),
			}))
			c, info := newTaskSubmitContext(t, tc.model, tc.mapping)
			c.Set("group", "default")
			c.Set("task_plugin_key", tc.plugin)
			info.UserGroup = "default"
			info.UsingGroup = "default"
			info.OriginModelName = tc.model
			plugin, ok := registry.Generation().Get(tc.plugin)
			require.True(t, ok)
			c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
			_, taskErr := RelayTaskSubmit(c, info)
			require.NotNil(t, taskErr) // This fixture stops at reservation, before upstream submission.
			if tc.wantPriceError {
				assert.Equal(t, "model_price_error", taskErr.Code)
				assert.Nil(t, info.TieredBillingSnapshot)
				return
			}
			require.NotNil(t, info.TieredBillingSnapshot, "submission error: %+v", taskErr)
			assert.Equal(t, tc.wantExpr, info.TieredBillingSnapshot.ExprString)
			assert.NotEqual(t, "model_price_error", taskErr.Code)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{billing_setting.PluginBillingExprOption: `{}`, "billing_setting.billing_expr": `{}`}))
			field := "seconds"
			if tc.plugin == "billing-beta" {
				field = "credits"
			}
			result, usage, err := service.EvaluateTaskCompletionUsage(info.TieredBillingSnapshot, map[string]any{field: float64(4)})
			require.NoError(t, err)
			assert.Equal(t, float64(4), usage[field])
			assert.Equal(t, float64(2), info.TieredBillingSnapshot.UsageFacts[field])
			assert.Equal(t, 2*info.TieredBillingSnapshot.EstimatedQuotaAfterGroup, result.ActualQuotaAfterGroup)
			assert.Equal(t, tc.wantExpr, info.TieredBillingSnapshot.ExprString)
		})
	}
}

// Issue #7478: task APIs answer 201 Created or 202 Accepted on submission.
// Every 2xx must reach parseSubmitResponse; only other statuses are upstream
// failures that keep the upstream status and body.
func TestRelayTaskSubmitAcceptsAnySuccessfulUpstreamStatus(t *testing.T) {
	service.InitHttpClient()
	const source = `
export const meta = {apiVersion:1,key:"status-echo",name:"Status Echo",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl+"/submit", method:"POST", body:{model: ctx.model}, action:"text_to_video"}; }
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id, taskData: {status: response.statusCode}}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
`
	for _, tc := range []struct {
		status   int
		wantCode string
	}{
		{status: http.StatusOK},
		{status: http.StatusCreated},
		{status: http.StatusAccepted},
		{status: http.StatusBadRequest, wantCode: "fail_to_fetch_task"},
		{status: http.StatusBadGateway, wantCode: "fail_to_fetch_task"},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			saveBillingConfig(t)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"billing_setting.billing_mode": `{"declared-model":"tiered_expr"}`,
				"billing_setting.billing_expr": `{"declared-model":"tier(\"flat\", 3)"}`,
			}))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"id":"job-42","message":"upstream body"}`))
			}))
			defer server.Close()

			c, info := newTaskSubmitContext(t, "declared-model", "")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			c.Set("group", "default")
			info.UserGroup, info.UsingGroup = "default", "default"
			info.OriginModelName = "declared-model"
			// An existing reservation skips pre-consume so the fixture reaches the upstream call.
			info.Billing = &imageReservation{limit: 1 << 30}
			pinMappingOrderPlugin(t, c, source)

			result, taskErr := RelayTaskSubmit(c, info)
			if tc.wantCode != "" {
				require.NotNil(t, taskErr)
				assert.Equal(t, tc.wantCode, taskErr.Code)
				assert.Equal(t, tc.status, taskErr.StatusCode)
				assert.Contains(t, taskErr.Message, "upstream body")
				assert.Nil(t, result)
				return
			}
			require.Nil(t, taskErr, "submission error: %+v", taskErr)
			require.NotNil(t, result)
			assert.Equal(t, "job-42", result.UpstreamTaskID)
			assert.JSONEq(t, `{"status":`+strconv.Itoa(tc.status)+`}`, string(result.TaskData))
		})
	}
}
