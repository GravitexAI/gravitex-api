package common

import "strings"

const (
	GeminiOmniFlashPreview   = "gemini-omni-flash-preview"
	GeminiOmni11FlashPreview = "gemini-omni-1.1-flash-preview"
)

// IsGeminiOmniModel identifies the video models handled by the Interactions task adaptor.
func IsGeminiOmniModel(name string) bool {
	name = strings.TrimSpace(name)
	return strings.EqualFold(name, GeminiOmniFlashPreview) || strings.EqualFold(name, GeminiOmni11FlashPreview)
}
