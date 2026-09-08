package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestAppendUsageConversion(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{logUsageConversionEnabledOption: "true"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	convertedInfo := &relaycommon.RelayInfo{
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}
	convertedInfo.SetUsageConversion(map[string]any{"input_tokens": 10, "output_tokens": 2})
	other := map[string]interface{}{}
	appendUsageConversion(other, convertedInfo)

	require.Equal(t, map[string]any{
		"input_tokens":  float64(10),
		"output_tokens": float64(2),
	}, other["usage_conversion"])

	responseOnlyConvertedInfo := &relaycommon.RelayInfo{
		RequestConversionChain: []types.RelayFormat{types.RelayFormatClaude},
	}
	responseOnlyConvertedInfo.SetUsageConversion(map[string]any{"input_tokens": 10, "output_tokens": 2})
	responseOnlyOther := map[string]interface{}{}
	appendUsageConversion(responseOnlyOther, responseOnlyConvertedInfo)
	require.Equal(t, map[string]any{
		"input_tokens":  float64(10),
		"output_tokens": float64(2),
	}, responseOnlyOther["usage_conversion"])
}

func TestAppendUpstreamResponsesPersistsTargetImageModelsForImageEndpointsWhenGlobalLoggingIsDisabled(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{logUpstreamResponsesEnabledOption: "false"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	tests := []struct {
		name  string
		model string
		mode  int
		want  bool
	}{
		{name: "pro edits", model: "dola-seedream-5-0-pro-260628", mode: constant.RelayModeImagesEdits, want: true},
		{name: "pro generations", model: "dola-seedream-5-0-pro-260628", mode: constant.RelayModeImagesGenerations, want: true},
		{name: "nsfw edits", model: "seedream-5-0-pro-NSFW", mode: constant.RelayModeImagesEdits, want: true},
		{name: "nsfw generations", model: "seedream-5-0-pro-NSFW", mode: constant.RelayModeImagesGenerations, want: true},
		{name: "other image model", model: "seedream-4-5-251128", mode: constant.RelayModeImagesGenerations, want: false},
		{name: "target model on chat", model: "dola-seedream-5-0-pro-260628", mode: constant.RelayModeChatCompletions, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: tt.model,
				RelayMode:       tt.mode,
				UpstreamResponses: map[string]any{
					"usage": dto.Usage{InputImages: 1, GeneratedImages: 1, OutputTokens: 4096, TotalTokens: 4096},
				},
				RawUpstreamUsage: []byte(`{"output_tokens":4096, "input_images":1}`),
			}
			other := map[string]interface{}{}
			appendUpstreamResponses(other, info)
			if tt.want {
				require.NotNil(t, other["upstream_responses"])
				responses := other["upstream_responses"].(map[string]any)
				require.Equal(t, json.RawMessage(`{"output_tokens":4096, "input_images":1}`), responses["usage"])
			} else {
				require.Nil(t, other["upstream_responses"])
			}
		})
	}
}
