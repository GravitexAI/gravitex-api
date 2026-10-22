package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真实生产记录 SN_RaqDKNsLQrtoPxPaTwA8（gemini-4-argon，倍率计费）：
//
//	promptTokenCount 24549 = TEXT 16012 + IMAGE 8537，cachedContentTokenCount 21831
//	candidatesTokenCount 31 + thoughtsTokenCount 12 = 43
//
// 非缓存输入只有 24549-21831=2718，却有 8537 个图片 token，图片与缓存必然重叠。
// 修复前图片被二次扣减，baseTokens 落到 -5819 并被 clamp 成 0，重叠的 5819 个
// token 同时按缓存价和图片价各计费一次，线上记录的 quota 是 6398。
func TestCalculateTextQuotaSummaryDoesNotDoubleChargeCachedImageTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-4-argon",
		PriceData: hosttypes.PriceData{
			ModelRatio:      1,
			CompletionRatio: 5,
			CacheRatio:      0.05,
			ImageRatio:      1,
			GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 0.65},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     24549,
		CompletionTokens: 43,
		TotalTokens:      24592,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 16012, ImageTokens: 8537, CachedTokens: 21831,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 12, TextTokens: 31},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 非缓存 2718 按输入价 + 缓存 21831 按 0.05 + 输出 43 按 5，再 ×ModelRatio×GroupRatio
	// (2718 + 1091.55 + 215) × 1 × 0.65 = 2615.96
	assert.Equal(t, 2616, summary.Quota)
}

// 同一条记录：账单展示的文本输入 token 不能因为重叠而塌成 0。
func TestCalculateTextQuotaSummaryKeepsImageTokensWithinPromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-4-argon",
		PriceData: hosttypes.PriceData{
			ModelRatio: 1, CompletionRatio: 5, CacheRatio: 0.05, ImageRatio: 1,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 0.65},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens: 24549, CompletionTokens: 43, TotalTokens: 24592,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 16012, ImageTokens: 8537, CachedTokens: 21831,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	assert.LessOrEqual(t, summary.ImageTokens, summary.PromptTokens-summary.CacheTokens,
		"billable image tokens must fit inside the uncached input")
}

// 真实生产记录 pzO9auP8BpXRi9sPprO96Ao（gemini-3.1-pro-preview，分档表达式计费）。
// 表达式只引用 p/c/cr，不引用 img，所以图片从未被二次扣减，这条账本来就是对的。
// 修复不得改动它的金额：线上记录的 quota 是 10848。
func TestTryTieredSettleKeepsCorrectChargeUnchanged(t *testing.T) {
	expr := `p <= 200000 ? tier("short context", p * 2 + c * 12 + cr * 0.2) : ` +
		`p > 200000 ? tier("long context", p * 4 + c * 18 + cr * 0.4) : ` +
		`tier("default", p * 2 + c * 12 + cr * 0.2)`

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3.1-pro-preview",
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr", ExprString: expr, ExprHash: expr, GroupRatio: 0.65, QuotaPerUnit: 500000,
		},
	}
	usage := &dto.Usage{
		PromptTokens: 35537, CompletionTokens: 1762, TotalTokens: 37299,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 1967, ImageTokens: 33570, CachedTokens: 32689,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 1270, TextTokens: 492},
	}

	ok, quota, _ := TryTieredSettle(relayInfo, BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expr)))

	require.True(t, ok)
	assert.Equal(t, 10848, quota)
}

// gemini-3.5-flash 的线上表达式同时引用 cr 和 img，因此会对同一批 token 连续扣减
// 两次。img 与 p 的系数同为 1.5（图文同价），正确金额应与「只扣缓存」完全一致。
func TestTryTieredSettleDoesNotDoubleChargeWhenExpressionUsesBothCrAndImg(t *testing.T) {
	const flashExpr = `p <= 200000 ? tier("short context", p * 1.5 + c * 9 + cr * 0.15 + img * 1.5 + ai * 1.5) : ` +
		`p > 200000 ? tier("long context", p * 1.5 + c * 9 + cr * 0.15 + img * 1.5 + ai * 1.5) : ` +
		`tier("default", p * 1.5 + c * 9 + cr * 0.15 + img * 1.5 + ai * 1.5)`
	// 同样的价格，但不拆模态 —— 图文同价时两者必须等价，且都等于正确金额。
	const plainExpr = `tier("default", p * 1.5 + c * 9 + cr * 0.15)`
	// (2848×1.5 + 1762×9 + 32689×0.15) ÷ 2 × 0.65 = 8135.84
	const wantQuota = 8136

	usage := &dto.Usage{
		PromptTokens: 35537, CompletionTokens: 1762, TotalTokens: 37299,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: 1967, ImageTokens: 33570, CachedTokens: 32689,
		},
	}
	settle := func(expr string) int {
		relayInfo := &relaycommon.RelayInfo{
			OriginModelName: "gemini-3.5-flash",
			TieredBillingSnapshot: &billingexpr.BillingSnapshot{
				BillingMode: "tiered_expr", ExprString: expr, ExprHash: expr, GroupRatio: 0.65, QuotaPerUnit: 500000,
			},
		}
		ok, quota, _ := TryTieredSettle(relayInfo, BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expr)))
		require.True(t, ok)
		return quota
	}

	assert.Equal(t, wantQuota, settle(plainExpr), "baseline expression without modality split")
	assert.Equal(t, wantQuota, settle(flashExpr), "cr+img expression must charge the same")
}

// 生产的 Gemini 计费走 BillingUsage（日志 usage_billing_path="upstream"），所以
// cacheTokensDetails 必须一路穿过 BillingUsage → canonical usage → 计费，而不是
// 只在直连解析里可见。这里用异价场景断言：缓存的模态拆分一旦生效，图片单价只
// 应作用在未命中缓存的那部分上。
func TestCalculateTextQuotaSummaryUsesCacheTokensDetailsFromBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3-pro-preview",
		PriceData: hosttypes.PriceData{
			ModelRatio: 1, CompletionRatio: 1, CacheRatio: 0.1,
			ImageRatio:     2, // 异价：只有这种模型才依赖缓存的模态拆分
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount:        1000,
			CachedContentTokenCount: 800,
			TotalTokenCount:         1000,
			PromptTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 400},
				{Modality: "IMAGE", TokenCount: 600},
			},
			CacheTokensDetails: []dto.GeminiPromptTokensDetails{
				{Modality: "TEXT", TokenCount: 300},
				{Modality: "IMAGE", TokenCount: 500},
			},
		}),
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, effectiveBillingUsage(usage))

	// 上游明说缓存里有 500 个图片 token，故只剩 100 个图片按 ×2 计价；
	// 文本 100 按 ×1，缓存 800 按 ×0.1 → 100 + 200 + 80 = 380
	assert.Equal(t, 100, summary.ImageTokens)
	assert.Equal(t, 380, summary.Quota)
}
