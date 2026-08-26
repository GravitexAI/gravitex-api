package lyria

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBuildVertexInteractionsURLUsesProjectAndGlobalLocation(t *testing.T) {
	key := `{"project_id":"demo-project"}`

	url, err := buildVertexInteractionsURL("https://aiplatform.googleapis.com", key)

	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", url)
}

func TestBuildVertexInteractionsURLDoesNotDuplicateInteractionsPath(t *testing.T) {
	key := `{"project_id":"demo-project"}`

	url, err := buildVertexInteractionsURL("https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", key)

	require.NoError(t, err)
	require.Equal(t, "https://aiplatform.googleapis.com/v1beta1/projects/demo-project/locations/global/interactions", url)
}

func TestLyriaEndpointKindDetectsVertexByEndpoint(t *testing.T) {
	require.True(t, isVertexInteractionsEndpoint("https://aiplatform.googleapis.com"))
	require.True(t, isVertexInteractionsEndpoint("https://us-central1-aiplatform.googleapis.com"))
	require.False(t, isVertexInteractionsEndpoint("https://generativelanguage.googleapis.com"))
}

func TestVertexLyriaInteractionRequiresChannelEndpointAndModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi},
		OriginModelName:    "lyria-3-pro-preview",
		RequestURLPath:     "/v1/video/generations",
		NativeInteractions: true,
	}

	require.True(t, isVertexLyriaInteraction(info))

	info.NativeInteractions = false
	require.False(t, isVertexLyriaInteraction(info))

	info.NativeInteractions = true
	info.OriginModelName = "lyria-3-experimental"
	require.False(t, isVertexLyriaInteraction(info))
}

func TestLyriaPollingUsesExplicitVertexChannelType(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeVertexAi,
			ChannelBaseUrl: "https://generativelanguage.googleapis.com",
			ApiKey:         "opaque-key-for-routing-test",
		},
	})

	require.True(t, adaptor.shouldUseVertexPolling(
		"https://generativelanguage.googleapis.com",
		"opaque-key-for-routing-test",
	))
}
