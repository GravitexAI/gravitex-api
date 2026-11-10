package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 保存前必须拒绝会产生零/负扣费或异常放大的 1h 缓存创建倍率。
func TestValidateCreateCache1hRatio(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: `{"claude-opus-5-5":2,"claude-sonnet-5-5":2}`},
		{name: "empty object", value: `{}`},
		{name: "zero", value: `{"claude-opus-5-5":0}`, wantErr: true},
		{name: "negative", value: `{"claude-opus-5-5":-2}`, wantErr: true},
		{name: "over limit", value: `{"claude-opus-5-5":101}`, wantErr: true},
		{name: "not a ratio map", value: `{"claude-opus-5-5":{"1h":2}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateCache1hRatio(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
