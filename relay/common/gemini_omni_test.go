package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGeminiOmniModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"gemini-omni-flash-preview", true},
		{"gemini-omni-1.1-flash-preview", true},
		{" Gemini-Omni-1.1-Flash-Preview ", true},
		{"gemini-3-flash-preview", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsGeminiOmniModel(tc.name))
		})
	}
}
