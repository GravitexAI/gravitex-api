package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
