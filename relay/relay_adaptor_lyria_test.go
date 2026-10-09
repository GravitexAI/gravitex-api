package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
