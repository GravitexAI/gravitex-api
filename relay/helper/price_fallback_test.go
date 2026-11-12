package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newFallbackPricingEnv 构造带默认分组的测试上下文。
func newFallbackPricingEnv(t *testing.T) (*gin.Context, *relaycommon.RelayInfo, string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")
	originModel := "fb-requested-" + t.Name()
	info := &relaycommon.RelayInfo{
		OriginModelName: originModel,
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{}`),
		},
	}
	return ctx, info, originModel
}

// TestApplyUpstreamModelFallbackPricingPricedSwap 验证有价回退：倍率换成实际
// 模型的配置，而计费身份（GetBillingModelName）保持在请求模型。
func TestApplyUpstreamModelFallbackPricingPricedSwap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	const actualModel = "fb-actual-priced"
	modelRatios, err := common.Marshal(map[string]float64{
		"fb-requested-TestApplyUpstreamModelFallbackPricingPricedSwap": 30,
		actualModel: 15,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	ctx, info, originModel := newFallbackPricingEnv(t)
	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, float64(30), info.PriceData.ModelRatio)

	info.UpstreamModelFallback = &relaycommon.ModelFallbackInfo{SentModel: originModel, ActualModel: actualModel}
	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.True(t, info.UpstreamModelFallback.Priced)
	require.Equal(t, float64(15), info.PriceData.ModelRatio)
	require.Equal(t, originModel, info.GetBillingModelName())
}

// TestApplyUpstreamModelFallbackPricingUnpricedKeepsOriginal 验证实际模型无价格
// 配置时：保留请求模型的价格，Priced=false 供人工补差额。
func TestApplyUpstreamModelFallbackPricingUnpricedKeepsOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	modelRatios, err := common.Marshal(map[string]float64{
		"fb-requested-TestApplyUpstreamModelFallbackPricingUnpricedKeepsOriginal": 30,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	ctx, info, originModel := newFallbackPricingEnv(t)
	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)

	info.UpstreamModelFallback = &relaycommon.ModelFallbackInfo{SentModel: originModel, ActualModel: "fb-not-configured-anywhere"}
	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.False(t, info.UpstreamModelFallback.Priced)
	require.Equal(t, float64(30), info.PriceData.ModelRatio)
	require.Equal(t, originModel, info.GetBillingModelName())
}

// TestApplyUpstreamModelFallbackPricingTieredRequestedToRatio 验证请求模型是
// tiered 表达式计费、实际模型是普通倍率：旧快照必须清空，走实际模型倍率。
func TestApplyUpstreamModelFallbackPricingTieredRequestedToRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"fb-tiered-requested":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"fb-tiered-requested":"tier(\"base\", p * 2)"}`,
	}))
	modelRatios, err := common.Marshal(map[string]float64{"fb-tiered-actual-ratio": 12})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "fb-tiered-requested",
		UserGroup:       "default",
		UsingGroup:     "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{}`),
		},
	}

	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NotNil(t, info.TieredBillingSnapshot)

	info.UpstreamModelFallback = &relaycommon.ModelFallbackInfo{SentModel: "fb-tiered-requested", ActualModel: "fb-tiered-actual-ratio"}
	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.True(t, info.UpstreamModelFallback.Priced)
	require.Nil(t, info.TieredBillingSnapshot)
	require.Equal(t, float64(12), info.PriceData.ModelRatio)
}

// TestApplyUpstreamModelFallbackPricingRatioToTieredActual 验证实际模型是 tiered
// 表达式计费：快照按实际模型重建。
func TestApplyUpstreamModelFallbackPricingRatioToTieredActual(t *testing.T) {
	gin.SetMode(gin.TestMode)
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"fb-tiered-actual":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"fb-tiered-actual":"tier(\"base\", p * 3)"}`,
	}))
	modelRatios, err := common.Marshal(map[string]float64{"fb-ratio-requested": 30})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "fb-ratio-requested",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{}`),
		},
	}

	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Nil(t, info.TieredBillingSnapshot)

	info.UpstreamModelFallback = &relaycommon.ModelFallbackInfo{SentModel: "fb-ratio-requested", ActualModel: "fb-tiered-actual"}
	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.True(t, info.UpstreamModelFallback.Priced)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "fb-tiered-actual", info.TieredBillingSnapshot.ModelName)
	require.Equal(t, "fb-ratio-requested", info.GetBillingModelName())
}

// TestApplyUpstreamModelFallbackPricingSkipsImagePriced 验证实际模型是按张/
// 图片计费时跳过重定价（文本结算无法按图片分支走），保留原价格。
func TestApplyUpstreamModelFallbackPricingSkipsImagePriced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
		// 按张计费缓存没有快照导出接口，测试包内无人配置它，重置回空即可。
		require.NoError(t, ratio_setting.UpdateImageModelPricePerImageByJSONString("{}"))
	})
	modelRatios, err := common.Marshal(map[string]float64{
		"fb-requested-TestApplyUpstreamModelFallbackPricingSkipsImagePriced": 30,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))
	perImage, err := common.Marshal(map[string]float64{"fb-actual-image-priced": 0.04})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateImageModelPricePerImageByJSONString(string(perImage)))

	ctx, info, originModel := newFallbackPricingEnv(t)
	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)

	info.UpstreamModelFallback = &relaycommon.ModelFallbackInfo{SentModel: originModel, ActualModel: "fb-actual-image-priced"}
	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.False(t, info.UpstreamModelFallback.Priced)
	require.Equal(t, float64(30), info.PriceData.ModelRatio)
}

// TestApplyUpstreamModelFallbackPricingNoFallbackNoop 验证无回退时是 no-op。
func TestApplyUpstreamModelFallbackPricingNoFallbackNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedModelRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatios))
	})
	modelRatios, err := common.Marshal(map[string]float64{
		"fb-requested-TestApplyUpstreamModelFallbackPricingNoFallbackNoop": 30,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatios)))

	ctx, info, _ := newFallbackPricingEnv(t)
	_, err = ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	priceDataBefore := info.PriceData

	ApplyUpstreamModelFallbackPricing(ctx, info)

	require.Nil(t, info.UpstreamModelFallback)
	require.Equal(t, priceDataBefore, info.PriceData)
}
