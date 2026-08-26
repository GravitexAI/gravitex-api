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
