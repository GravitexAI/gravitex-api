package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func IsTokenModelAccessLimited(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) ||
		common.GetContextKeyBool(c, constant.ContextKeyTokenVendorLimitEnabled)
}

// IsModelAllowedByToken applies model and vendor limits as a union: an
// explicitly allowed model or any enabled model under an allowed vendor is
// accessible. Callers should invoke this only when token access is limited.
func IsModelAllowedByToken(c *gin.Context, modelName string) bool {
	matchName := ratio_setting.FormatMatchingModelName(modelName)
	if value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit); ok {
		if limits, ok := value.(map[string]bool); ok && limits[matchName] {
			return true
		}
	}

	value, ok := common.GetContextKey(c, constant.ContextKeyTokenVendorLimit)
	if !ok {
		return false
	}
	vendorLimits, ok := value.(map[int]bool)
	if !ok {
		return false
	}
	vendorId, ok := model.GetEnabledVendorIdFromModel(matchName)
	return ok && vendorLimits[vendorId]
}
