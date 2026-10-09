package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNanoBanana21ImageOutputRatio(t *testing.T) {
	InitRatioSettings()

	require.Equal(t, 20.0, GetImageCompletionRatio("gemini-nano-banana-2.1"))
}
