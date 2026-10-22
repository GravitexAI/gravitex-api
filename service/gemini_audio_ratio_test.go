package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func audioSummary(t *testing.T, model string, audioRatio float64, usage *dto.Usage) textQuotaSummary {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: model,
		PriceData: hosttypes.PriceData{
			ModelRatio: 0.125, CompletionRatio: 6, CacheRatio: 0.1,
			ImageRatio: 1, AudioRatio: audioRatio,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	return calculateTextQuotaSummary(ctx, relayInfo, usage)
}

// AudioRatio 配在 options 里，但文本 relay 路径此前只认一张写死的模型前缀白名单
// （gemini-2.0-flash* / gemini-2.5-flash* / gemini-robotics-er-1.5），gemini-3.x
// 一律返回 0，配置形同虚设、音频按文本价计费。
func TestCalculateTextQuotaSummaryAppliesAudioRatioToGeminiTextRelay(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 1000, CompletionTokens: 0, TotalTokens: 1000,
		PromptTokensDetails: dto.InputTokenDetails{TextTokens: 600, AudioTokens: 400},
	}

	// 音频 ×2：(600 + 400×2) × 0.125 = 175
	assert.Equal(t, 175, audioSummary(t, "gemini-3.1-flash-lite", 2, usage).Quota)
}

// 未配置 AudioRatio 的模型必须保持原状：音频按普通输入价计费，不另立名目。
func TestCalculateTextQuotaSummaryFallsBackToTextPriceWhenAudioRatioUnset(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 1000, CompletionTokens: 0, TotalTokens: 1000,
		PromptTokensDetails: dto.InputTokenDetails{TextTokens: 600, AudioTokens: 400},
	}

	// 1000 × 0.125 = 125
	assert.Equal(t, 125, audioSummary(t, "gemini-3.7-flash", 1, usage).Quota)
}

// 音频与缓存同样是两把刀切同一批 token，启用音频单价后不得重蹈图片的覆辙。
func TestCalculateTextQuotaSummaryDoesNotDoubleChargeCachedAudioTokens(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 1000, CompletionTokens: 0, TotalTokens: 1000,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 600, AudioTokens: 400, CachedTokens: 900,
			CachedTextTokens: 600, CachedAudioTokens: 300,
		},
	}

	// 非缓存只剩音频 100：(0 + 100×2 + 900×0.1) × 0.125 = 36.25
	summary := audioSummary(t, "gemini-3.1-flash-lite", 2, usage)
	assert.Equal(t, 100, summary.AudioTokens)
	assert.Equal(t, 36, summary.Quota)
}
