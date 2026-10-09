package model_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsGeminiModelSupportImagineIncludesNanoBanana21(t *testing.T) {
	require.True(t, IsGeminiModelSupportImagine("gemini-nano-banana-2.1"))
	require.False(t, IsGeminiModelSupportImagine("gemini-3-flash-preview"))
}
