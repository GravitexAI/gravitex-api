package common

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoUsageConversionAndRequestFormatConversion(t *testing.T) {
	info := &RelayInfo{
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini},
	}
	info.SetUsageConversion(map[string]any{
		"promptTokenCount": 12,
		"nested":           map[string]any{"tokenCount": 3},
	})

	require.True(t, info.HasRequestFormatConversion())
	require.Equal(t, map[string]any{
		"promptTokenCount": float64(12),
		"nested":           map[string]any{"tokenCount": float64(3)},
	}, info.UsageConversion)

	info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAI}
	assert.False(t, info.HasRequestFormatConversion())
	info.RequestConversionChain = []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAI}
	assert.False(t, info.HasRequestFormatConversion())
}
