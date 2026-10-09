package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGetTaskAdaptorForRequestUsesLyriaOnlyForNativeVertexInteractions(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		NativeInteractions: true,
	}
	platform := constant.TaskPlatform("41")

	adaptor := GetTaskAdaptorForRequest(platform, "lyria-3-pro-preview", info)
	require.NotNil(t, adaptor)
	require.Equal(t, "lyria", adaptor.GetChannelName())

	info.NativeInteractions = false
	nonNative := GetTaskAdaptorForRequest(platform, "lyria-3-pro-preview", info)
	require.NotNil(t, nonNative)
	require.NotEqual(t, "lyria", nonNative.GetChannelName())

	info.NativeInteractions = true
	info.ChannelMeta.ChannelType = constant.ChannelTypeGemini
	nonVertex := GetTaskAdaptorForRequest(constant.TaskPlatform("24"), "lyria-3-pro-preview", info)
	require.NotNil(t, nonVertex)
	require.NotEqual(t, "lyria", nonVertex.GetChannelName())
}

func TestGetTaskAdaptorForRequestUsesInteractionsAdaptorForSupportedVertexModels(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		NativeInteractions: true,
	}

	for _, model := range []string{
		"gemini-2.5-flash-image",
		"gemini-3-flash-preview",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image-preview",
		"gemini-3.5-flash",
		"gemini-3.1-flash-image",
		"gemini-3-pro-image",
		"gemini-3.1-flash-lite-image",
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-2.5-flash-lite",
		"gemini-nano-banana-2.1",
	} {
		adaptor := GetTaskAdaptorForRequest(constant.TaskPlatform("41"), model, info)
		require.NotNil(t, adaptor, model)
		if model == "gemini-omni-flash-preview" {
			require.NotEqual(t, "lyria", adaptor.GetChannelName(), model)
		} else {
			require.Equal(t, "lyria", adaptor.GetChannelName(), model)
		}
	}
	for _, model := range []string{
		"veo-3.1-generate-001",
		"veo-3.1-fast-generate-001",
		"veo-3.1-lite-generate-001",
		"gemini-3.1-pro-preview",
		"gemini-3.1-flash-lite",
		"gemini-omni-flash-preview",
		"gemini-3.6-flash",
		"gemini-3.7-flash",
		"lyria-3-pro-preview",
		"gemini-3.8-flash",
		"gemini-3.5-flash-lite",
	} {
		adaptor := GetTaskAdaptorForRequest(constant.TaskPlatform("41"), model, info)
		require.NotNil(t, adaptor, model)
		require.NotEqual(t, "lyria", adaptor.GetChannelName(), model)
	}
}

func TestNativeInteractionTaskPlatformKeepsLyriaBillingScopeSeparate(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		NativeInteractions: true,
	}

	require.Equal(t, constant.TaskPlatformLyria, nativeInteractionTaskPlatform(constant.TaskPlatform("41"), info, "lyria-3-pro-preview"))
	require.Equal(t, constant.TaskPlatformVertexInteractions, nativeInteractionTaskPlatform(constant.TaskPlatform("41"), info, "gemini-nano-banana-2.1"))
	require.Equal(t, constant.TaskPlatform("41"), nativeInteractionTaskPlatform(constant.TaskPlatform("41"), info, "gemini-3.1-pro-preview"))

	info.ChannelMeta.ChannelType = constant.ChannelTypeGemini
	require.Equal(t, constant.TaskPlatform("24"), nativeInteractionTaskPlatform(constant.TaskPlatform("24"), info, "gemini-nano-banana-2.1"))
}

func TestNativeGenerateContentBillingDoesNotDependOnChannelType(t *testing.T) {
	info := &relaycommon.RelayInfo{NativeInteractions: true}
	require.True(t, isNativeGenerateContentBilling(info, "gemini-nano-banana-2.1"))

	info.NativeInteractions = false
	require.False(t, isNativeGenerateContentBilling(info, "gemini-nano-banana-2.1"))
	info.NativeInteractions = true
	require.False(t, isNativeGenerateContentBilling(info, "lyria-3-pro-preview"))
}

