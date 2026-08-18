package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateAutoRouterByJsonStringRoundTrip(t *testing.T) {
	original := AutoRouter2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoRouterByJsonString(original))
	})

	require.NoError(t, UpdateAutoRouterByJsonString(`{
		"enabled": true,
		"default_tier": "high",
		"stickiness_ttl": 600,
		"tiers": {"medium": ["deepseek-chat"]},
		"task_prefer": {"code": ["claude-sonnet"]},
		"weights": {"deepseek-chat": 3},
		"capabilities": {"gpt-4o": {"vision": true, "tools": true}}
	}`))

	cfg := GetAutoRouterSetting()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, AutoCostTierHigh, cfg.DefaultTier)
	assert.Equal(t, 600, cfg.StickinessTTL)
	assert.Equal(t, []string{"deepseek-chat"}, cfg.Tiers[AutoCostTierMedium])
	assert.Equal(t, []string{"claude-sonnet"}, cfg.TaskPrefer["code"])
	assert.Equal(t, 3, cfg.Weights["deepseek-chat"])
	require.NotNil(t, cfg.Capabilities["gpt-4o"].Vision)
	assert.True(t, *cfg.Capabilities["gpt-4o"].Vision)
}

func TestUpdateAutoRouterByJsonStringRejectsInvalidJSON(t *testing.T) {
	original := AutoRouter2JsonString()
	t.Cleanup(func() {
		require.NoError(t, UpdateAutoRouterByJsonString(original))
	})

	require.Error(t, UpdateAutoRouterByJsonString(`{"enabled":`))
	assert.JSONEq(t, original, AutoRouter2JsonString())
}

func TestNormalizeAutoCostTier(t *testing.T) {
	assert.Equal(t, AutoCostTierLow, NormalizeAutoCostTier("LOW"))
	assert.Equal(t, "", NormalizeAutoCostTier("cheap"))
}
