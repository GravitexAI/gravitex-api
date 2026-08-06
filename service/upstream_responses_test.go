package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
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

	directInfo := &relaycommon.RelayInfo{
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI},
	}
	directInfo.SetUsageConversion(map[string]any{"prompt_tokens": 10})
	directOther := map[string]interface{}{}
	appendUsageConversion(directOther, directInfo)
	assert.NotContains(t, directOther, "usage_conversion")
}