func TestVertexVeoKeepsExistingTaskAdaptor(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		NativeInteractions: true,
	}
	adaptor := GetTaskAdaptorForRequest(constant.TaskPlatform("41"), "veo-3.1-generate-001", info)
	require.NotNil(t, adaptor)
	require.Equal(t, "vertex", adaptor.GetChannelName())
}

func TestNativeGenerateContentUsageQuotaBillsThoughtTokensAtTextRatio(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "gemini-nano-banana-2.1",
		PriceData: hosttypes.PriceData{
			ModelRatio:           0.75, // $1.5 / 1M input tokens
			CompletionRatio:      10,   // $15 / 1M text output tokens
			ImageCompletionRatio: 20,   // $30 / 1M image output tokens
			GroupRatioInfo:       hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	taskInfo := &relaycommon.TaskInfo{
		Status:           model.TaskStatusSuccess,
		InputTokens:      5,
		CompletionTokens: 1680,
		TotalTokens:      2589, // 904 thinking tokens only visible in the total
	}

	quota, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	// 5×$1.5/1M + 1680×$30/1M + 904×$15/1M = $0.0639675 → 31983.75 → 31984
	require.Equal(t, 31984, quota)

	taskInfo.TotalTokens = 1685 // input + output exactly, no thinking tokens
	quotaWithoutThoughts, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	// 5×$1.5/1M + 1680×$30/1M = $0.0504075 → 25203.75 → 25204
	require.Equal(t, 25204, quotaWithoutThoughts)
}

func TestNativeGenerateContentUsageQuotaDiscountsCachedInputTokens(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "gemini-nano-banana-2.1",
		PriceData: hosttypes.PriceData{
			ModelRatio:           0.75, // $1.5 / 1M input tokens
			CompletionRatio:      10,
			ImageCompletionRatio: 20, // $30 / 1M image output tokens
			CacheRatio:           0.2,
			GroupRatioInfo:       hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	taskInfo := &relaycommon.TaskInfo{
		Status:           model.TaskStatusSuccess,
		InputTokens:      1000,
		CachedTokens:     600,
		CompletionTokens: 1680,
		TotalTokens:      2680,
	}

	quota, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	// fresh 400×$1.5/1M + cached 600×$0.3/1M + 1680×$30/1M = $0.05118 → 25590
	require.Equal(t, 25590, quota)

	// A missing cache ratio must fall back to full input price, never a free ride.
	info.PriceData.CacheRatio = 0
	fullPrice, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	// 1000×$1.5/1M + 1680×$30/1M = $0.0519 → 25950
	require.Equal(t, 25950, fullPrice)
}

func TestNativeGenerateContentUsageQuotaSplitsMixedOutputByModality(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "gemini-nano-banana-2.1",
		PriceData: hosttypes.PriceData{
			ModelRatio:           0.75, // $1.5 / 1M input tokens
			CompletionRatio:      10,   // $15 / 1M text output tokens
			ImageCompletionRatio: 20,   // $30 / 1M image output tokens
			GroupRatioInfo:       hosttypes.GroupRatioInfo{GroupRatio: 1},
		},
	}
	taskInfo := &relaycommon.TaskInfo{
		Status:           model.TaskStatusSuccess,
		InputTokens:      5,
		CompletionTokens: 1680,
		TextOutputTokens: 80, // candidatesTokensDetails reported a text share
		TotalTokens:      1685,
	}

	quota, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	// 5×$1.5/1M + 1600×$30/1M + 80×$15/1M = $0.0492075 → 24603.75 → 24604
	require.Equal(t, 24604, quota)

	// Without modality details every output token stays on the image ratio.
	taskInfo.TextOutputTokens = 0
	undetailed, ok := nativeGenerateContentUsageQuota(info, taskInfo)
	require.True(t, ok)
	require.Equal(t, 25204, undetailed)
}
