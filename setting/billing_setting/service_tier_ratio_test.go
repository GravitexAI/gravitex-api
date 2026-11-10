package billing_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 档位倍率先按完整模型名查，模型未配置或漏配档位时用内置默认值，都没有按原价。
func TestGetServiceTierRatioLookupOrder(t *testing.T) {
	saved := billingSetting.ServiceTierRatio
	t.Cleanup(func() { billingSetting.ServiceTierRatio = saved })

	require.NoError(t, config.UpdateConfigFromMap(&billingSetting, map[string]string{
		"service_tier_ratio": `{"gpt-5-mini":{"priority":1.8,"fast":1.8},"gpt-5.5":{"flex":0.6}}`,
	}))

	tests := []struct {
		name  string
		model string
		tier  string
		want  float64
	}{
		{name: "model override", model: "gpt-5-mini", tier: "priority", want: 1.8},
		{name: "model missing tier uses built-in default", model: "gpt-5-mini", tier: "flex", want: 0.5},
		{name: "model override below default", model: "gpt-5.5", tier: "flex", want: 0.6},
		{name: "unlisted model uses built-in fast", model: "claude-opus-5-5", tier: "fast", want: 2},
		{name: "unlisted model uses built-in ultrafast", model: "gpt-6-astra", tier: "ultrafast", want: 6},
		{name: "unknown tier keeps base price", model: "gpt-5", tier: "scale", want: 1},
		{name: "default tier keeps base price", model: "gpt-5", tier: "default", want: 1},
		{name: "full name only, no prefix match", model: "gpt-5-mini-2025", tier: "priority", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, GetServiceTierRatio(tt.model, tt.tier))
		})
	}
}

// 保存前必须拒绝会产生零/负扣费或异常放大的倍率。
func TestValidateServiceTierRatio(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: `{"gpt-5.5":{"priority":2.5,"flex":0.5},"gpt-5":{"priority":100}}`},
		{name: "empty object", value: `{}`},
		{name: "zero", value: `{"gpt-5":{"flex":0}}`, wantErr: true},
		{name: "negative", value: `{"gpt-5":{"flex":-1}}`, wantErr: true},
		{name: "over limit", value: `{"gpt-5":{"priority":101}}`, wantErr: true},
		{name: "flat map not nested", value: `{"priority":2}`, wantErr: true},
		{name: "not json", value: `priority=2`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceTierRatio(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
