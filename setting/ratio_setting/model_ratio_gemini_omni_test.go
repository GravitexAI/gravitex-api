package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiOmni11DefaultPricingMatchesLegacy(t *testing.T) {
	InitRatioSettings()
	for _, modelName := range []string{"gemini-omni-flash-preview", "gemini-omni-1.1-flash-preview"} {
		t.Run(modelName, func(t *testing.T) {
			inputRatio, ok, _ := GetModelRatio(modelName)
			require.True(t, ok)
			require.Equal(t, 0.75, inputRatio)
			videoPrice, ok := GetVideoCompletionRatioPricing(modelName, false)
			require.True(t, ok)
			require.Equal(t, 17.5, videoPrice)
		})
	}
}
