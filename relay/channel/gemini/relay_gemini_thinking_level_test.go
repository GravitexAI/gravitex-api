package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Gemini 4 takes thinkingLevel (thinkingBudget is rejected with a 400 on Gemini 3 and
// later) and supports low/medium/high only, so minimal has to land on low and medium
// must survive instead of being promoted to high the way Gemini 3.x needs.
func TestMapEffortToGeminiThinkingLevel(t *testing.T) {
	cases := []struct {
		model  string
		effort string
		want   string
	}{
		{"gemini-4-argon", "minimal", "low"},
		{"gemini-4-argon", "low", "low"},
		{"gemini-4-argon", "medium", "medium"},
		{"gemini-4-argon", "high", "high"},
		{"gemini-4-argon", "xhigh", "high"},
		{"gemini-4-argon", "max", "high"},
		{"gemini-4-argon", "", ""},
		{"gemini-3-pro-preview", "minimal", "low"},
		{"gemini-3-pro-preview", "medium", "high"},
		{"gemini-3-pro-preview", "max", "high"},
	}

	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			assert.Equal(t, tc.want, mapEffortToGeminiThinkingLevel(tc.model, tc.effort))
		})
	}
}

func TestGeminiThinkingOnByDefaultCoversGeminiFour(t *testing.T) {
	assert.True(t, geminiThinkingOnByDefault("gemini-4-argon"))
	assert.True(t, geminiThinkingOnByDefault("gemini-3-pro-preview"))
	assert.False(t, geminiThinkingOnByDefault("gemini-4-argon-nothinking"))
}
