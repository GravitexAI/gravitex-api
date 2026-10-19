package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gemini 4 controls thinking with thinking_level, defaults to medium, and supports only
// low/medium/high. thinking_budget is rejected with a 400 on Gemini 3 and later, so an
// unknown-model fallback that emits a budget — or no configuration at all — breaks every
// request that carries a reasoning effort.
func TestRenderGeminiFourArgonUsesThinkingLevel(t *testing.T) {
	cases := []struct {
		name      string
		intent    Intent
		wantLevel string
		wantEffun Effort
	}{
		{
			name:      "low stays low",
			intent:    Intent{Effort: EffortLow},
			wantLevel: "low",
			wantEffun: EffortLow,
		},
		{
			name:      "medium stays medium",
			intent:    Intent{Effort: EffortMedium},
			wantLevel: "medium",
			wantEffun: EffortMedium,
		},
		{
			name:      "high stays high",
			intent:    Intent{Effort: EffortHigh},
			wantLevel: "high",
			wantEffun: EffortHigh,
		},
		{
			name:      "minimal is not supported and lands on low",
			intent:    Intent{Effort: EffortMinimal},
			wantLevel: "low",
			wantEffun: EffortLow,
		},
		{
			name:      "xhigh saturates to high",
			intent:    Intent{Effort: EffortXHigh},
			wantLevel: "high",
			wantEffun: EffortHigh,
		},
		{
			name:      "max saturates to high",
			intent:    Intent{Effort: EffortMax},
			wantLevel: "high",
			wantEffun: EffortHigh,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rendered, err := RenderGemini("gemini-4-argon", tc.intent, nil, 0)
			require.NoError(t, err)
			require.NotNil(t, rendered.Config)
			assert.Equal(t, tc.wantLevel, rendered.Config.ThinkingLevel)
			assert.Nil(t, rendered.Config.ThinkingBudget, "Gemini 4 rejects thinkingBudget")
			assert.Equal(t, tc.wantEffun, rendered.EffectiveEffort)
		})
	}
}

func TestRenderGeminiFourArgonModeDefaultsToMedium(t *testing.T) {
	rendered, err := RenderGemini("gemini-4-argon", Intent{Mode: ModeEnabled}, nil, 0)
	require.NoError(t, err)
	require.NotNil(t, rendered.Config)
	assert.Nil(t, rendered.Config.ThinkingBudget)
	assert.Equal(t, EffortMedium, rendered.EffectiveEffort)
}

func TestRenderGeminiFourArgonCannotDisableThinking(t *testing.T) {
	_, err := RenderGemini("gemini-4-argon", Intent{Effort: EffortNone}, nil, 0)
	require.ErrorIs(t, err, ErrThinkingNotDisabled)
}

// max_output_tokens must not be turned into a thinkingBudget for a thinking_level model.
func TestRenderGeminiFourArgonIgnoresMaxOutputTokensBudget(t *testing.T) {
	maxOutputTokens := uint(40000)
	rendered, err := RenderGemini("gemini-4-argon", Intent{Mode: ModeEnabled}, &maxOutputTokens, 0.6)
	require.NoError(t, err)
	require.NotNil(t, rendered.Config)
	assert.Nil(t, rendered.Config.ThinkingBudget)
}
