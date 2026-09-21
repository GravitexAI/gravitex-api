package common

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetConfiguredCostDiscount(t *testing.T) {
	tests := []struct {
		name       string
		discount   float64
		configured bool
		want       float64
		ok         bool
	}{
		{name: "explicit zero", discount: 0, configured: true, want: 0, ok: true},
		{name: "configured value", discount: 0.5, configured: true, want: 0.5, ok: true},
		{name: "legacy positive value", discount: 0.5, configured: false, want: 0.5, ok: true},
		{name: "missing", discount: 0, configured: false, want: 0, ok: false},
		{name: "negative", discount: -0.1, configured: true, want: 0, ok: false},
		{name: "above one", discount: 1.1, configured: true, want: 0, ok: false},
		{name: "nan", discount: math.NaN(), configured: true, want: 0, ok: false},
		{name: "infinity", discount: math.Inf(1), configured: true, want: 0, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			SetContextKey(c, constant.ContextKeyChannelCostDiscount, tt.discount)
			SetContextKey(c, constant.ContextKeyChannelCostDiscountConfigured, tt.configured)

			got, ok := GetConfiguredCostDiscount(c)
			require.Equal(t, tt.ok, ok)
			if ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}
