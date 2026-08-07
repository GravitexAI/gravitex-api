package tokenhub

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestTokenHubResponsesRequestURL(t *testing.T) {
	adaptor := &Adaptor{}
	requestURL, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://tokenhub.tencentcloudmaas.com"},
		RelayMode:   relayconstant.RelayModeResponses,
	})
	require.NoError(t, err)
	require.Equal(t, "https://tokenhub.tencentcloudmaas.com/v1/responses", requestURL)

	chatURL, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://tokenhub.tencentcloudmaas.com"},
	})
	require.NoError(t, err)
	require.Equal(t, "https://tokenhub.tencentcloudmaas.com/v1/chat/completions", chatURL)
}

func TestTokenHubConvertOpenAIResponsesRequest(t *testing.T) {
	adaptor := &Adaptor{}
	for _, model := range []string{
		"deepseek-v4-flash-202605",
		"deepseek-v4-pro-202606",
		"deepseek-v4-flash",
		"deepseek-v4-pro",
	} {
		t.Run(model, func(t *testing.T) {
			request := dto.OpenAIResponsesRequest{Model: model}
			converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, nil, request)
			require.NoError(t, err)
			require.Equal(t, request, converted)
		})
	}

	_, err := adaptor.ConvertOpenAIResponsesRequest(nil, nil, dto.OpenAIResponsesRequest{Model: "glm-5"})
	require.ErrorContains(t, err, "only supports")
}
