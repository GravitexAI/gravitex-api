package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1h 缓存创建倍率：配置了按配置计费，未配置按 5m 倍率 × 官方 1h/5m 比例（1.6）推算。
func TestModelPriceHelperCacheCreation1hRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)

	savedModelRatio := ratio_setting.ModelRatio2JSONString()
	savedCreateCache := ratio_setting.CreateCacheRatio2JSONString()
	savedCreateCache1h := ratio_setting.CreateCache1hRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModelRatio))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(savedCreateCache))
		require.NoError(t, ratio_setting.UpdateCreateCache1hRatioByJSONString(savedCreateCache1h))
	})

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"cache-1h-configured":2.5,"cache-1h-derived":2.5}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"cache-1h-configured":1.25,"cache-1h-derived":1.25}`))
	require.NoError(t, ratio_setting.UpdateCreateCache1hRatioByJSONString(`{"cache-1h-configured":3}`))

	tests := []struct {
		name   string
		model  string
		want5m float64
		want1h float64
	}{
		{name: "configured 1h ratio", model: "cache-1h-configured", want5m: 1.25, want1h: 3},
		{name: "derived from 5m ratio", model: "cache-1h-derived", want5m: 1.25, want1h: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			ctx.Set("group", "default")
			info := &relaycommon.RelayInfo{
				OriginModelName: tt.model,
				UserGroup:       "default",
				UsingGroup:      "default",
			}

			priceData, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
			require.NoError(t, err)
			assert.InDelta(t, tt.want5m, priceData.CacheCreation5mRatio, 1e-9)
			assert.InDelta(t, tt.want1h, priceData.CacheCreation1hRatio, 1e-9)
		})
	}
}
