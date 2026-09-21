package common

import (
	"math"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// GetConfiguredCostDiscount returns the effective channel cost discount from
// the request context. The positive-value fallback keeps compatibility with
// older paths that did not set the explicit configured marker, while an
// explicitly configured zero remains a valid discount.
func GetConfiguredCostDiscount(c *gin.Context) (float64, bool) {
	if c == nil {
		return 0, false
	}
	discount := GetContextKeyFloat64(c, constant.ContextKeyChannelCostDiscount)
	configured := GetContextKeyBool(c, constant.ContextKeyChannelCostDiscountConfigured)
	if !configured && discount <= 0 {
		return 0, false
	}
	if math.IsNaN(discount) || math.IsInf(discount, 0) || discount < 0 || discount > 1 {
		return 0, false
	}
	return discount, true
}
