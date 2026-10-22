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

// Gemini 生图的输出按「图片档 / 文本档」两档计价，拆分依据是
// candidatesTokensDetails。该数组里还可能出现 AUDIO 等模态，它们既不在文本档也
// 不在图片档，此前会被整段漏掉而不计费。任何没有归入某个档位的输出 token 都应
// 落回文本输出档，宁可按文本价收也不能白送。
func TestCalculateTextQuotaSummaryBillsUnaccountedOutputModalities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3.1-flash-image",
		PriceData: hosttypes.PriceData{
			ModelRatio: 1, CompletionRatio: 2, ImageCompletionRatio: 10, ImageRatio: 1,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens: 100, CompletionTokens: 1000, TotalTokens: 1100,
		PromptTokensDetails: dto.InputTokenDetails{TextTokens: 100},
		CompletionTokenDetails: dto.OutputTokenDetails{
			ImageTokens: 600, TextTokens: 100, AudioTokens: 300,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 图片 600×10 + （文本 100 + 未归档的音频 300）×2 = 6800，加输入 100
	assert.Equal(t, 6900, summary.Quota)
}

// 归档数之和超过 completion_tokens 时（上游口径漂移）不得倒扣。
func TestCalculateTextQuotaSummaryDoesNotCreditWhenOutputDetailsOvershoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gemini-3.1-flash-image",
		PriceData: hosttypes.PriceData{
			ModelRatio: 1, CompletionRatio: 2, ImageCompletionRatio: 10, ImageRatio: 1,
			GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens: 100, CompletionTokens: 500, TotalTokens: 600,
		PromptTokensDetails:    dto.InputTokenDetails{TextTokens: 100},
		CompletionTokenDetails: dto.OutputTokenDetails{ImageTokens: 600, TextTokens: 100},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 图片 600×10 + 文本 100×2 = 6200，加输入 100；负的差额不得冲抵
	assert.Equal(t, 6300, summary.Quota)
}
